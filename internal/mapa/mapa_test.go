package mapa

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
)

func testRepo(t *testing.T) repo.Repo {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"README.md":            "# demo\nUm projeto de exemplo.\n",
		"main.go":              "package main\n\nfunc main() {}\n",
		"internal/llm/llm.go":  "package llm\n\ntype Chat interface{}\n",
		"internal/llm/a/b.go":  "package a\n",
		"internal/repo/git.go": "package repo\n",
		".env":                 "SEGREDO=1\n",
		"go.sum":               "x\n",
		"logo.png":             "\x89PNG\x00\x00",
	}
	for p, c := range files {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s", out)
		}
	}
	root, _ := repo.Root(dir)
	return repo.Repo{Root: root}
}

type fakeChat struct {
	mu      *sync.Mutex
	prompts *[]string
}

func (f fakeChat) Send(_ context.Context, user string, emit func(llm.Event)) (llm.Usage, error) {
	f.mu.Lock()
	*f.prompts = append(*f.prompts, user)
	f.mu.Unlock()
	switch {
	case strings.Contains(user, "## Visão geral"):
		emit(llm.Event{Kind: llm.EvText, Text: "## Visão geral\nUm demo.\n\n## Por onde começar\n1. `main.go:3`\n\n## Fluxo principal\n1. main\n\n## Glossário\n**demo** — exemplo"})
	default:
		dir := strings.SplitN(strings.SplitN(user, "`", 3)[1], "`", 2)[0]
		emit(llm.Event{Kind: llm.EvText, Text: fmt.Sprintf("A pasta %s.\n\n- `x.go:1` — algo", dir)})
	}
	return llm.Usage{InputTokens: 100, OutputTokens: 10}, nil
}

func gen(prompts *[]string) Generator {
	mu := &sync.Mutex{}
	return Generator{Model: "barato", Commit: "abc", Concurrency: 3,
		NewChat: func(string, int64) (llm.Chat, error) { return fakeChat{mu, prompts}, nil }}
}

func TestMapaCompletoEIncremental(t *testing.T) {
	r := testRepo(t)
	s, err := ScanRepo(r)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, f := range s.Folders {
		dirs = append(dirs, f.Dir)
	}
	if strings.Join(dirs, ",") != ".,internal/llm,internal/repo" || s.Skipped != 3 {
		t.Fatalf("pastas %v, de fora %d", dirs, s.Skipped)
	}
	p := MakePlan(s, nil, 0)
	if len(p.Steps) != 4 || p.Tokens <= 0 {
		t.Fatalf("plano: %+v", p.Steps)
	}
	var prompts []string
	m, u, err := gen(&prompts).Run(context.Background(), p, func(Event) {})
	if err != nil || u.InputTokens != 400 {
		t.Fatalf("err=%v uso=%+v", err, u)
	}
	for _, pr := range prompts {
		if strings.Contains(pr, "SEGREDO") {
			t.Fatal("arquivo sensível foi enviado")
		}
	}
	if !strings.Contains(strings.Join(prompts, ""), "3  func main() {}") {
		t.Fatal("o conteúdo deveria ir numerado")
	}
	ch := m.Chapters()
	var titles []string
	for _, c := range ch {
		titles = append(titles, c.Title)
	}
	if strings.Join(titles, "|") != "Visão geral|Por onde começar|raiz do repositório|internal/llm|internal/repo|Fluxo principal|Glossário" {
		t.Fatalf("capítulos: %v", titles)
	}
	if c := m.Compact(5000); !strings.Contains(c, "internal/llm: A pasta internal/llm.") || !strings.Contains(c, "Um demo.") {
		t.Fatalf("compacto: %s", c)
	}

	// Salvar, mudar uma pasta e planejar de novo: só ela e a visão geral.
	path := filepath.Join(t.TempDir(), "m.json")
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	old, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p := MakePlan(s, old, 0); !p.UpToDate() {
		t.Fatalf("sem mudanças, nada a fazer: %+v", p.Steps)
	}
	os.WriteFile(filepath.Join(r.Root, "internal/llm/llm.go"), []byte("package llm\n\ntype Chat interface{ Send() }\n"), 0o644)
	s2, _ := ScanRepo(r)
	p2 := MakePlan(s2, old, 0)
	if len(p2.Steps) != 2 || p2.Steps[0].Label != "internal/llm" || p2.Reused != 2 {
		t.Fatalf("incremental: %+v reused=%d", p2.Steps, p2.Reused)
	}
	prompts = nil
	m2, _, err := gen(&prompts).Run(context.Background(), p2, func(Event) {})
	if err != nil || len(prompts) != 2 || len(m2.Folders) != 3 {
		t.Fatalf("err=%v chamadas=%d pastas=%d", err, len(prompts), len(m2.Folders))
	}
}

func TestChunks(t *testing.T) {
	f := Folder{Dir: "x", Files: []File{{Path: "a", content: make([]byte, 30)}, {Path: "b", content: make([]byte, 30)}, {Path: "c", content: make([]byte, 100)}}}
	cs := chunks(f, 50)
	if len(cs) != 3 || len(cs[2][0].content) <= 50 || len(f.Files[2].content) != 100 {
		t.Fatalf("partes: %d", len(cs))
	}
	if chunkLimit(16384) != 32768 || chunkLimit(0) != maxChunkBytes {
		t.Fatal("chunkLimit")
	}
}
