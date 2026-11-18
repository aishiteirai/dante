package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// Modelos que aceitam o fallback automático do servidor quando o pedido é recusado.
var fallbackModels = map[string]bool{
	"claude-fable-5-2": true, "claude-fable-5-1": true,
	"claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-5-5": true,
}

type anthropicChat struct {
	client   anthropic.Client
	opts     Options
	tools    []anthropic.BetaToolUnionParam
	messages []anthropic.BetaMessageParam
}

func newAnthropic(p Provider, o Options) *anthropicChat {
	var ro []option.RequestOption
	if k := p.Key(); k != "" {
		ro = append(ro, option.WithAPIKey(k))
	}
	if p.BaseURL != "" {
		ro = append(ro, option.WithBaseURL(p.BaseURL))
	}
	c := &anthropicChat{client: anthropic.NewClient(ro...), opts: o}
	for _, t := range o.Tools {
		props, req := splitSchema(t.Params)
		c.tools = append(c.tools, anthropic.BetaToolUnionParam{OfTool: &anthropic.BetaToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.BetaToolInputSchemaParam{Properties: props, Required: req},
			// Entrada da ferramenta chega já em streaming; o Executor valida o JSON.
			EagerInputStreaming: anthropic.Bool(true),
		}})
	}
	return c
}

func (c *anthropicChat) params() anthropic.BetaMessageNewParams {
	maxTok := c.opts.MaxTokens
	if maxTok == 0 {
		maxTok = 32000
	}
	p := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.opts.Model),
		MaxTokens: maxTok,
		Messages:  c.messages,
		Tools:     c.tools,
		Thinking:  anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{}},
		// Cache automático no último bloco: cada rodada reaproveita o histórico anterior.
		CacheControl: anthropic.BetaCacheControlEphemeralParam{},
	}
	if c.opts.System != "" {
		p.System = []anthropic.BetaTextBlockParam{{Text: c.opts.System}}
	}
	if c.opts.Effort != "" {
		p.OutputConfig = anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(c.opts.Effort)}
	}
	// Conversas longas: o servidor limpa resultados antigos de ferramentas. Não conta como
	// edição do histórico (o pensamento preservado continua válido), e o histórico do lado
	// do dante continua só crescendo.
	p.ContextManagement = anthropic.BetaContextManagementConfigParam{
		Edits: []anthropic.BetaContextManagementConfigEditUnionParam{{
			OfClearToolUses20250919: &anthropic.BetaClearToolUses20250919EditParam{
				Trigger: anthropic.BetaClearToolUses20250919EditTriggerUnionParam{
					OfInputTokens: &anthropic.BetaInputTokensTriggerParam{Value: c.clearTrigger()},
				},
				Keep:         anthropic.BetaToolUsesKeepParam{Value: 6},
				ClearAtLeast: anthropic.BetaInputTokensClearAtLeastParam{Value: 20000},
			},
		}},
	}
	p.Betas = append(p.Betas, anthropic.AnthropicBetaContextManagement2025_06_27)
	if fallbackModels[c.opts.Model] {
		p.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		p.Betas = append(p.Betas, anthropic.AnthropicBetaServerSideFallback2026_07_01)
	}
	return p
}

func (c *anthropicChat) Send(ctx context.Context, user string, emit func(Event)) (Usage, error) {
	var usage Usage
	// Histórico só cresce no fim: em caso de erro, desfaz esta mensagem inteira.
	start := len(c.messages)
	c.messages = append(c.messages, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(user)))
	fail := func(err error) (Usage, error) {
		c.messages = c.messages[:start]
		return usage, err
	}

	guard := newGuard(ctx, c.opts.TurnBudget)
	for step := 0; step < c.opts.MaxSteps; step++ {
		stream := c.client.Beta.Messages.NewStreaming(ctx, c.params())
		msg := anthropic.BetaMessage{}
		for stream.Next() {
			ev := stream.Current()
			if err := msg.Accumulate(ev); err != nil {
				return fail(err)
			}
			if d, ok := ev.AsAny().(anthropic.BetaRawContentBlockDeltaEvent); ok {
				if t, ok := d.Delta.AsAny().(anthropic.BetaTextDelta); ok && t.Text != "" {
					emit(Event{Kind: EvText, Text: t.Text})
				}
			}
		}
		if err := stream.Err(); err != nil {
			return fail(anthropicError(err))
		}
		u := Usage{InputTokens: msg.Usage.InputTokens + msg.Usage.CacheReadInputTokens + msg.Usage.CacheCreationInputTokens,
			OutputTokens: msg.Usage.OutputTokens, CachedTokens: msg.Usage.CacheReadInputTokens, Rounds: 1}
		usage.Add(u)
		guard.add(u)
		c.messages = append(c.messages, msg.ToParam())

		switch msg.StopReason {
		case anthropic.BetaStopReasonRefusal:
			emit(Event{Kind: EvNotice, Text: "o modelo recusou o pedido", IsError: true})
			return usage, nil
		case anthropic.BetaStopReasonMaxTokens:
			emit(Event{Kind: EvNotice, Text: "resposta cortada no limite de tokens", IsError: true})
			return usage, nil
		case anthropic.BetaStopReasonToolUse:
		default:
			return usage, nil
		}

		var names []string
		for _, b := range msg.Content {
			if tu, ok := b.AsAny().(anthropic.BetaToolUseBlock); ok {
				names = append(names, tu.Name)
			}
		}
		v := guard.check(names, emit)
		var results []anthropic.BetaContentBlockParamUnion
		for _, b := range msg.Content {
			tu, ok := b.AsAny().(anthropic.BetaToolUseBlock)
			if !ok {
				continue
			}
			call := ToolCall{ID: tu.ID, Name: tu.Name, Args: tu.JSON.Input.Raw()}
			if v.blocked(guard, call.Name) { // todo tool_use precisa de um tool_result, mesmo sem executar
				results = append(results, anthropic.NewBetaToolResultBlock(tu.ID, v.block, true))
				continue
			}
			emit(Event{Kind: EvToolCall, Call: call})
			out, isErr := c.opts.Exec(ctx, call)
			emit(Event{Kind: EvToolResult, Call: call, Text: out, IsError: isErr})
			results = append(results, anthropic.NewBetaToolResultBlock(tu.ID, out, isErr))
		}
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		c.messages = append(c.messages, anthropic.NewBetaUserMessage(results...))
		if v.stop {
			return usage, nil
		}
	}
	emit(Event{Kind: EvNotice, Text: fmt.Sprintf("parei após %d rodadas de ferramentas", c.opts.MaxSteps), IsError: true})
	return usage, nil
}

// Limpa os resultados antigos a partir de 150 mil tokens (ou 60% do contexto informado).
func (c *anthropicChat) clearTrigger() int64 {
	if c.opts.ContextTokens > 0 {
		return int64(c.opts.ContextTokens) * 6 / 10
	}
	return 150000
}

func anthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401:
			return fmt.Errorf("chave da Anthropic inválida ou ausente (defina ANTHROPIC_API_KEY)")
		case 404:
			return fmt.Errorf("modelo não encontrado: %s", strings.TrimSpace(apiErr.Error()))
		case 429:
			return fmt.Errorf("limite de requisições da Anthropic atingido; tente de novo em instantes")
		}
		if apiErr.StatusCode >= 500 {
			return fmt.Errorf("a API da Anthropic está instável (HTTP %d); tente de novo", apiErr.StatusCode)
		}
	}
	return err
}
