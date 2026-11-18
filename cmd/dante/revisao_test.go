package main

import (
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/repo"
)

func TestListaDeVerificacao(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n@@ -1 +1,2 @@\n contexto com mutex\n+\trows, err := db.Query(\"SELECT * FROM u WHERE id=\" + id)\n"
	c := reviewChecklist(diff)
	for _, want := range []string{"Erros e casos de borda", "Código limpo", "Entrada externa e injeção"} {
		if !strings.Contains(c, want) {
			t.Errorf("lista sem %q:\n%s", want, c)
		}
	}
	// O contexto (linha sem +/−) não ativa listas: o mutex não está nas mudanças.
	if strings.Contains(c, "Concorrência") {
		t.Fatalf("o contexto não deveria contar:\n%s", c)
	}
}

func TestPedidoDeCorrecao(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	t.Setenv("DANTE_HOME", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.target = repo.Range("main", "feat/x")
	f := finding{Arquivo: "a.go", Linha: 10, Severidade: "alta", Categoria: "seguranca", Titulo: "SQL montado com texto",
		Explicacao: "id vem da URL.", Sugestao: "Use parâmetros."}
	m.entries = []entry{{kind: eFinding, f: &f, ref: 1}}
	p := m.fixRequest(m.allFindings())
	for _, want := range []string{"na branch feat/x (comparada com main)", "**SQL montado com texto** (ALTA, segurança) em `a.go:10`",
		"Sugestão: Use parâmetros.", "Confira cada ponto"} {
		if !strings.Contains(p, want) {
			t.Errorf("pedido sem %q:\n%s", want, p)
		}
	}
	if m.findingByRef(1) == nil || m.findingByRef(2) != nil {
		t.Fatal("findingByRef")
	}
	m.copyFixRequest(m.allFindings())
	if !strings.Contains(m.status, "copiado") {
		t.Fatalf("status = %q", m.status)
	}
}
