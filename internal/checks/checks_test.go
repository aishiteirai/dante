package checks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsers(t *testing.T) {
	sarif := `{"runs":[{"tool":{"driver":{"name":"ESLint"}},"results":[{"ruleId":"no-unused-vars","level":"error",
		"message":{"text":"'x' is unused"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"src/a.ts"},"region":{"startLine":3}}}]}]}]}`
	is := Parse("qlty", "sarif", "progresso…\n"+sarif, "/r")
	if len(is) != 1 || is[0].Tool != "qlty/eslint" || is[0].Path != "src/a.ts" || is[0].Line != 3 || !is[0].Error {
		t.Fatalf("sarif = %+v", is)
	}
	gh := "::error title=lint/style/useConst,file=/r/src/b.ts,line=7,col=3::This let is never reassigned\n::warning file=src/c.ts,line=2::hmm"
	is = Parse("biome", "github", gh, "/r")
	if len(is) != 2 || is[0].Path != "src/b.ts" || is[0].Line != 7 || is[0].Rule != "lint/style/useConst" || is[1].Error {
		t.Fatalf("github = %+v", is)
	}
	es := `[{"filePath":"/r/src/d.js","messages":[{"ruleId":"eqeqeq","severity":1,"message":"Expected ===","line":4}]}]`
	if is = Parse("eslint", "eslint", es, "/r"); len(is) != 1 || is[0].Path != "src/d.js" || is[0].Error {
		t.Fatalf("eslint = %+v", is)
	}
	ts := "src/e.ts(10,5): error TS2322: Type 'string' is not assignable to type 'number'."
	if is = Parse("tsc", "tsc", ts, "/r"); len(is) != 1 || is[0].Rule != "TS2322" || is[0].Line != 10 {
		t.Fatalf("tsc = %+v", is)
	}
	ruff := "app/x.py:12:5: F841 Local variable `y` is assigned to but never used\nFound 1 error."
	if is = Parse("ruff", "text", ruff, "/r"); len(is) != 1 || is[0].Path != "app/x.py" || is[0].Line != 12 {
		t.Fatalf("ruff = %+v", is)
	}
}

func TestSoOQueENovo(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -3,0 +4,2 @@\n+x\n+y\n@@ -10 +12 @@\n-z\n+w\n"
	ch := ChangedLines(diff)
	if !ch["a.go"][4] || !ch["a.go"][5] || !ch["a.go"][12] || ch["a.go"][6] {
		t.Fatalf("linhas = %v", ch)
	}
	ch["novo.go"] = map[int]bool{0: true}
	is := OnlyNew([]Issue{{Path: "a.go", Line: 4, Message: "novo"}, {Path: "a.go", Line: 8, Message: "antigo"},
		{Path: "b.go", Line: 1, Message: "outro arquivo"}, {Path: "novo.go", Line: 30, Message: "arquivo novo", Error: true}}, ch)
	if len(is) != 2 || is[0].Message != "arquivo novo" || is[1].Message != "novo" {
		t.Fatalf("novos = %+v", is)
	}
}

func TestGoVetDeVerdade(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go não está instalado")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.21\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n\nimport \"fmt\"\n\nfunc F() { fmt.Printf(\"%d\\n\", \"texto\") }\n"), 0o644)
	ts := Detect(dir, []string{"pkg/a.go", "README.md"})
	if len(ts) != 1 || ts[0].Name != "go vet" || ts[0].Command() != "go vet ./pkg" {
		t.Fatalf("detectadas = %+v", ts)
	}
	r := Run(context.Background(), ts[0])
	if r.Err != "" || len(r.Issues) != 1 || r.Issues[0].Path != "pkg/a.go" || r.Issues[0].Line != 5 || !strings.Contains(r.Issues[0].Message, "Printf") {
		t.Fatalf("resultado = %+v", r)
	}
}
