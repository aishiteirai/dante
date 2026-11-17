package main

import (
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFirstHunkLine(t *testing.T) {
	d := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -10,3 +42,5 @@ func x()\n"
	if n := firstHunkLine(d); n != 42 {
		t.Fatalf("linha = %d", n)
	}
	if n := firstHunkLine("nada"); n != 1 {
		t.Fatalf("sem hunk = %d", n)
	}
}

func TestRemoteURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/a/b.git": "https://github.com/a/b.git",
		"git@github.com:a/b.git":     "git@github.com:a/b.git",
		"aishiteirai/dante":          "https://github.com/aishiteirai/dante",
		"github.com/a/b":             "https://github.com/a/b",
	}
	for in, want := range cases {
		if got, ok := remoteURL(in); !ok || got != want {
			t.Errorf("remoteURL(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := remoteURL("/tmp"); ok {
		t.Error("/tmp é caminho local")
	}
	if d := cloneDir("git@github.com:dono/proj.git"); !strings.HasSuffix(d, "repos/dono-proj") {
		t.Errorf("cloneDir = %s", d)
	}
}

func TestDescribeCall(t *testing.T) {
	c := llm.ToolCall{Name: "read_file", Args: `{"path":"a.go","start_line":3,"end_line":9,"ref":"abcdef1234"}`}
	if got := describeCall(c); got != "abriu a.go:3-9 @ abcdef1" {
		t.Fatalf("got %q", got)
	}
}

func TestModelRowsEPreSelecao(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	cfg := config.Defaults()
	cfg.UltimoModelo = &config.Choice{Provedor: "ollama", Modelo: "qwen2.5-coder:7b"}
	m := newModel(cfg, "", "")
	m.enterModelScreen()
	if r := m.modelRows[m.modelIdx]; r.prov.ID != "claude" {
		t.Fatalf("antes da lista local, esperava o primeiro modelo; veio %s/%s", r.prov.ID, r.model)
	}
	nm, _ := m.Update(modelsListedMsg{prov: "ollama", models: []string{"deepseek-r1:8b", "qwen2.5-coder:7b"}})
	m = nm.(model)
	if r := m.modelRows[m.modelIdx]; r.model != "qwen2.5-coder:7b" {
		t.Fatalf("pré-seleção = %s", r.model)
	}
}

func TestTelaPrincipalRenderiza(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.screen = scrMain
	m.repo = repo.Repo{Root: "/tmp/x"}
	m.prov, _ = m.cfg.Provider("deepseek")
	m.modelName = "deepseek-chat"
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	m = nm.(model)
	m.rows = []logRow{{worktree: true}, {c: repo.Commit{Graph: "* ", SHA: "abc", Short: "abc", Subject: "primeiro"}}}
	m.dirty = 2
	m.files = []repo.FileChange{{Path: "a.go", Status: "M", Add: 3, Del: 1}}
	m.entries = []entry{{kind: eUser, text: "oi"}, {kind: eAssistant, text: "**olá**"}}
	m.refreshChat()
	v := m.View()
	for _, want := range []string{"Commits", "Mudanças locais", "a.go", "deepseek-chat", "olá"} {
		if !strings.Contains(v, want) {
			t.Errorf("tela sem %q", want)
		}
	}
	for i, l := range strings.Split(v, "\n") {
		if w := len([]rune(stripANSI(l))); w > 150 {
			t.Fatalf("linha %d passa da largura: %d", i, w)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
