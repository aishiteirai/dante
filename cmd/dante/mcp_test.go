package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
)

func TestServidorMCP(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	r := gitRepo(t)
	os.WriteFile(filepath.Join(r.Root, "calc.py"), []byte("def total(xs):\n    return sum(xs)\n\nprint(total([1]))\n"), 0o644)
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(r.Root)
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dante_funcao","arguments":{"nome":"total"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"dante_verificar","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nada","arguments":{}}}`,
	}, "\n")
	var out bytes.Buffer
	runMCP(config.Defaults(), strings.NewReader(in), &out)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 { // a notificação não tem resposta
		t.Fatalf("respostas = %d:\n%s", len(lines), out.String())
	}
	var init struct {
		Result struct{ ProtocolVersion string } `json:"result"`
	}
	json.Unmarshal([]byte(lines[0]), &init)
	if init.Result.ProtocolVersion != "2025-03-26" {
		t.Fatalf("versão = %q", init.Result.ProtocolVersion)
	}
	type callResult struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	var f, v, x callResult
	json.Unmarshal([]byte(lines[2]), &f)
	json.Unmarshal([]byte(lines[3]), &v)
	json.Unmarshal([]byte(lines[4]), &x)
	if f.Result.IsError || !strings.Contains(f.Result.Content[0].Text, "calc.py:4  print(total([1]))") ||
		strings.Contains(f.Result.Content[0].Text, "O que faz") {
		t.Fatalf("dante_funcao = %+v", f)
	}
	if !v.Result.IsError || !strings.Contains(v.Result.Content[0].Text, "não estão permitidas") {
		t.Fatalf("dante_verificar sem permissão = %+v", v)
	}
	if !x.Result.IsError {
		t.Fatal("ferramenta desconhecida deveria ser erro")
	}
}

func TestRegras(t *testing.T) {
	t.Setenv("DANTE_HOME", t.TempDir())
	r := gitRepo(t)
	os.WriteFile(filepath.Join(r.Root, "package.json"), []byte(`{"name":"web","scripts":{"test":"vitest","lint":"biome check"},"dependencies":{"react":"19"}}`), 0o644)
	os.WriteFile(filepath.Join(r.Root, "CLAUDE.md"), []byte("# web\nNunca mexa em gerado/.\n"), 0o644)
	f := regrasFacts(r, "")
	for _, want := range []string{"scripts:\n  lint: biome check\n  test: vitest", "dependencies: react", `<arquivo_atual nome="CLAUDE.md">`, "Nunca mexa em gerado/"} {
		if !strings.Contains(f, want) {
			t.Errorf("fatos sem %q:\n%s", want, f)
		}
	}
	if got := cleanRegras("```markdown\n# web\n\ntexto\n```"); got != "# web\n\ntexto\n" {
		t.Fatalf("cleanRegras = %q", got)
	}
	dst, err := writeRegras(r.Root, "", "# novo\n")
	if err != nil || filepath.Base(dst) != "CLAUDE.md" {
		t.Fatal(err)
	}
	bak, _ := os.ReadFile(filepath.Join(os.Getenv("DANTE_HOME"), "pedidos", filepath.Base(r.Root)+"-CLAUDE.md.antes"))
	if !strings.Contains(string(bak), "Nunca mexa") {
		t.Fatal("a versão anterior deveria ficar guardada")
	}
	if _, err := writeRegras(r.Root, "../fora.md", "x"); err == nil {
		t.Fatal("só CLAUDE.md ou AGENTS.md")
	}
}
