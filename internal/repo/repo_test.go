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

func TestDiffComContexto(t *testing.T) {
	r := setup(t)
	log, _ := r.Log(10)
	exec := r.Executor(func() string { return log[0].SHA })
	curto, _ := exec(context.Background(), llm.ToolCall{Name: "git_diff", Args: `{"context_lines":1}`})
	padrao, _ := exec(context.Background(), llm.ToolCall{Name: "git_diff", Args: `{}`})
	// As mudanças são nas linhas 2 e 7: com 10 de contexto a linha 4 aparece; com 1, não.
	if strings.Contains(curto, "\n b = 0") || !strings.Contains(padrao, "\n b = 0") {
		t.Fatalf("contexto não aplicado:\ncurto=%s\npadrão=%s", curto, padrao)
	}
	d, _ := r.DiffForModel(log[0].SHA, "", 10)
	if d != strings.TrimSuffix(padrao, "\n") && d != padrao {
		t.Fatalf("DiffForModel difere do git_diff:\n%q\n%q", d, padrao)
	}
}

func TestSafePathComAsDuasBarras(t *testing.T) {
	r := Repo{Root: "/repo"}
	ok := map[string]string{"a/b.go": "a/b.go", `a\b.go`: "a/b.go", "./x/../y.go": "y.go", `x\..\y.go`: "y.go", "/repo/z.go": "z.go"}
	for in, want := range ok {
		if got, err := r.safePath(in); err != nil || got != want {
			t.Errorf("safePath(%q) = %q, %v; queria %q", in, got, err, want)
		}
	}
	for _, in := range []string{"..", "../x", `..\x`, `a\..\..\x`, `..\..\Windows\win.ini`, "/etc/passwd", `\etc\passwd`, "a/../../x"} {
		if got, err := r.safePath(in); err == nil {
			t.Errorf("safePath(%q) deveria recusar, deu %q", in, got)
		}
	}
}

func TestStatsEMetadados(t *testing.T) {
	r := setup(t)
	log, _ := r.Log(10)
	if log[0].Time == 0 {
		t.Fatal("o commit deveria trazer a data")
	}
	st, err := r.Stats([]string{log[0].SHA, log[1].SHA})
	if err != nil {
		t.Fatal(err)
	}
	// O segundo commit renomeia a.py para b.py e muda 2 linhas (sem renomeação detectada: --no-renames).
	if s := st[log[1].SHA]; s.Files != 1 || s.Add != 6 || s.Del != 0 {
		t.Fatalf("commit raiz = %+v", s)
	}
	if s := st[log[0].SHA]; s.Files != 2 {
		t.Fatalf("commit de renomeação = %+v", s)
	}
	fs, _ := r.Files(log[0].SHA)
	if fs[0].Status != "R" || fs[0].OldPath != "a.py" || fs[0].Path != "b.py" {
		t.Fatalf("renomeação = %+v", fs[0])
	}
	if s := parseShortstat(" 3 files changed, 10 insertions(+), 2 deletions(-)"); s != (CommitStat{3, 10, 2}) {
		t.Fatalf("parseShortstat = %+v", s)
	}
}

func TestMergedSubjects(t *testing.T) {
	r := setup(t)
	run(t, r.Root, "git", "checkout", "-q", "-b", "ramo")
	os.WriteFile(filepath.Join(r.Root, "n.txt"), []byte("1\n"), 0o644)
	run(t, r.Root, "git", "add", ".")
	run(t, r.Root, "git", "commit", "-qm", "feat: algo novo")
	run(t, r.Root, "git", "checkout", "-q", "main")
	run(t, r.Root, "git", "merge", "-q", "--no-ff", "-m", "Merge do ramo", "ramo")
	subs := r.MergedSubjects(r.Head(), 10)
	if len(subs) != 1 || !strings.HasSuffix(subs[0], "feat: algo novo") {
		t.Fatalf("merge: %v", subs)
	}
	if r.MergedSubjects(r.Head()+"^1", 10) != nil {
		t.Fatal("commit comum não é merge")
	}
}

func TestBranchComoPR(t *testing.T) {
	r := setup(t) // main com 2 commits
	run(t, r.Root, "git", "checkout", "-q", "-b", "feat/x")
	os.WriteFile(filepath.Join(r.Root, "novo.py"), []byte("z = 1\n"), 0o644)
	run(t, r.Root, "git", "add", ".")
	run(t, r.Root, "git", "commit", "-qm", "feat: novo")
	os.WriteFile(filepath.Join(r.Root, "novo.py"), []byte("z = 2\n"), 0o644)
	run(t, r.Root, "git", "commit", "-qam", "fix: ajuste")
	// A main anda depois que a branch saiu: isso não deve entrar no "PR".
	run(t, r.Root, "git", "checkout", "-q", "main")
	os.WriteFile(filepath.Join(r.Root, "outro.py"), []byte("w = 1\n"), 0o644)
	run(t, r.Root, "git", "add", ".")
	run(t, r.Root, "git", "commit", "-qm", "main andou")
	run(t, r.Root, "git", "checkout", "-q", "feat/x")

	if r.BaseBranch() != "main" {
		t.Fatalf("base = %q", r.BaseBranch())
	}
	rng := Range("main", "feat/x")
	if subs := r.RangeCommits(rng, 10); len(subs) != 2 || !strings.HasSuffix(subs[0], "fix: ajuste") {
		t.Fatalf("commits = %v", subs)
	}
	fs, err := r.Files(rng)
	if err != nil || len(fs) != 1 || fs[0].Path != "novo.py" || fs[0].Status != "A" {
		t.Fatalf("arquivos = %+v %v", fs, err)
	}
	if Tip(rng) != "feat/x" || Tip("abc") != "abc" {
		t.Fatal("Tip")
	}
}
