package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

func TestVerificacoesSoONovo(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go não está instalado")
	}
	t.Setenv("DANTE_CONFIG", t.TempDir())
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command(args[0], args[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	run("git", "init", "-q")
	run("git", "config", "user.email", "t@e")
	run("git", "config", "user.name", "T")
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.21\n"), 0o644)
	// Um problema antigo, que já estava no commit: não pode aparecer.
	old := "package x\n\nimport \"fmt\"\n\nfunc Antigo() { fmt.Printf(\"%d\\n\", \"a\") }\n"
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(old), 0o644)
	run("git", "add", ".")
	run("git", "commit", "-qm", "base")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(old+"\nfunc Novo() { fmt.Printf(\"%s\\n\", 1) }\n"), 0o644)

	m := newModel(config.Defaults(), "", "")
	root, _ := repo.Root(dir)
	m.repo = repo.Repo{Root: root}
	m.target = repo.WorkTree
	m.files, _ = m.repo.Files(repo.WorkTree)
	m.chatView.Width, m.chatView.Height = 100, 40

	nm, _ := m.send("/verificar")
	m = nm.(model)
	if m.mode != modeChecks {
		t.Fatalf("modo=%d status=%q", m.mode, m.status)
	}
	if !strings.Contains(m.checksConfirmPopup(), "go vet .") {
		t.Fatal("na primeira vez, deveria pedir a permissão mostrando o comando")
	}
	nm, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	m = nm.(model)
	var done checksDoneMsg
	for _, msg := range collectMsgs(cmd) {
		if d, ok := msg.(checksDoneMsg); ok {
			done = d
		}
	}
	nm, _ = m.Update(done)
	m = nm.(model)
	var cards []finding
	for _, e := range m.entries {
		if e.kind == eFinding {
			cards = append(cards, *e.f)
		}
	}
	if len(cards) != 1 || cards[0].Linha != 7 || cards[0].Fonte != "go vet" {
		t.Fatalf("só o problema novo (linha 7) deveria aparecer: %+v", cards)
	}
	if !strings.Contains(m.checkBlock, "a.go:7") || m.cfg.ChecksAllowed(root) {
		t.Fatalf("bloco = %q (\"só desta vez\" não salva a permissão)", m.checkBlock)
	}
}

// collectMsgs roda um tea.Cmd (e os de um Batch) e devolve as mensagens.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			if c != nil {
				if _, isTick := c().(interface{ Tick() }); !isTick {
					out = append(out, collectMsgs(c)...)
				}
			}
		}
		return out
	}
	return []tea.Msg{msg}
}
