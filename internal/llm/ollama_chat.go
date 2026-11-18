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

// Cliente da API nativa do Ollama (/api/chat). Diferente do endpoint compatível com
// OpenAI, ela aceita options.num_ctx: sem isso o Ollama usa uma janela pequena e corta o
// começo da conversa sem avisar.

const defaultOllamaContext = 16384

type olToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type olMessage struct {
	Role      string       `json:"role"`
	Content   string       `json:"content"`
	ToolCalls []olToolCall `json:"tool_calls,omitempty"`
	ToolName  string       `json:"tool_name,omitempty"`
}

type olChunk struct {
	Message struct {
		Content   string       `json:"content"`
		Thinking  string       `json:"thinking"`
		ToolCalls []olToolCall `json:"tool_calls"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int64  `json:"prompt_eval_count"`
	EvalCount       int64  `json:"eval_count"`
	Error           string `json:"error"`
}

type ollamaChat struct {
	p         Provider
	opts      Options
	http      *http.Client
	tools     []oaTool // mesmo formato de ferramentas da OpenAI
	messages  []olMessage
	budget    *budget
	warnedCtx bool
}

func ollamaRoot(base string) string {
	return strings.TrimSuffix(strings.TrimSuffix(base, "/"), "/v1")
}

func newOllama(p Provider, o Options) *ollamaChat {
	if o.ContextTokens <= 0 {
		o.ContextTokens = defaultOllamaContext
	}
	c := &ollamaChat{p: p, opts: o, http: &http.Client{Timeout: 30 * time.Minute}, budget: newBudget(o.ContextTokens)}
	if o.System != "" {
		c.messages = append(c.messages, olMessage{Role: "system", Content: o.System})
	}
	for _, t := range o.Tools {
		var ot oaTool
		ot.Type = "function"
		ot.Function.Name, ot.Function.Description, ot.Function.Parameters = t.Name, t.Description, t.Params
		c.tools = append(c.tools, ot)
	}
	return c
}

func (c *ollamaChat) sizes() (total int, size func(int) int) {
	size = func(i int) int { b, _ := json.Marshal(c.messages[i]); return len(b) }
	for i := range c.messages {
		total += size(i)
	}
	tb, _ := json.Marshal(c.tools)
	return total + len(tb), size
}

func (c *ollamaChat) fit(current int, emit func(Event)) int {
	total, size := c.sizes()
	removed, total := c.budget.prune(len(c.messages), current, total, size,
		func(i int) bool { return c.messages[i].Role == "tool" && c.messages[i].Content != prunedResult },
		func(i int) { c.messages[i].Content = prunedResult })
	for _, ev := range c.budget.notices(removed, total) {
		emit(ev)
	}
	return total
}

func (c *ollamaChat) Send(ctx context.Context, user string, emit func(Event)) (Usage, error) {
	var usage Usage
	start := len(c.messages)
	c.messages = append(c.messages, olMessage{Role: "user", Content: user})
	fail := func(err error) (Usage, error) {
		c.messages = c.messages[:start]
		return usage, err
	}
	guard := newGuard(ctx, c.opts.TurnBudget)
	for step := 0; step < c.opts.MaxSteps; step++ {
		bytesSent := c.fit(start, emit)
		msg, chunk, err := c.complete(ctx, emit)
		u := Usage{InputTokens: chunk.PromptEvalCount, OutputTokens: chunk.EvalCount, Rounds: 1}
		usage.Add(u)
		guard.add(u)
		if err != nil {
			return fail(err)
		}
		c.budget.calibrate(chunk.PromptEvalCount, bytesSent)
		// O Ollama corta o começo da conversa quando ela passa de num_ctx, sem erro.
		if chunk.PromptEvalCount >= int64(c.opts.ContextTokens)*97/100 && !c.warnedCtx {
			c.warnedCtx = true
			emit(Event{Kind: EvNotice, IsError: true, Text: fmt.Sprintf(
				"a conversa encheu o contexto do Ollama (%d tokens) e o início pode ter sido cortado; "+
					"aumente \"contexto\" do provedor no config.json ou comece outra conversa (ctrl+n)", c.opts.ContextTokens)})
		}
		c.messages = append(c.messages, msg)
		if len(msg.ToolCalls) == 0 {
			if chunk.DoneReason == "length" {
				emit(Event{Kind: EvNotice, Text: "resposta cortada no limite de tokens", IsError: true})
			}
			return usage, nil
		}
		var names []string
		for _, tc := range msg.ToolCalls {
			names = append(names, tc.Function.Name)
		}
		v := guard.check(names, emit)
		for i, tc := range msg.ToolCalls {
			if v.blocked(guard, tc.Function.Name) {
				c.messages = append(c.messages, olMessage{Role: "tool", ToolName: tc.Function.Name, Content: "ERRO: " + v.block})
				continue
			}
			args := string(tc.Function.Arguments)
			if args == "" || args == "null" {
				args = "{}"
			}
			// Alguns modelos mandam os argumentos como string JSON em vez de objeto.
			var s string
			if json.Unmarshal(tc.Function.Arguments, &s) == nil {
				args = s
			}
			call := ToolCall{ID: fmt.Sprintf("ol_%d_%d", len(c.messages), i), Name: tc.Function.Name, Args: args}
			emit(Event{Kind: EvToolCall, Call: call})
			out, isErr := c.opts.Exec(ctx, call)
			emit(Event{Kind: EvToolResult, Call: call, Text: out, IsError: isErr})
			if isErr {
				out = "ERRO: " + out
			}
			c.messages = append(c.messages, olMessage{Role: "tool", ToolName: tc.Function.Name, Content: c.budget.clip(out)})
		}
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		if v.stop {
			return usage, nil
		}
	}
	emit(Event{Kind: EvNotice, Text: fmt.Sprintf("parei após %d rodadas de ferramentas", c.opts.MaxSteps), IsError: true})
	return usage, nil
}

func (c *ollamaChat) complete(ctx context.Context, emit func(Event)) (olMessage, olChunk, error) {
	body := map[string]any{
		"model":    c.opts.Model,
		"messages": c.messages,
		"stream":   true,
		"options":  map[string]any{"num_ctx": c.opts.ContextTokens},
	}
	if len(c.tools) > 0 {
		body["tools"] = c.tools
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", ollamaRoot(c.p.BaseURL)+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return olMessage{}, olChunk{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return olMessage{}, olChunk{}, fmt.Errorf("não consegui conectar no Ollama em %s (ele está aberto?)", ollamaRoot(c.p.BaseURL))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(b))
		}
		if resp.StatusCode == 404 {
			return olMessage{}, olChunk{}, fmt.Errorf("o Ollama não tem o modelo %s: %s (baixe-o na tela de modelos)", c.opts.Model, e.Error)
		}
		return olMessage{}, olChunk{}, fmt.Errorf("Ollama: HTTP %d: %s", resp.StatusCode, e.Error)
	}

	msg := olMessage{Role: "assistant"}
	var content strings.Builder
	var last olChunk
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var ch olChunk
		if json.Unmarshal(sc.Bytes(), &ch) != nil {
			continue
		}
		if ch.Error != "" {
			return olMessage{}, olChunk{}, fmt.Errorf("Ollama: %s", ch.Error)
		}
		if ch.Message.Content != "" {
			content.WriteString(ch.Message.Content)
			emit(Event{Kind: EvText, Text: ch.Message.Content})
		}
		if ch.Message.Thinking != "" {
			emit(Event{Kind: EvReasoning, Text: ch.Message.Thinking})
		}
		msg.ToolCalls = append(msg.ToolCalls, ch.Message.ToolCalls...)
		if ch.Done {
			last = ch
			break
		}
	}
	if err := sc.Err(); err != nil {
		return olMessage{}, olChunk{}, err
	}
	msg.Content = content.String()
	return msg, last, nil
}

// listOllama lista os modelos baixados (GET /api/tags).
func listOllama(ctx context.Context, p Provider) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", ollamaRoot(p.BaseURL)+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var names []string
	for _, m := range out.Models {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names, nil
}
