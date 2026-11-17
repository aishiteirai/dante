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
