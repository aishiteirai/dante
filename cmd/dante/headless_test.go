package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/checks"
	"github.com/aishiteirai/dante/internal/config"
)

func TestReorder(t *testing.T) {
	got := strings.Join(reorder([]string{"main...feat", "--modelo", "x/y", "--ci", "--falhar-em=media"}), " ")
	if got != "--modelo x/y --ci --falhar-em=media main...feat" {
		t.Fatalf("reorder = %s", got)
	}
}

func TestQualityGate(t *testing.T) {
	media := []finding{{Severidade: "media"}}
	if failsGate(media, nil, "alta") || !failsGate(media, nil, "media") || !failsGate(media, nil, "MÉDIA") {
		t.Fatal("gravidade dos achados")
	}
	erro := []checks.Issue{{Error: true}}
	if failsGate(nil, erro, "alta") || !failsGate(nil, erro, "media") || !failsGate(nil, []checks.Issue{{}}, "baixa") {
		t.Fatal("problemas das ferramentas: erro conta como média, aviso como baixa")
	}
}

func TestHookDevolveAoClaude(t *testing.T) {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	hookDecision([]checks.Issue{{Path: "a.go", Line: 7, Tool: "go vet", Message: "Printf errado"}},
		[]finding{{Arquivo: "b.go", Linha: 3, Severidade: "baixa", Titulo: "nome ruim"}, {Arquivo: "c.go", Linha: 9, Severidade: "alta", Titulo: "bug"}})
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	var d struct{ Decision, Reason string }
	if err := json.Unmarshal(b, &d); err != nil || d.Decision != "block" || !strings.Contains(d.Reason, "a.go:7 (go vet)") ||
		!strings.Contains(d.Reason, "c.go:9 (ALTA) bug") || strings.Contains(d.Reason, "nome ruim") {
		t.Fatalf("saída = %s", b)
	}
	// Sem nada novo, o hook não escreve nada (o Claude termina normalmente).
	r, w, _ = os.Pipe()
	os.Stdout = w
	hookDecision(nil, []finding{{Severidade: "baixa"}})
	w.Close()
	os.Stdout = old
	if b, _ := io.ReadAll(r); len(b) != 0 {
		t.Fatalf("deveria ficar quieto: %s", b)
	}
}

func TestInstalarHookMantemOResto(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(m.repo.Root)
	os.MkdirAll(".claude", 0o755)
	os.WriteFile(filepath.Join(".claude", "settings.json"), []byte(`{"permissions":{"allow":["Bash(make test)"]}}`), 0o644)
	if installHook(config.Defaults(), false) != 0 || installHook(config.Defaults(), false) != 0 {
		t.Fatal("instalar")
	}
	b, _ := os.ReadFile(filepath.Join(".claude", "settings.json"))
	s := string(b)
	if !strings.Contains(s, `"Bash(make test)"`) || strings.Count(s, "review --hook") != 1 {
		t.Fatalf("settings = %s", s)
	}
}
