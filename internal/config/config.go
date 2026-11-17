// Package config guarda provedores de modelos, a última escolha e os repositórios
// confiados. Arquivo: $XDG_CONFIG_HOME/dante/config.json (padrão ~/.config/dante).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/aishiteirai/dante/internal/llm"
)

type Provider struct {
	ID       string   `json:"id"`
	Nome     string   `json:"nome"`
	Tipo     llm.Kind `json:"tipo"`                     // "anthropic" ou "openai" (compatível)
	URL      string   `json:"url,omitempty"`            // base da API
	ChaveEnv string   `json:"chave_env,omitempty"`      // variável de ambiente com a chave
	Chave    string   `json:"chave,omitempty"`          // chave direto no arquivo (evite)
	Modelos  []string `json:"modelos,omitempty"`        // lista fixa
	Listar   bool     `json:"listar_modelos,omitempty"` // consulta GET /models (servidores locais)
	Local    bool     `json:"local,omitempty"`
}

func (p Provider) LLM() llm.Provider {
	return llm.Provider{ID: p.ID, Kind: p.Tipo, BaseURL: p.URL, APIKey: p.Chave, APIKeyEnv: p.ChaveEnv}
}

// HasKey informa se há credencial (servidores locais não precisam).
func (p Provider) HasKey() bool {
	if p.Local || p.Chave != "" {
		return true
	}
	if p.ChaveEnv != "" && os.Getenv(p.ChaveEnv) != "" {
		return true
	}
	// O SDK da Anthropic também aceita token OAuth e perfis do `ant auth login`.
	if p.Tipo == llm.KindAnthropic {
		home, _ := os.UserHomeDir()
		_, err := os.Stat(filepath.Join(home, ".config", "anthropic"))
		return os.Getenv("ANTHROPIC_AUTH_TOKEN") != "" || err == nil
	}
	return false
}

type Choice struct {
	Provedor string `json:"provedor"`
	Modelo   string `json:"modelo"`
}

type Config struct {
	Provedores   []Provider `json:"provedores"`
	UltimoModelo *Choice    `json:"ultimo_modelo,omitempty"`
	Esforco      string     `json:"esforco"`
	Confiados    []string   `json:"repositorios_confiados,omitempty"`
}

func Defaults() Config {
	return Config{
		Esforco: "high",
		Provedores: []Provider{
			{ID: "claude", Nome: "Claude (Anthropic)", Tipo: llm.KindAnthropic, ChaveEnv: "ANTHROPIC_API_KEY",
				Modelos: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}},
			{ID: "deepseek", Nome: "DeepSeek", Tipo: llm.KindOpenAI, URL: "https://api.deepseek.com/v1",
				ChaveEnv: "DEEPSEEK_API_KEY", Modelos: []string{"deepseek-chat", "deepseek-reasoner"}},
			{ID: "qwen", Nome: "Qwen (Alibaba Cloud)", Tipo: llm.KindOpenAI,
				URL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", ChaveEnv: "DASHSCOPE_API_KEY",
				Modelos: []string{"qwen3-coder-plus", "qwen-max", "qwen-plus"}},
			{ID: "ollama", Nome: "Ollama (local)", Tipo: llm.KindOpenAI, URL: "http://localhost:11434/v1",
				Listar: true, Local: true},
			{ID: "lmstudio", Nome: "LM Studio (local)", Tipo: llm.KindOpenAI, URL: "http://localhost:1234/v1",
				Listar: true, Local: true},
		},
	}
}

func Dir() string {
	if v := os.Getenv("DANTE_CONFIG"); v != "" {
		return v
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "dante")
}

func Path() string { return filepath.Join(Dir(), "config.json") }

// Load lê a configuração; na primeira execução grava os padrões para o usuário editar.
func Load() (Config, error) {
	b, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		c := Defaults()
		return c, c.Save()
	}
	if err != nil {
		return Defaults(), err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Defaults(), err
	}
	if len(c.Provedores) == 0 {
		c.Provedores = Defaults().Provedores
	}
	if c.Esforco == "" {
		c.Esforco = "high"
	}
	return c, nil
}

func (c Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(Path(), append(b, '\n'), 0o600)
}

func (c Config) Provider(id string) (Provider, bool) {
	for _, p := range c.Provedores {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

func (c Config) Trusted(root string) bool {
	for _, r := range c.Confiados {
		if r == root {
			return true
		}
	}
	return false
}

func (c *Config) Trust(root string) {
	if !c.Trusted(root) {
		c.Confiados = append(c.Confiados, root)
	}
}
