package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestExploradorDoProjeto(t *testing.T) {
	tr := &projTree{files: []string{"README.md", "cmd/dante/main.go", "internal/llm/llm.go", "internal/repo/git.go"}, open: map[string]bool{}}
	names := func() (out []string) {
		for _, n := range tr.nodes() {
			out = append(out, strings.Repeat(".", n.depth)+n.name)
		}
		return
	}
	if got := strings.Join(names(), ","); got != "cmd,internal,README.md" {
		t.Fatalf("raiz = %s", got)
	}
	tr.open["internal"] = true
	tr.open["internal/llm"] = true
	if got := strings.Join(names(), ","); got != "cmd,internal,.llm,..llm.go,.repo,README.md" {
		t.Fatalf("aberto = %s", got)
	}
}

func TestAbaArquivo(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	m.screen, m.width, m.height = scrMain, 120, 30
	m.relayout()
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		b.WriteString("linha comum\n")
	}
	b.WriteString("aqui está o alvo\n")
	os.WriteFile(filepath.Join(m.repo.Root, "longo.txt"), []byte(b.String()), 0o644)

	_, cmd := m.openFileTab("longo.txt", repo.WorkTree, 0)
	nm, _ := m.Update(cmd())
	m = nm.(model)
	if m.vtab != vtFile || m.ftab == nil || len(m.ftab.lines) != 61 {
		t.Fatal("a aba Arquivo deveria abrir")
	}
	m.focus = pDiff
	// / busca: as teclas vão para a busca (inclusive q, que fecharia o dante).
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("/")}, {Type: tea.KeyRunes, Runes: []rune("alvo q")}, {Type: tea.KeyBackspace}, {Type: tea.KeyBackspace}, {Type: tea.KeyEnter}} {
		nm, _ = m.onKey(k)
		m = nm.(model)
	}
	if m.ftab.search != "alvo" || len(m.ftab.matches) != 1 || m.ftab.matches[0] != 61 {
		t.Fatalf("busca = %q %v", m.ftab.search, m.ftab.matches)
	}
	if !strings.Contains(ansi.Strip(m.preview.View()), "aqui está o alvo") {
		t.Fatal("a busca deveria rolar até o resultado")
	}
	// : vai para a linha.
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune(":")}, {Type: tea.KeyRunes, Runes: []rune("10")}, {Type: tea.KeyEnter}} {
		nm, _ = m.onKey(k)
		m = nm.(model)
	}
	if m.markLine != 10 {
		t.Fatalf("linha marcada = %d", m.markLine)
	}
	if p, _, line, ok := m.funcLocation(); !ok || p != "longo.txt" || line != 10 {
		t.Fatalf("f usaria %s:%d", p, line)
	}
}

func TestTermosEPastas(t *testing.T) {
	got := parseTerms("1. orders, `ListOrders`\n- filtro de data; dateFilter, ab, orders")
	if strings.Join(got, "|") != "orders|ListOrders|filtro de data|dateFilter" {
		t.Fatalf("termos = %v", got)
	}
	if f := folderList([]string{"a.go", "cmd/dante/x.go", "cmd/dante/y.go", "internal/llm/a/b.go"}, 10); f != ".(1)\ncmd/dante (2)\ninternal/llm (1)" && !strings.Contains(f, "cmd/dante (2)") {
		t.Fatalf("pastas = %q", f)
	}
}
