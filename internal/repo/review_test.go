package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/llm"
)

func TestKind(t *testing.T) {
	cases := map[string]FileKind{
		"cmd/dante/app.go": KindCode, "cmd/dante/app_test.go": KindTest, "tests/test_x.py": KindTest,
		"web/a.spec.ts": KindTest, "README.md": KindDoc, "docs/guia.html": KindDoc, "go.sum": KindGenerated,
		"web/app.min.js": KindGenerated, "scripts/instalar.sh": KindCode, "LICENSE": KindDoc,
	}
	for p, want := range cases {
		if got := Kind(p); got != want {
			t.Errorf("%s: %v, queria %v", p, kindWords[got], kindWords[want])
		}
	}
}

func TestPacoteDeRevisao(t *testing.T) {
	r := setup(t)
	write := func(p, c string) {
		os.MkdirAll(filepath.Join(r.Root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(r.Root, p), []byte(c), 0o644)
	}
	write("grande.go", "package x\n")
	write("pequeno.go", "package x\n")
	run(t, r.Root, "git", "add", ".")
	run(t, r.Root, "git", "commit", "-qm", "base")
	var big strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&big, "var v%d = %d\n", i, i)
	}
	write("grande.go", "package x\n"+big.String())
	write("pequeno.go", "package x\n\nfunc F() {}\n")
	write("pequeno_test.go", "package x\n")
	write("README.md", "# doc\n")
	write(".env", "SEGREDO=1\n")
	run(t, r.Root, "git", "add", ".")
	run(t, r.Root, "git", "commit", "-qm", "mudanças")
	head := r.Head()

	p, err := r.ReviewDiff(head, 100000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Included != 2 || !strings.Contains(p.Diff, "func F()") || strings.Contains(p.Diff, "SEGREDO") {
		t.Fatalf("incluídos=%d diff=%q", p.Included, p.Diff)
	}
	var left []string
	for _, l := range p.Left {
		left = append(left, l.Path+":"+l.Why)
	}
	if strings.Join(left, ",") != "README.md:documentação,pequeno_test.go:teste" {
		t.Fatalf("fora do diff: %v", left)
	}
	// Sem espaço para o maior: entra o menor, e o maior vai para a lista.
	p, _ = r.ReviewDiff(head, 1000)
	if p.Included != 1 || !strings.Contains(p.Diff, "pequeno.go") || p.Left[1].Path != "grande.go" || p.Left[1].Why != "não coube aqui" {
		t.Fatalf("limite: %+v", p.Left)
	}
}

func TestEsqueleto(t *testing.T) {
	r := setup(t)
	var b strings.Builder
	b.WriteString("package x\n\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "// F%d faz algo.\nfunc F%d() {\n\tprintln(%d)\n}\n\n", i, i, i)
	}
	os.WriteFile(filepath.Join(r.Root, "big.go"), []byte(b.String()), 0o644)
	exec := r.Executor(func() string { return WorkTree })
	out, bad := exec(context.Background(), llm.ToolCall{Name: "read_file", Args: `{"path":"big.go"}`})
	if bad || !strings.Contains(out, "esqueleto") || !strings.Contains(out, "    4  func F0() {") || strings.Contains(out, "println") {
		t.Fatalf("esqueleto:\n%s", out[:min(len(out), 600)])
	}
	out, _ = exec(context.Background(), llm.ToolCall{Name: "read_file", Args: `{"path":"big.go","start_line":4,"end_line":6}`})
	if !strings.Contains(out, "println(0)") {
		t.Fatalf("trecho: %s", out)
	}
	out, _ = exec(context.Background(), llm.ToolCall{Name: "read_files", Args: `{"files":[{"path":"big.go","inteiro":true}]}`})
	if !strings.Contains(out, "println(59)") {
		t.Fatal("inteiro=true deveria ler tudo")
	}
}
