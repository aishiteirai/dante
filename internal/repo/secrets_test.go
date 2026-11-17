package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/llm"
)

func TestBlocked(t *testing.T) {
	r := Repo{Root: "/x", Bloquear: []string{"config/prod.yml"}, Liberar: []string{"certs/publico.pem"}}
	bloqueados := []string{".env", "app/.env.production", "deploy/server.key", "home/.ssh/id_rsa", "infra/terraform.tfstate",
		"credentials.json", ".npmrc", "config/prod.yml", "prod.env"}
	liberados := []string{".env.example", "id_rsa.pub", "src/env.go", "credentials_test.go", "certs/publico.pem", "README.md", "src/keys.go"}
	for _, p := range bloqueados {
		if !r.Blocked(p) {
			t.Errorf("deveria bloquear %s", p)
		}
	}
	for _, p := range liberados {
		if r.Blocked(p) {
			t.Errorf("não deveria bloquear %s", p)
		}
	}
}

func TestRedact(t *testing.T) {
	in := strings.Join([]string{
		`ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123`,
		`aws = "AKIAIOSFODNN7EXAMPLE"`,
		`token: ghp_abcdefghijklmnopqrstuvwxyz0123456789AB`,
		`db_password = "s3nh4-muito-forte"`,
		`api_key = os.Getenv("API_KEY")`,
		`senha_curta = "abc"`,
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
		`texto normal com token de acesso`,
	}, "\n")
	out, n := Redact(in)
	for _, vazou := range []string{"sk-ant-api03", "AKIAIOSFODNN7", "ghp_abc", "s3nh4", "MIIEow"} {
		if strings.Contains(out, vazou) {
			t.Errorf("vazou %q:\n%s", vazou, out)
		}
	}
	for _, fica := range []string{`os.Getenv("API_KEY")`, `senha_curta = "abc"`, "texto normal com token de acesso", "db_password = \""} {
		if !strings.Contains(out, fica) {
			t.Errorf("deveria manter %q:\n%s", fica, out)
		}
	}
	if n != 5 {
		t.Errorf("mascarados = %d, esperava 5:\n%s", n, out)
	}
}

func TestFerramentasNaoVazamSegredos(t *testing.T) {
	r := setup(t)
	os.WriteFile(filepath.Join(r.Root, ".env"), []byte("SENHA=segredo-de-producao\n"), 0o644)
	os.WriteFile(filepath.Join(r.Root, "app.py"), []byte("chave = 'sk-proj-abcdefghijklmnopqrstuvwxyz'\nSENHA_LIDA = 1\n"), 0o644)
	run(t, r.Root, "git", "add", "-f", ".env", "app.py")
	run(t, r.Root, "git", "commit", "-qm", "segredos")
	exec := r.Executor(func() string { return r.Head() })
	call := func(name, args string) (string, bool) {
		return exec(context.Background(), llm.ToolCall{Name: name, Args: args})
	}
	if out, bad := call("read_file", `{"path":".env"}`); !bad || strings.Contains(out, "segredo-de-producao") {
		t.Fatalf("read_file .env = %q", out)
	}
	if out, bad := call("read_file", `{"path":"./.env","ref":"HEAD"}`); !bad || strings.Contains(out, "segredo-de-producao") {
		t.Fatalf("read_file .env@HEAD = %q", out)
	}
	out, _ := call("git_diff", `{}`)
	if strings.Contains(out, "segredo-de-producao") || !strings.Contains(out, "diff omitido: arquivo sensível") {
		t.Fatalf("git_diff vazou:\n%s", out)
	}
	if strings.Contains(out, "sk-proj-abc") || !strings.Contains(out, "mascarados") {
		t.Fatalf("git_diff não mascarou a chave:\n%s", out)
	}
	if out, _ := call("grep", `{"pattern":"SENHA"}`); strings.Contains(out, ".env") || !strings.Contains(out, "app.py") {
		t.Fatalf("grep = %q", out)
	}
	if out, _ := call("list_files", `{}`); !strings.Contains(out, ".env  (sensível") {
		t.Fatalf("list_files = %q", out)
	}
}
