package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/llm"
)

func TestPlanApplyRevert(t *testing.T) {
	r := setup(t)
	b := filepath.Join(r.Root, "b.py")
	orig, _ := os.ReadFile(b)

	c, err := r.PlanEdit("b.py", "y = 3", "y = 30", false)
	if err != nil {
		t.Fatal(err)
	}
	if cur, _ := os.ReadFile(b); string(cur) != string(orig) {
		t.Fatal("planejar não pode gravar")
	}
	if d := c.Diff(); !strings.Contains(d, "-y = 3") || !strings.Contains(d, "+y = 30") {
		t.Fatalf("diff = %q", d)
	}
	if err := r.Apply(c); err != nil {
		t.Fatal(err)
	}
	if cur, _ := os.ReadFile(b); !strings.Contains(string(cur), "y = 30") {
		t.Fatal("não aplicou")
	}
	if err := r.Apply(c); err == nil {
		t.Fatal("aplicar de novo deveria detectar que o arquivo mudou")
	}
	if err := r.Revert(c); err != nil {
		t.Fatal(err)
	}
	if cur, _ := os.ReadFile(b); string(cur) != string(orig) {
		t.Fatal("desfazer não restaurou")
	}

	// Criar e apagar.
	nc, err := r.PlanCreate("novo/x.go", "package x\n")
	if err != nil || r.Apply(nc) != nil {
		t.Fatalf("criar: %v", err)
	}
	if _, err := r.PlanCreate("novo/x.go", "y"); err == nil {
		t.Fatal("criar arquivo existente deveria falhar")
	}
	dc, _ := r.PlanDelete("novo/x.go")
	if r.Apply(dc) != nil || r.Revert(dc) != nil {
		t.Fatal("apagar e desfazer")
	}
	if _, err := os.Stat(filepath.Join(r.Root, "novo/x.go")); err != nil {
		t.Fatal("desfazer a exclusão deveria recriar o arquivo")
	}

	// Desfazer não atropela edição do desenvolvedor.
	c2, _ := r.PlanEdit("b.py", "x = 1", "x = 2", false)
	r.Apply(c2)
	os.WriteFile(b, []byte("mexi à mão\n"), 0o644)
	if err := r.Revert(c2); err == nil {
		t.Fatal("deveria recusar desfazer sobre edição manual")
	}
}

func TestEscritaRecusada(t *testing.T) {
	r := setup(t)
	os.WriteFile(filepath.Join(r.Root, "dup.txt"), []byte("a\na\n"), 0o644)
	cases := []struct {
		name string
		plan func() error
	}{
		{"fora", func() error { _, err := r.PlanCreate("../fora.txt", "x"); return err }},
		{"absoluto", func() error { _, err := r.PlanCreate("/tmp/x.txt", "x"); return err }},
		{".git", func() error { _, err := r.PlanCreate(".git/hooks/pre-commit", "x"); return err }},
		{"sensível", func() error { _, err := r.PlanCreate(".env", "X=1"); return err }},
		{"trecho ausente", func() error { _, err := r.PlanEdit("b.py", "nao existe", "y", false); return err }},
		{"trecho ambíguo", func() error { _, err := r.PlanEdit("dup.txt", "a", "b", false); return err }},
		{"inexistente", func() error { _, err := r.PlanEdit("nada.py", "a", "b", false); return err }},
	}
	for _, c := range cases {
		if c.plan() == nil {
			t.Errorf("%s: deveria recusar", c.name)
		}
	}
	if c, err := r.PlanEdit("dup.txt", "a", "b", true); err != nil || string(c.After) != "b\nb\n" {
		t.Fatalf("replace_all: %v %q", err, c.After)
	}
	// Link simbólico para fora.
	os.Symlink(os.TempDir(), filepath.Join(r.Root, "link"))
	if _, err := r.PlanCreate("link/x.txt", "x"); err == nil {
		t.Fatal("link simbólico para fora deveria ser recusado")
	}
	// Ler a pasta .git também é recusado.
	exec := r.Executor(func() string { return WorkTree })
	if out, bad := exec(context.Background(), llm.ToolCall{Name: "read_file", Args: `{"path":".git/config"}`}); !bad {
		t.Fatalf(".git/config foi lido: %q", out)
	}
}
