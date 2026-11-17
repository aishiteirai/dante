package config

import (
	"os"
	"testing"
)

func TestChavesSalvas(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DANTE_CONFIG", dir)
	t.Setenv("DEEPSEEK_API_KEY", "da-variavel")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := c.Provider("deepseek")
	if p.KeySource() != "env" || p.LLM().Key() != "da-variavel" {
		t.Fatalf("antes de salvar: %s %q", p.KeySource(), p.LLM().Key())
	}
	if err := c.SetKey("deepseek", "do-dante"); err != nil {
		t.Fatal(err)
	}
	p, _ = c.Provider("deepseek")
	if p.KeySource() != "dante" || p.LLM().Key() != "do-dante" {
		t.Fatalf("chave do dante deveria ter prioridade: %s %q", p.KeySource(), p.LLM().Key())
	}
	st, _ := os.Stat(CredentialsPath())
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("permissão = %v", st.Mode().Perm())
	}
	// Recarregar mantém a chave, e ela não vai para o config.json.
	c2, _ := Load()
	p, _ = c2.Provider("deepseek")
	if p.LLM().Key() != "do-dante" {
		t.Fatal("chave não persistiu")
	}
	_ = c2.Save()
	if b, _ := os.ReadFile(Path()); contains(string(b), "do-dante") {
		t.Fatal("a chave vazou para o config.json")
	}
	_ = c2.SetKey("deepseek", "")
	p, _ = c2.Provider("deepseek")
	if p.KeySource() != "env" {
		t.Fatalf("depois de remover: %s", p.KeySource())
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestMigracaoDeConfigAntigo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DANTE_CONFIG", dir)
	// Config de uma versão anterior: sem OpenRouter e sem sugestões do Ollama.
	antigo := `{"provedores":[{"id":"deepseek","nome":"DeepSeek","tipo":"openai","url":"https://api.deepseek.com/v1"},
	 {"id":"ollama","nome":"Ollama","tipo":"openai","url":"http://localhost:11434/v1","listar_modelos":true,"local":true}],
	 "esforco":"high"}`
	os.WriteFile(Path(), []byte(antigo), 0o600)
	c, _ := Load()
	if _, ok := c.Provider("openrouter"); !ok {
		t.Fatal("OpenRouter deveria ter sido acrescentado")
	}
	if o, _ := c.Provider("ollama"); len(o.Sugeridos) == 0 {
		t.Fatal("Ollama deveria ganhar as sugestões")
	}
	if o, _ := c.Provider("ollama"); o.Tipo != "ollama" || o.URL != "http://localhost:11434" || o.Contexto != 16384 {
		t.Fatalf("Ollama deveria migrar para a API nativa: %+v", o)
	}
	for _, id := range []string{"opencode", "opencode-go"} {
		if _, ok := c.Provider(id); !ok {
			t.Fatalf("%s deveria ter sido acrescentado ao config antigo", id)
		}
	}
	if d, _ := c.Provider("deepseek"); d.Contexto != 128000 {
		t.Fatalf("contexto do DeepSeek = %d", d.Contexto)
	}
	// Se o usuário apagar o OpenRouter, ele não volta.
	var rest []Provider
	for _, p := range c.Provedores {
		if p.ID != "openrouter" {
			rest = append(rest, p)
		}
	}
	c.Provedores = rest
	_ = c.Save()
	c2, _ := Load()
	if _, ok := c2.Provider("openrouter"); ok {
		t.Fatal("provedor removido pelo usuário voltou")
	}
}

func TestOrcamento(t *testing.T) {
	cases := map[int]int{0: DefaultTurnBudget, -1: 0, 50000: 50000}
	for in, want := range cases {
		if got := (Config{OrcamentoTokens: in}).TurnBudget(); got != want {
			t.Errorf("OrcamentoTokens=%d → %d, queria %d", in, got, want)
		}
	}
}

func TestOpenCode(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	t.Setenv("OPENCODE_API_KEY", "")
	c, _ := Load()
	console, ok1 := c.Provider("opencode")
	goSub, ok2 := c.Provider("opencode-go")
	if !ok1 || !ok2 {
		t.Fatal("provedores do OpenCode ausentes")
	}
	if l := goSub.LLM(); l.SessionHeader != "x-opencode-session" || l.BaseURL != "https://opencode.ai/inference/go/openai/v1" || l.ModelsURL == "" {
		t.Fatalf("OpenCode Go = %+v", l)
	}
	if console.HasKey() || goSub.HasKey() {
		t.Fatal("sem chave ainda")
	}
	// Uma chave cadastrada no Console vale também para o Go (e vice-versa).
	if err := c.SetKey(goSub.CredID(), "oc_sk_teste"); err != nil {
		t.Fatal(err)
	}
	console, _ = c.Provider("opencode")
	goSub, _ = c.Provider("opencode-go")
	if console.LLM().Key() != "oc_sk_teste" || goSub.LLM().Key() != "oc_sk_teste" || goSub.KeySource() != "dante" {
		t.Fatal("chave compartilhada não funcionou")
	}
}
