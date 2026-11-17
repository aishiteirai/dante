package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Cliente para APIs compatíveis com o Chat Completions da OpenAI: DeepSeek, Qwen
// (DashScope), Ollama, LM Studio, vLLM, OpenRouter...

type oaMessage struct {
	Role             string       `json:"role"`
	Content          *string      `json:"content"`
	ReasoningContent string       `json:"reasoning_content,omitempty"`
	ToolCalls        []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type oaChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"` // Ollama/OpenRouter
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type openAIChat struct {
	p        Provider
	opts     Options
	http     *http.Client
	tools    []oaTool
	messages []oaMessage
	budget   *budget
	session  string
}

func newOpenAI(p Provider, o Options) *openAIChat {
	c := &openAIChat{p: p, opts: o, http: &http.Client{Timeout: 15 * time.Minute}, budget: newBudget(o.ContextTokens), session: newSessionID()}
	if o.System != "" {
		c.messages = append(c.messages, oaMessage{Role: "system", Content: ptr(o.System)})
	}
	for _, t := range o.Tools {
		var ot oaTool
		ot.Type = "function"
		ot.Function.Name, ot.Function.Description, ot.Function.Parameters = t.Name, t.Description, t.Params
		c.tools = append(c.tools, ot)
	}
	return c
}

func ptr(s string) *string { return &s }

func (c *openAIChat) Send(ctx context.Context, user string, emit func(Event)) (Usage, error) {
	var usage Usage
	start := len(c.messages)
	c.messages = append(c.messages, oaMessage{Role: "user", Content: ptr(user)})
	fail := func(err error) (Usage, error) {
		c.messages = c.messages[:start]
		return usage, err
	}

	guard := newGuard(c.opts.TurnBudget)
	for step := 0; step < c.opts.MaxSteps; step++ {
		bytesSent := c.fit(start, emit)
		msg, finish, u, err := c.complete(ctx, emit)
		guard.add(u)
		usage.InputTokens += u.InputTokens
		usage.OutputTokens += u.OutputTokens
		if err != nil {
			return fail(err)
		}
		c.budget.calibrate(u.InputTokens, bytesSent)
		c.messages = append(c.messages, msg)
		if len(msg.ToolCalls) == 0 {
			if finish == "length" {
				emit(Event{Kind: EvNotice, Text: "resposta cortada no limite de tokens", IsError: true})
			}
			return usage, nil
		}
		run, stop := guard.check(emit)
		for _, tc := range msg.ToolCalls {
			call := ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments}
			if !run {
				c.messages = append(c.messages, oaMessage{Role: "tool", ToolCallID: tc.ID, Content: ptr("ERRO: " + budgetResult)})
				continue
			}
			emit(Event{Kind: EvToolCall, Call: call})
			out, isErr := c.opts.Exec(ctx, call)
			emit(Event{Kind: EvToolResult, Call: call, Text: out, IsError: isErr})
			if isErr {
				out = "ERRO: " + out
			}
			c.messages = append(c.messages, oaMessage{Role: "tool", ToolCallID: tc.ID, Content: ptr(c.budget.clip(out))})
		}
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		if stop {
			return usage, nil
		}
	}
	emit(Event{Kind: EvNotice, Text: fmt.Sprintf("parei após %d rodadas de ferramentas", c.opts.MaxSteps), IsError: true})
	return usage, nil
}

// fit poda o histórico antes de cada requisição; devolve o tamanho enviado, em bytes.
func (c *openAIChat) fit(current int, emit func(Event)) int {
	// O raciocínio de rodadas anteriores não serve mais ao modelo (o DeepSeek o ignora).
	for i := 0; i < current; i++ {
		c.messages[i].ReasoningContent = ""
	}
	size := func(i int) int { b, _ := json.Marshal(c.messages[i]); return len(b) }
	total := 0
	for i := range c.messages {
		total += size(i)
	}
	tb, _ := json.Marshal(c.tools)
	total += len(tb)
	removed, total := c.budget.prune(len(c.messages), current, total, size,
		func(i int) bool {
			m := c.messages[i]
			return m.Role == "tool" && m.Content != nil && *m.Content != prunedResult
		},
		func(i int) { c.messages[i].Content = ptr(prunedResult) })
	for _, ev := range c.budget.notices(removed, total) {
		emit(ev)
	}
	return total
}

// complete faz uma requisição em streaming e devolve a mensagem do assistente montada.
func (c *openAIChat) complete(ctx context.Context, emit func(Event)) (oaMessage, string, Usage, error) {
	var usage Usage
	body := map[string]any{
		"model":          c.opts.Model,
		"messages":       c.messages,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if len(c.tools) > 0 {
		body["tools"] = c.tools
	}
	if c.opts.MaxTokens > 0 {
		body["max_tokens"] = c.opts.MaxTokens
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(c.p.BaseURL, "/")+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return oaMessage{}, "", usage, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", UserAgent)
	if c.p.SessionHeader != "" {
		req.Header.Set(c.p.SessionHeader, c.session)
	}
	if k := c.p.Key(); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return oaMessage{}, "", usage, fmt.Errorf("não consegui conectar em %s: %w", c.p.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return oaMessage{}, "", usage, httpError(resp.StatusCode, b, c.p)
	}

	var content, reasoning strings.Builder
	type partial struct{ id, name, args strings.Builder }
	calls := map[int]*partial{}
	finish := ""

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch oaChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			continue
		}
		if ch.Error != nil {
			return oaMessage{}, "", usage, fmt.Errorf("erro do provedor: %s", ch.Error.Message)
		}
		if ch.Usage != nil {
			usage = Usage{InputTokens: ch.Usage.PromptTokens, OutputTokens: ch.Usage.CompletionTokens}
		}
		for _, choice := range ch.Choices {
			d := choice.Delta
			if d.Content != "" {
				content.WriteString(d.Content)
				emit(Event{Kind: EvText, Text: d.Content})
			}
			if r := d.ReasoningContent + d.Reasoning; r != "" {
				reasoning.WriteString(r)
				emit(Event{Kind: EvReasoning, Text: r})
			}
			for _, tc := range d.ToolCalls {
				p := calls[tc.Index]
				if p == nil {
					p = &partial{}
					calls[tc.Index] = p
				}
				p.id.WriteString(tc.ID)
				p.name.WriteString(tc.Function.Name)
				p.args.WriteString(tc.Function.Arguments)
			}
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return oaMessage{}, "", usage, err
	}

	msg := oaMessage{Role: "assistant", ReasoningContent: reasoning.String()}
	if content.Len() > 0 || len(calls) == 0 {
		msg.Content = ptr(content.String())
	}
	idx := make([]int, 0, len(calls))
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for n, i := range idx {
		p := calls[i]
		var tc oaToolCall
		tc.ID, tc.Type = p.id.String(), "function"
		if tc.ID == "" { // alguns servidores locais não mandam id
			tc.ID = fmt.Sprintf("call_%d_%d", len(c.messages), n)
		}
		tc.Function.Name = p.name.String()
		tc.Function.Arguments = p.args.String()
		if tc.Function.Arguments == "" {
			tc.Function.Arguments = "{}"
		}
		msg.ToolCalls = append(msg.ToolCalls, tc)
	}
	return msg, finish, usage, nil
}

func httpError(status int, body []byte, p Provider) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Error.Message
	if msg == "" {
		msg = e.Message
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	switch status {
	case 401, 403:
		hint := ""
		if p.APIKeyEnv != "" {
			hint = fmt.Sprintf(" (defina %s)", p.APIKeyEnv)
		}
		return fmt.Errorf("chave de API recusada pelo provedor%s: %s", hint, msg)
	case 404:
		if strings.Contains(strings.ToLower(msg), "data policy") || strings.Contains(strings.ToLower(msg), "privacy") {
			return fmt.Errorf("o OpenRouter bloqueou este modelo pela sua política de dados; os modelos grátis "+
				"exigem liberar o uso dos dados em https://openrouter.ai/settings/privacy (%s)", msg)
		}
		return fmt.Errorf("modelo ou endpoint não encontrado: %s", msg)
	case 429:
		return fmt.Errorf("limite de requisições atingido: %s", msg)
	}
	return fmt.Errorf("HTTP %d: %s", status, msg)
}

// Filtros de ListModels.
const (
	FilterNone    = ""
	FilterFree    = "gratis"     // só preço zero com ferramentas (formato do OpenRouter)
	FilterNotFree = "sem_gratis" // tira os "-free" (no OpenCode, só funcionam dentro do opencode)
)

// ListModels consulta GET /models (Ollama, LM Studio, vLLM, OpenRouter, OpenCode...),
// aplica o filtro e põe DeepSeek e Qwen primeiro.
func ListModels(ctx context.Context, p Provider, filter string) ([]string, error) {
	onlyFree := filter == FilterFree
	if p.Kind == KindOllama {
		return listOllama(ctx, p)
	}
	url := strings.TrimSuffix(p.BaseURL, "/") + "/models"
	if p.ModelsURL != "" {
		url = p.ModelsURL
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if k := p.Key(); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var ids []string
	for _, d := range out.Data {
		if onlyFree {
			if d.Pricing == nil || !isZero(d.Pricing.Prompt) || !isZero(d.Pricing.Completion) {
				continue
			}
			if d.SupportedParameters != nil && !contains(d.SupportedParameters, "tools") {
				continue // sem ferramentas o modelo não consegue abrir arquivos
			}
		}
		if filter == FilterNotFree && strings.HasSuffix(d.ID, "-free") {
			continue
		}
		ids = append(ids, d.ID)
	}
	rank := func(id string) int {
		l := strings.ToLower(id)
		switch {
		case strings.Contains(l, "qwen") && strings.Contains(l, "coder"):
			return 0
		case strings.Contains(l, "deepseek"):
			return 1
		case strings.Contains(l, "qwen"):
			return 2
		}
		return 3
	}
	sort.SliceStable(ids, func(i, j int) bool {
		if ri, rj := rank(ids[i]), rank(ids[j]); ri != rj {
			return ri < rj
		}
		return ids[i] < ids[j]
	})
	return ids, nil
}

func isZero(price string) bool {
	price = strings.TrimSpace(price)
	return price != "" && strings.Trim(price, "0.") == ""
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
