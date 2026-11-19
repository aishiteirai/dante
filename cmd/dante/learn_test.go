package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/tech"
)

func TestUsosPeloApelido(t *testing.T) {
	lines := strings.Split("import (\n\ttea \"github.com/charmbracelet/bubbletea\"\n)\n\nfunc (m model) Init() tea.Cmd { return nil }\nvar x = 1\n", "\n")
	tt := tech.Tech{Package: "github.com/charmbracelet/bubbletea", Source: "go.mod"}
	uses := usesIn(lines, tt, tech.ImportNames(tt))
	if len(uses) != 2 || !strings.HasPrefix(uses[1], "5: func (m model) Init() tea.Cmd") {
		t.Fatalf("usos = %q", uses)
	}
	py := tech.Tech{Package: "numpy", Source: "requirements.txt"}
	if u := usesIn([]string{"import numpy as np", "a = np.zeros(3)"}, py, tech.ImportNames(py)); len(u) != 2 {
		t.Fatalf("usos pelo apelido do Python = %q", u)
	}
}

func TestTecnologiasETrilha(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	os.WriteFile(filepath.Join(m.repo.Root, "requirements.txt"), []byte("requests\n"), 0o644)
	os.WriteFile(filepath.Join(m.repo.Root, "a.py"), []byte("import requests\n\nr = requests.get('x')\n"), 0o644)

	nm, cmd := m.send("/tecnologias")
	m = nm.(model)
	nm, _ = m.Update(cmd())
	m = nm.(model)
	last := m.entries[len(m.entries)-1]
	if last.kind != eAssistant || !strings.Contains(last.text, "**Requests**") || !strings.Contains(last.text, "`a.py`") {
		t.Fatalf("/tecnologias: %+v", last)
	}

	// Com o relatório em mãos, a trilha monta o pacote sem tokens; sem modelo, avisa.
	nm, _ = m.send("/trilha requests")
	m = nm.(model)
	if !strings.Contains(m.status, "escolha um modelo") {
		t.Fatalf("sem modelo: %q", m.status)
	}
	rep := *m.techs
	tt, _ := rep.Find("requests")
	p := trailPrompt(m.repo, rep, tt)
	for _, want := range []string{`<arquivo caminho="a.py"`, "3: r = requests.get('x')", "Monte a trilha para aprender Requests"} {
		if !strings.Contains(p, want) {
			t.Errorf("faltou %q no pacote:\n%s", want, p)
		}
	}
	nm, _ = m.send("/trilha nao-existe")
	if s := nm.(model).status; !strings.Contains(s, "não achei nao-existe") || !strings.Contains(s, "Requests") {
		t.Fatalf("trilha desconhecida: %q", s)
	}
}
