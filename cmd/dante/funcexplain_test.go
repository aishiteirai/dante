package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/repo"
)

func TestDocLocation(t *testing.T) {
	cases := []struct{ title, path, ref string }{
		{"a.go", "a.go", repo.WorkTree},
		{"a.go @ abc1234", "a.go", "abc1234"},
		{"cmd/x.go:12 · referência [3]", "cmd/x.go", "alvo"},
		{"cmd/x.go:12 · referência [3] · versão atual (não existe neste commit)", "cmd/x.go", repo.WorkTree},
	}
	for _, c := range cases {
		p, ref, ok := docLocation(c.title, "alvo")
		if !ok || p != c.path || ref != c.ref {
			t.Errorf("%q → %q %q", c.title, p, ref)
		}
	}
	if _, _, ok := docLocation("3 arquivos abertos pelo modelo", "alvo"); ok {
		t.Error("lote de arquivos não tem um arquivo só")
	}
}

func TestPerguntaSobreFuncao(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	os.WriteFile(filepath.Join(m.repo.Root, "calc.py"), []byte("def total(itens):\n    return sum(itens)\n\nprint(total([1, 2]))\n"), 0o644)
	m.target = repo.WorkTree
	m.prov, _ = m.cfg.Provider("ollama")
	m.modelName = "x"

	// /funcao com o nome acha a função e monta o pacote.
	nm, cmd := m.send("/funcao total")
	m = nm.(model)
	if cmd == nil {
		t.Fatalf("deveria procurar a função; status = %q", m.status)
	}
	msg := cmd().(funcPackMsg)
	if msg.err != nil || msg.info.Name != "total" || msg.info.Path != "calc.py" {
		t.Fatalf("pacote = %+v", msg)
	}
	p := m.funcPrompt(msg)
	for _, want := range []string{`<funcao nome="total" arquivo="calc.py" linhas="1-2"`, "return sum(itens)",
		"<onde_e_usada>", "calc.py:4  print(total([1, 2]))", "O que faz a função total"} {
		if !strings.Contains(p, want) {
			t.Errorf("pergunta sem %q:\n%s", want, p)
		}
	}
	// Uma mensagem com uma função colada e "o que faz" vai pelo mesmo caminho.
	if !asksAboutPastedFunc("o que faz isso?\nfunc soma(a, b int) int {\n\treturn a + b\n}") || asksAboutPastedFunc("o que faz o commit?") {
		t.Fatal("asksAboutPastedFunc")
	}
	// Código colado que não existe no repositório vai como veio.
	_, cmd = m.explainFuncText("func soma(a, b int) int {\n\treturn a + b\n}")
	msg = cmd().(funcPackMsg)
	if msg.err != nil || !msg.pasted || !strings.Contains(msg.info.Code, "return a + b") {
		t.Fatalf("colado = %+v", msg)
	}
}
