// Package llm conversa com modelos de linguagem de vários provedores (Claude via API
// da Anthropic; DeepSeek, Qwen, Ollama e outros via API compatível com OpenAI) e roda o
// laço de ferramentas: o modelo pede uma ferramenta, o app executa e devolve o resultado.
package llm

import (
	"context"
	"fmt"
	"os"
)

// Tool descreve uma ferramenta oferecida ao modelo. Params é um JSON Schema de objeto.
type Tool struct {
	Name        string
	Description string
	Params      map[string]any // {"type":"object","properties":{...},"required":[...]}
}

type ToolCall struct {
	ID   string
	Name string
	Args string // JSON
}

type EventKind int

const (
	EvText       EventKind = iota // trecho da resposta
	EvReasoning                   // trecho de raciocínio (modelos que expõem, ex.: deepseek-reasoner)
	EvToolCall                    // o modelo pediu uma ferramenta
	EvToolResult                  // resultado da ferramenta
	EvNotice                      // aviso (fallback de modelo, limite de tokens...)
)

type Event struct {
	Kind    EventKind
	Text    string
	Call    ToolCall
	IsError bool
}

// Executor roda uma ferramenta e devolve o texto do resultado (isError marca falha).
type Executor func(ctx context.Context, call ToolCall) (result string, isError bool)

type Usage struct {
	InputTokens, OutputTokens int64
}

// Chat é uma conversa com histórico próprio do provedor.
type Chat interface {
	// Send envia a mensagem do usuário e roda o laço de ferramentas até a resposta final.
	Send(ctx context.Context, user string, emit func(Event)) (Usage, error)
}

type Kind string

const (
	KindAnthropic Kind = "anthropic"
	KindOpenAI    Kind = "openai"
)

// Provider é a configuração de conexão de um provedor.
type Provider struct {
	ID        string
	Kind      Kind
	BaseURL   string
	APIKey    string
	APIKeyEnv string
}

func (p Provider) Key() string {
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.APIKeyEnv != "" {
		return os.Getenv(p.APIKeyEnv)
	}
	return ""
}

type Options struct {
	Model     string
	System    string
	Tools     []Tool
	Exec      Executor
	Effort    string // só Claude: low, medium, high, xhigh, max
	MaxTokens int64
	MaxSteps  int // máximo de rodadas de ferramenta por mensagem
}

func New(p Provider, o Options) (Chat, error) {
	if o.MaxSteps == 0 {
		o.MaxSteps = 40
	}
	switch p.Kind {
	case KindAnthropic:
		return newAnthropic(p, o), nil
	case KindOpenAI:
		return newOpenAI(p, o), nil
	}
	return nil, fmt.Errorf("tipo de provedor desconhecido: %q", p.Kind)
}

// Parâmetros da ferramenta separados em properties/required (formato da Anthropic).
func splitSchema(s map[string]any) (props any, required []string) {
	props = s["properties"]
	if r, ok := s["required"].([]string); ok {
		required = r
	}
	return
}
