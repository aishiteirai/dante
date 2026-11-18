// Package config guarda provedores de modelos, a última escolha e os repositórios
// confiados. Arquivo: $XDG_CONFIG_HOME/dante/config.json (padrão ~/.config/dante).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

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
	Filtro    string   `json:"filtro,omitempty"`     // "gratis" (só preço zero), "sem_gratis" (tira os -free) ou "gemini"
	// Tamanho do contexto do modelo, em tokens. Guia a poda de resultados antigos; no
	// Ollama também define o num_ctx (mais contexto usa mais memória).
	Contexto int `json:"contexto,omitempty"`
	// Cabeçalho em que vai um ID estável por conversa (ex.: x-opencode-session).
	CabecalhoSessao string `json:"cabecalho_sessao,omitempty"`
	// Onde listar os modelos, quando não é url + "/models".
	URLModelos string `json:"url_modelos,omitempty"`
	// Usa a chave cadastrada de outro provedor (ex.: OpenCode Go usa a do OpenCode Console).
	ChaveDe   string     `json:"chave_de,omitempty"`
	Sugeridos []Sugestao `json:"sugeridos,omitempty"` // modelos para baixar (Ollama)
	// Modelo barato deste provedor, oferecido para gerar o mapa do projeto (/mapa).
	ModeloMapa string `json:"modelo_mapa,omitempty"`

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
	return llm.Provider{ID: p.ID, Kind: p.Tipo, BaseURL: p.URL, APIKey: p.key(), APIKeyEnv: p.ChaveEnv,
		SessionHeader: p.CabecalhoSessao, ModelsURL: p.URLModelos}
}

// CredID é o provedor sob o qual a chave fica salva (o próprio, ou o de chave_de).
func (p Provider) CredID() string {
	if p.ChaveDe != "" {
		return p.ChaveDe
	}
	return p.ID
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

// Sugestao é um modelo que o dante oferece para baixar num servidor local.
type Sugestao struct {
	Modelo  string `json:"modelo"`
	Tamanho string `json:"tamanho"`
	Nota    string `json:"nota,omitempty"`
}

type Choice struct {
	Provedor string `json:"provedor"`
	Modelo   string `json:"modelo"`
}

type Config struct {
	Provedores   []Provider `json:"provedores"`
	UltimoModelo *Choice    `json:"ultimo_modelo,omitempty"`
	MapaModelo   *Choice    `json:"mapa_modelo,omitempty"` // último modelo usado no /mapa
	Esforco      string     `json:"esforco"`
	Confiados    []string   `json:"repositorios_confiados,omitempty"`
	// Arquivos que o modelo não pode ler, além dos padrões do dante (.env, *.pem, id_rsa...),
	// e exceções a eles. Padrões glob, comparados com o nome ou, se tiverem "/", com o caminho.
	ArquivosBloqueados []string `json:"arquivos_bloqueados,omitempty"`
	ArquivosLiberados  []string `json:"arquivos_liberados,omitempty"`
	// Orçamento de tokens (entrada + saída de todas as rodadas) de cada pergunta ao modelo.
	// 0 usa o padrão (DefaultTurnBudget); -1 desliga o limite.
	OrcamentoTokens int `json:"orcamento_tokens,omitempty"`
	// O que o agente pode fazer no modo editar, por ação ("editar", "criar", "apagar"):
	// "perguntar", "permitir" ou "negar". Faltando, valem os padrões de DefaultPermissions.
	Permissoes map[string]string `json:"permissoes,omitempty"`
	// Provedores padrão que este config já conhece; os novos são acrescentados ao carregar.
	PadroesConhecidos []string `json:"padroes_conhecidos,omitempty"`
}

// DefaultTurnBudget cobre com folga a revisão de um commit grande: com o diff na mensagem
// e no máximo duas rodadas de leitura, uma revisão típica fica em 10 a 30 mil tokens.
const DefaultTurnBudget = 100000

// TurnBudget devolve o orçamento por pergunta (0 = sem limite).
func (c Config) TurnBudget() int {
	switch {
	case c.OrcamentoTokens < 0:
		return 0
	case c.OrcamentoTokens == 0:
		return DefaultTurnBudget
	}
	return c.OrcamentoTokens
}

func Defaults() Config {
	return Config{
		Esforco: "high",
		Provedores: []Provider{
			{ID: "claude", Nome: "Claude (Anthropic)", Tipo: llm.KindAnthropic, ChaveEnv: "ANTHROPIC_API_KEY",
				SiteChave: "https://platform.claude.com/settings/keys",
				Modelos:   []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}, ModeloMapa: "claude-haiku-5-5"},
			{ID: "deepseek", Nome: "DeepSeek", Tipo: llm.KindOpenAI, URL: "https://api.deepseek.com/v1",
				ChaveEnv: "DEEPSEEK_API_KEY", SiteChave: "https://platform.deepseek.com/api_keys", Contexto: 128000, Modelos: []string{"deepseek-chat", "deepseek-reasoner"}, ModeloMapa: "deepseek-chat"},
			{ID: "qwen", Nome: "Qwen (Alibaba Cloud)", Tipo: llm.KindOpenAI,
				URL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", ChaveEnv: "DASHSCOPE_API_KEY",
				SiteChave: "Alibaba Cloud Model Studio → API Key", Contexto: 128000,
				Modelos: []string{"qwen3-coder-plus", "qwen-max", "qwen-plus"}, ModeloMapa: "qwen-plus"},
			// Gemini pela API compatível com OpenAI. A chave do Google AI Studio tem uma camada
			// gratuita com limite de uso; nela, o Google pode usar os dados para melhorar os
			// produtos dele.
			{ID: "gemini", Nome: "Gemini (Google)", Tipo: llm.KindOpenAI,
				URL: "https://generativelanguage.googleapis.com/v1beta/openai", ChaveEnv: "GEMINI_API_KEY",
				SiteChave: "https://aistudio.google.com/apikey", Listar: true, Filtro: "gemini", Contexto: 1000000,
				Modelos: []string{"gemini-3.1-pro-preview", "gemini-2.5-flash"}, ModeloMapa: "gemini-2.5-flash"},
			{ID: "openrouter", Nome: "OpenRouter · modelos grátis", Tipo: llm.KindOpenAI,
				URL: "https://openrouter.ai/api/v1", ChaveEnv: "OPENROUTER_API_KEY",
				SiteChave: "https://openrouter.ai/settings/keys", Listar: true, Filtro: "gratis", Contexto: 32000},
			// OpenCode Console e Go: fora do próprio opencode só funcionam os modelos pagos e
			// os do Go; os "-free" não. Ver https://opencode.ai/docs/console/models.
			{ID: "opencode", Nome: "OpenCode Console · pago por uso", Tipo: llm.KindOpenAI,
				URL: "https://opencode.ai/inference/openai/v1", URLModelos: "https://opencode.ai/inference/v1/models",
				ChaveEnv: "OPENCODE_API_KEY", SiteChave: "https://opencode.ai/console → Account → Personal API keys",
				Listar: true, Filtro: "sem_gratis", Contexto: 128000, CabecalhoSessao: "x-opencode-session",
				Modelos: []string{"deepseek-v4-pro", "deepseek-v4.1-flash", "qwen3.8-max", "kimi-k3", "glm-5.3", "minimax-m3"}, ModeloMapa: "deepseek-v4.1-flash"},
			{ID: "opencode-go", Nome: "OpenCode Go · assinatura", Tipo: llm.KindOpenAI,
				URL: "https://opencode.ai/inference/go/openai/v1", URLModelos: "https://opencode.ai/inference/go/v1/models",
				ChaveEnv: "OPENCODE_API_KEY", ChaveDe: "opencode", SiteChave: "https://opencode.ai/console → assine o Go → Personal API keys",
				Listar: true, Filtro: "sem_gratis", Contexto: 128000, CabecalhoSessao: "x-opencode-session",
				Modelos: []string{"kimi-k3", "deepseek-v4-pro", "deepseek-v4.1-flash", "minimax-m3", "glm-5.3", "qwen3.8-max"}, ModeloMapa: "deepseek-v4.1-flash"},
			{ID: "ollama", Nome: "Ollama (local, grátis)", Tipo: llm.KindOllama, URL: "http://localhost:11434",
				Listar: true, Local: true, Contexto: 16384, Sugeridos: []Sugestao{
					{Modelo: "qwen2.5-coder:7b", Tamanho: "4,7 GB", Nota: "bom para começar; ~8 GB de RAM"},
					{Modelo: "qwen3:8b", Tamanho: "5,2 GB", Nota: "raciocina antes de responder"},
					{Modelo: "deepseek-r1:8b", Tamanho: "5,2 GB", Nota: "DeepSeek local"},
					{Modelo: "qwen2.5-coder:14b", Tamanho: "9,0 GB", Nota: "revisões melhores; ~16 GB de RAM"},
					{Modelo: "qwen3-coder:30b", Tamanho: "19 GB", Nota: "o mais forte; pede GPU ou 32 GB de RAM"},
				}},
			{ID: "lmstudio", Nome: "LM Studio (local, grátis)", Tipo: llm.KindOpenAI, URL: "http://localhost:1234/v1",
				Listar: true, Local: true, Contexto: 8192},
		},
	}
}

// mergeDefaults acrescenta provedores padrão novos e completa campos de configs antigos.
func (c *Config) mergeDefaults() (changed bool) {
	known := map[string]bool{}
	for _, id := range c.PadroesConhecidos {
		known[id] = true
	}
	for _, d := range Defaults().Provedores {
		i := -1
		for j, p := range c.Provedores {
			if p.ID == d.ID {
				i = j
			}
		}
		switch {
		case i < 0 && !known[d.ID]: // provedor novo nesta versão
			c.Provedores = append(c.Provedores, d)
			changed = true
		case i >= 0:
			p := &c.Provedores[i]
			// O Ollama passou a usar a API nativa, que aceita num_ctx.
			if p.ID == "ollama" && p.Tipo == llm.KindOpenAI && strings.Contains(p.URL, ":11434") {
				p.Tipo, p.URL, changed = llm.KindOllama, strings.TrimSuffix(strings.TrimSuffix(p.URL, "/"), "/v1"), true
			}
			if p.Contexto == 0 && d.Contexto > 0 {
				p.Contexto, changed = d.Contexto, true
			}
			// O Google descontinuou o gemini-2.5-pro para contas novas: troca a lista antiga.
			if p.ID == "gemini" && len(p.Modelos) > 0 && p.Modelos[0] == "gemini-2.5-pro" {
				p.Modelos, changed = d.Modelos, true
			}
			if p.ModeloMapa == "" && d.ModeloMapa != "" {
				p.ModeloMapa, changed = d.ModeloMapa, true
			}
			if p.SiteChave == "" && d.SiteChave != "" {
				p.SiteChave, changed = d.SiteChave, true
			}
			if len(p.Sugeridos) == 0 && len(d.Sugeridos) > 0 && !known[d.ID] {
				p.Sugeridos, changed = d.Sugeridos, true
			}
		}
		if !known[d.ID] {
			c.PadroesConhecidos = append(c.PadroesConhecidos, d.ID)
			changed = true
		}
	}
	return changed
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
		c.Provedores[i].salva = creds[c.Provedores[i].CredID()]
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
		c.mergeDefaults()
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
	if c.mergeDefaults() {
		_ = c.Save()
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

// Valores de permissão de escrita do agente.
const (
	PermAsk   = "perguntar"
	PermAllow = "permitir"
	PermDeny  = "negar"
)

// PermissionOps são as ações de escrita, na ordem em que aparecem na interface.
var PermissionOps = []string{"editar", "criar", "apagar"}

// DefaultPermissions: editar e criar pedem aprovação; apagar é negado.
var DefaultPermissions = map[string]string{"editar": PermAsk, "criar": PermAsk, "apagar": PermDeny}

// Permission devolve a permissão de uma ação de escrita.
func (c Config) Permission(op string) string {
	switch v := c.Permissoes[op]; v {
	case PermAsk, PermAllow, PermDeny:
		return v
	}
	if v, ok := DefaultPermissions[op]; ok {
		return v
	}
	return PermDeny
}

// SetPermission muda a permissão de uma ação.
func (c *Config) SetPermission(op, v string) {
	if c.Permissoes == nil {
		c.Permissoes = map[string]string{}
	}
	c.Permissoes[op] = v
}
