package tech

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestos(t *testing.T) {
	gomod := "module x\n\ngo 1.25\n\nrequire (\n\tgithub.com/charmbracelet/bubbletea/v2 v2.0.0\n\tgolang.org/x/sys v0.1.0 // indirect\n)\n\nrequire github.com/stretchr/testify v1.9.0\n"
	ts := parseGoMod(gomod)
	if len(ts) != 3 || ts[0].Name != "Go" || ts[0].Version != "1.25" || ts[1].Package != "github.com/charmbracelet/bubbletea/v2" {
		t.Fatalf("go.mod = %+v", ts)
	}
	known(&ts[1])
	if ts[1].Name != "Bubble Tea" || ts[1].Kind != "interface" {
		t.Fatalf("v2 deveria casar com o catálogo: %+v", ts[1])
	}
	pj := parsePackageJSON(`{"dependencies":{"react":"^19","@types/node":"x"},"devDependencies":{"vitest":"^3"}}`)
	if len(pj) != 2 || pj[0].Package != "react" || !pj[1].Dev {
		t.Fatalf("package.json = %+v", pj)
	}
	py := parsePyproject("[project]\nname = \"x\"\ndependencies = [\n  \"fastapi>=0.1\",\n  \"pydantic[email]\",\n]\n\n[dependency-groups]\ndev = [\"pytest\"]\n")
	if len(py) != 4 || py[1].Package != "fastapi" || py[2].Package != "pydantic" || !py[3].Dev {
		t.Fatalf("pyproject = %+v", py)
	}
	rq := parseRequirements("# x\nrequests==2.0\n-r base.txt\nPyYAML>=6 ; python_version>'3'\n")
	if len(rq) != 2 || rq[1].Package != "pyyaml" || rq[1].Version != ">=6" {
		t.Fatalf("requirements = %+v", rq)
	}
	if c := parseCargo("[package]\nname=\"x\"\n[dependencies]\ntokio = { version = \"1\" }\n[dev-dependencies]\nserde = \"1\"\n"); len(c) != 3 || !c[2].Dev {
		t.Fatalf("Cargo = %+v", c)
	}
	if g := parseGemfile("source 'x'\ngem 'rails', '~> 7'\n"); len(g) != 2 || g[1].Package != "rails" {
		t.Fatalf("Gemfile = %+v", g)
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module x\n\ngo 1.25\n\nrequire github.com/charmbracelet/lipgloss v1.0.0\nrequire example.com/desconhecida v1.0.0\n")
	write("ui/a.go", "package ui\n\nimport \"github.com/charmbracelet/lipgloss\"\n\nvar s = lipgloss.NewStyle()\n")
	write("ui/a_test.go", "package ui\n\nimport \"github.com/charmbracelet/lipgloss\"\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	r := Detect(dir)
	lg, ok := r.Find("lipgloss")
	if !ok || lg.Name != "Lip Gloss" || lg.Uses != 2 || lg.Files[0] != "ui/a.go" {
		t.Fatalf("lipgloss = %+v (%v)", lg, ok)
	}
	if len(r.Languages) != 1 || r.Languages[0].Name != "Go" {
		t.Fatalf("linguagens = %+v", r.Languages)
	}
	md := r.Markdown()
	for _, want := range []string{"**Lip Gloss**", "Em 2 arquivo(s), como `ui/a.go`", "Outras dependências:** example.com/desconhecida"} {
		if !strings.Contains(md, want) {
			t.Errorf("faltou %q em:\n%s", want, md)
		}
	}
}
