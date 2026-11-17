package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/llm"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", args, out)
	}
}

func setup(t *testing.T) Repo {
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	run(t, dir, "git", "config", "user.email", "t@example.com")
	run(t, dir, "git", "config", "user.name", "T")
	os.WriteFile(filepath.Join(dir, "a.py"), []byte("x = 1\ny = 2\na = 0\nb = 0\nc = 0\nd = 0\n"), 0o644)
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-qm", "primeiro")
	os.WriteFile(filepath.Join(dir, "a.py"), []byte("x = 1\ny = 3\na = 0\nb = 0\nc = 0\nd = 0\nz = 4\n"), 0o644)
	run(t, dir, "git", "mv", "a.py", "b.py")
	run(t, dir, "git", "commit", "-qam", "segundo")
	root, _ := Root(dir)
	return Repo{Root: root}
}

func TestLogFilesDiff(t *testing.T) {
	r := setup(t)
	log, err := r.Log(50)
	if err != nil || len(log) != 2 || log[0].Subject != "segundo" || !strings.HasPrefix(log[0].Graph, "*") {
		t.Fatalf("log = %+v, %v", log, err)
	}
	fs, _ := r.Files(log[0].SHA)
	if len(fs) != 1 || fs[0].Path != "b.py" || fs[0].Status != "R" || fs[0].Add != 2 || fs[0].Del != 1 {
		t.Fatalf("arquivos do commit = %+v", fs)
	}
	root, _ := r.Files(log[1].SHA) // commit raiz
	if len(root) != 1 || root[0].Status != "A" || root[0].Add != 6 {
		t.Fatalf("commit raiz = %+v", root)
	}
	// Mudanças locais: modificado + não rastreado.
	os.WriteFile(filepath.Join(r.Root, "b.py"), []byte("mudou\n"), 0o644)
	os.WriteFile(filepath.Join(r.Root, "novo.py"), []byte("a\nb\n"), 0o644)
	wt, _ := r.Files(WorkTree)
	if len(wt) != 2 || wt[1].Status != "?" || wt[1].Add != 2 {
		t.Fatalf("worktree = %+v", wt)
	}
	if d, _ := r.Diff(WorkTree, "novo.py"); !strings.Contains(d, "+a") {
		t.Fatalf("diff não rastreado = %q", d)
	}
	if d, _ := r.Diff(WorkTree, "b.py"); !strings.Contains(d, "+mudou") {
		t.Fatalf("diff worktree = %q", d)
	}
}

func TestTools(t *testing.T) {
	r := setup(t)
	log, _ := r.Log(10)
	exec := r.Executor(func() string { return log[0].SHA })
	call := func(name, args string) (string, bool) {
		return exec(context.Background(), llm.ToolCall{Name: name, Args: args})
	}
	if out, bad := call("read_file", `{"path":"b.py","start_line":2,"end_line":2}`); bad || !strings.Contains(out, "2  y = 3") || strings.Contains(out, "x = 1") {
		t.Fatalf("read_file trecho = %q", out)
	}
	if out, bad := call("read_file", `{"path":"a.py","ref":"`+log[1].SHA+`"}`); bad || !strings.Contains(out, "y = 2") {
		t.Fatalf("read_file ref = %q", out)
	}
	if out, bad := call("git_diff", `{}`); bad || !strings.Contains(out, "+z = 4") {
		t.Fatalf("git_diff = %q", out)
	}
	if out, bad := call("grep", `{"pattern":"z = "}`); bad || !strings.Contains(out, "b.py:7:z = 4") {
		t.Fatalf("grep = %q", out)
	}
	if out, bad := call("grep", `{"pattern":"inexistente"}`); bad || out != "(nenhum resultado)" {
		t.Fatalf("grep vazio = %q %v", out, bad)
	}
	for _, args := range []string{
		`{"path":"../../etc/passwd"}`, `{"path":"/etc/passwd"}`, `{"path":"b.py","ref":"--output=/tmp/x"}`, `{"path":`,
	} {
		if out, bad := call("read_file", args); !bad {
			t.Fatalf("deveria recusar %s, veio %q", args, out)
		}
	}
	// Link simbólico apontando para fora.
	os.Symlink("/etc/hostname", filepath.Join(r.Root, "link"))
	if _, bad := call("read_file", `{"path":"link"}`); !bad {
		t.Fatal("deveria recusar link para fora do repositório")
	}
}

func TestReadFiles(t *testing.T) {
	r := setup(t)
	os.WriteFile(filepath.Join(r.Root, ".env"), []byte("X=1\n"), 0o644)
	exec := r.Executor(func() string { return WorkTree })
	out, bad := exec(context.Background(), llm.ToolCall{Name: "read_files", Args: `{"files":[
		{"path":"b.py","start_line":2,"end_line":3},
		{"path":".env"},
		{"path":"nao-existe.py"},
		{"path":"../../etc/passwd"}]}`})
	if bad {
		t.Fatalf("o lote não deveria falhar inteiro: %s", out)
	}
	for _, want := range []string{"=== b.py (linhas 2-3) ===", "2  y = 3", "3  a = 0", "arquivo sensível bloqueado", "arquivo não encontrado", "fora do repositório"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em:\n%s", want, out)
		}
	}
	if strings.Contains(out, "1  x = 1") || strings.Contains(out, "X=1") {
		t.Errorf("leu fora do trecho ou o segredo:\n%s", out)
	}
	if _, bad := exec(context.Background(), llm.ToolCall{Name: "read_files", Args: `{"files":[]}`}); !bad {
		t.Error("lote vazio deveria ser erro")
	}
}
