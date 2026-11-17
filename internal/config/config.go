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
	ID        string   `json:"id"`
	Nome      string   `json:"nome"`
	Tipo      llm.Kind `json:"tipo"`                     // "anthropic" ou "openai" (compatível)
	URL       string   `json:"url,omitempty"`            // base da API
	ChaveEnv  string   `json:"chave_env,omitempty"`      // variável de ambiente com a chave
	Chave     string   `json:"chave,omitempty"`          // chave direto no arquivo (evite)
	Modelos   []string `json:"modelos,omitempty"`        // lista fixa
	Listar    bool     `json:"listar_modelos,omitempty"` // consulta GET /models (servidores locais)
	Local     bool     `json:"local,omitempty"`
	SiteChave string   `json:"site_chave,omitempty"` // onde gerar a chave

	salva string // chave cadastrada pelo dante (credenciais.json)
}

// Ordem: chave no config.json, chave cadastrada no dante, variável de ambiente.
func (p Provider) key() string {
	if p.Chave != "" {
		return p.Chave
	}
	return p.salva
}

func (p Provider) LLM() llm.Provider {
	return llm.Provider{ID: p.ID, Kind: p.Tipo, BaseURL: p.URL, APIKey: p.key(), APIKeyEnv: p.ChaveEnv}
}

// WithKey devolve uma cópia do provedor usando esta chave (para testar antes de salvar).
func (p Provider) WithKey(k string) Provider {
	p.Chave, p.salva = "", k
	return p
}

// KeySource diz de onde vem a chave: "config", "dante", "env" ou "" (nenhuma).
func (p Provider) KeySource() string {
	switch {
	case p.Chave != "":
		return "config"
	case p.salva != "":
		return "dante"
	case p.ChaveEnv != "" && os.Getenv(p.ChaveEnv) != "":
		return "env"
	case p.Tipo == llm.KindAnthropic && anthropicLogin():
		return "ant"
	}
	return ""
}

// O SDK da Anthropic também aceita token OAuth e o perfil do `ant auth login`.
func anthropicLogin() bool {
	if os.Getenv("ANTHROPIC_AUTH_TOKEN") != "" {
		return true
	}
	home, _ := os.UserHomeDir()
	_, err := os.Stat(filepath.Join(home, ".config", "anthropic"))
	return err == nil
}

// HasKey informa se há credencial (servidores locais não precisam).
func (p Provider) HasKey() bool {
	return p.Local || p.KeySource() != ""
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
				SiteChave: "https://platform.claude.com/settings/keys",
				Modelos:   []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}},
			{ID: "deepseek", Nome: "DeepSeek", Tipo: llm.KindOpenAI, URL: "https://api.deepseek.com/v1",
				ChaveEnv: "DEEPSEEK_API_KEY", SiteChave: "https://platform.deepseek.com/api_keys", Modelos: []string{"deepseek-chat", "deepseek-reasoner"}},
			{ID: "qwen", Nome: "Qwen (Alibaba Cloud)", Tipo: llm.KindOpenAI,
				URL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", ChaveEnv: "DASHSCOPE_API_KEY",
				SiteChave: "Alibaba Cloud Model Studio → API Key",
				Modelos:   []string{"qwen3-coder-plus", "qwen-max", "qwen-plus"}},
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

// ---- credenciais: chaves cadastradas pelo dante, fora do config.json ----

func CredentialsPath() string { return filepath.Join(Dir(), "credenciais.json") }

func loadCredentials() map[string]string {
	creds := map[string]string{}
	if b, err := os.ReadFile(CredentialsPath()); err == nil {
		_ = json.Unmarshal(b, &creds)
	}
	return creds
}

func (c *Config) applyCredentials() {
	creds := loadCredentials()
	for i := range c.Provedores {
		c.Provedores[i].salva = creds[c.Provedores[i].ID]
	}
}

// SetKey grava (ou remove, com chave vazia) a chave de um provedor. O arquivo só é
// legível pelo usuário (0600), numa pasta 0700.
func (c *Config) SetKey(providerID, key string) error {
	creds := loadCredentials()
	if key == "" {
		delete(creds, providerID)
	} else {
		creds[providerID] = key
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	_ = os.Chmod(Dir(), 0o700)
	b, _ := json.MarshalIndent(creds, "", "  ")
	tmp := CredentialsPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, CredentialsPath()); err != nil {
		return err
	}
	c.applyCredentials()
	return nil
}

// Load lê a configuração; na primeira execução grava os padrões para o usuário editar.
func Load() (Config, error) {
	b, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		c := Defaults()
		c.applyCredentials()
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
	// Completa o site da chave em configs antigos.
	for i, p := range c.Provedores {
		if d, ok := Defaults().Provider(p.ID); ok && p.SiteChave == "" {
			c.Provedores[i].SiteChave = d.SiteChave
		}
	}
	c.applyCredentials()
	if c.Esforco == "" {
		c.Esforco = "high"
	}
	return c, nil
}

func (c Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
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
