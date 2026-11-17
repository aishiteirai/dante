package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var readTool = Tool{Name: "read_file", Description: "lê", Params: map[string]any{
	"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"},
}}

func execStub(calls *[]ToolCall) Executor {
	return func(_ context.Context, c ToolCall) (string, bool) {
		*calls = append(*calls, c)
		var in struct{ Path string }
		if err := json.Unmarshal([]byte(c.Args), &in); err != nil {
			return "json inválido", true
		}
		return "conteúdo de " + in.Path, false
	}
}

func collect(evs *[]Event) func(Event) { return func(e Event) { *evs = append(*evs, e) } }

func text(evs []Event) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Kind == EvText {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

// ---- Anthropic ----

func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		var t struct{ Type string }
		_ = json.Unmarshal([]byte(e), &t)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", t.Type, e)
	}
}

func TestAnthropicToolLoop(t *testing.T) {
	var bodies []map[string]any
	var betas []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		bodies = append(bodies, body)
		betas = append(betas, r.Header.Get("anthropic-beta"))
		start := `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`
		if len(bodies) == 1 {
			sse(w, start,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Vou abrir "}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"o arquivo."}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"db.py\"}"}}`,
				`{"type":"content_block_stop","index":1}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}`,
				`{"type":"message_stop"}`)
			return
		}
		sse(w, start,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" Tem SQL injection."}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			`{"type":"message_stop"}`)
	}))
	defer srv.Close()

	var calls []ToolCall
	var evs []Event
	chat, _ := New(Provider{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: "x"},
		Options{Model: "claude-opus-5-5", System: "sys", Tools: []Tool{readTool}, Exec: execStub(&calls), Effort: "high"})
	u, err := chat.Send(context.Background(), "revise", collect(&evs))
	if err != nil {
		t.Fatal(err)
	}
	if got := text(evs); got != "Vou abrir o arquivo. Tem SQL injection." {
		t.Fatalf("texto = %q", got)
	}
	if len(calls) != 1 || calls[0].Name != "read_file" || calls[0].Args != `{"path":"db.py"}` {
		t.Fatalf("chamadas = %+v", calls)
	}
	if u.OutputTokens != 12 {
		t.Fatalf("uso = %+v", u)
	}
	// Segunda requisição leva o tool_use e o tool_result.
	msgs := bodies[1]["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("esperava 3 mensagens, veio %d", len(msgs))
	}
	res := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if res["type"] != "tool_result" || res["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool_result = %v", res)
	}
	if bodies[0]["fallbacks"] != "default" || !strings.Contains(betas[0], "server-side-fallback") {
		t.Fatalf("fallback ausente: %v / %s", bodies[0]["fallbacks"], betas[0])
	}
	if oc := bodies[0]["output_config"].(map[string]any); oc["effort"] != "high" {
		t.Fatalf("effort = %v", oc)
	}
	cm, _ := bodies[0]["context_management"].(map[string]any)
	if cm == nil || !strings.Contains(betas[0], "context-management-2025-06-27") {
		t.Fatalf("limpeza de contexto ausente: %v / %s", bodies[0]["context_management"], betas[0])
	}
	edit := cm["edits"].([]any)[0].(map[string]any)
	if edit["type"] != "clear_tool_uses_20250919" || edit["trigger"].(map[string]any)["value"] != float64(150000) {
		t.Fatalf("edit = %v", edit)
	}
}

// ---- Compatível com OpenAI ----

func TestOpenAIToolLoop(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"message":"bad key"}}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"choices":[{"delta":{"reasoning_content":"pensando"}}]}`,
			`{"choices":[{"delta":{"content":"Abrindo."}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read_file","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.py\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":4}}`,
		}
		if len(bodies) == 2 {
			chunks = []string{`{"choices":[{"delta":{"content":"Está ok."},"finish_reason":"stop"}]}`}
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	var calls []ToolCall
	var evs []Event
	p := Provider{Kind: KindOpenAI, BaseURL: srv.URL + "/v1", APIKey: "k"}
	chat, _ := New(p, Options{Model: "deepseek-chat", System: "sys", Tools: []Tool{readTool}, Exec: execStub(&calls)})
	if _, err := chat.Send(context.Background(), "revise", collect(&evs)); err != nil {
		t.Fatal(err)
	}
	if text(evs) != "Abrindo.Está ok." || len(calls) != 1 || calls[0].Args != `{"path":"a.py"}` {
		t.Fatalf("texto=%q chamadas=%+v", text(evs), calls)
	}
	msgs := bodies[1]["messages"].([]any)
	// system, user, assistant(tool_calls), tool
	if len(msgs) != 4 || msgs[3].(map[string]any)["role"] != "tool" || msgs[3].(map[string]any)["tool_call_id"] != "c1" {
		t.Fatalf("mensagens = %v", msgs)
	}
	if bodies[0]["tools"] == nil {
		t.Fatal("ferramentas não enviadas")
	}

	// Chave errada: erro claro e histórico intacto.
	bad, _ := New(Provider{Kind: KindOpenAI, BaseURL: srv.URL + "/v1", APIKey: "x", APIKeyEnv: "DEEPSEEK_API_KEY"}, Options{Model: "m"})
	_, err := bad.Send(context.Background(), "oi", func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Fatalf("erro = %v", err)
	}
	if len(bad.(*openAIChat).messages) != 0 {
		t.Fatal("mensagem com erro deveria sair do histórico")
	}
}

func TestCheckKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") + r.Header.Get("X-Api-Key") {
		case "Bearer boa", "boa":
			fmt.Fprint(w, `{"data":[]}`)
		default:
			w.WriteHeader(401)
			fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	for _, kind := range []Kind{KindOpenAI, KindAnthropic} {
		if err := CheckKey(ctx, Provider{Kind: kind, BaseURL: srv.URL, APIKey: "boa"}); err != nil {
			t.Errorf("%s chave boa: %v", kind, err)
		}
		if err := CheckKey(ctx, Provider{Kind: kind, BaseURL: srv.URL, APIKey: "ruim"}); err != ErrInvalidKey {
			t.Errorf("%s chave ruim: %v", kind, err)
		}
	}
	// Servidor fora do ar: erro, mas não "chave inválida".
	if err := CheckKey(ctx, Provider{Kind: KindOpenAI, BaseURL: "http://127.0.0.1:1", APIKey: "x"}); err == nil || err == ErrInvalidKey {
		t.Errorf("fora do ar: %v", err)
	}
}

func TestListModelsGratis(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
		 {"id":"anthropic/claude-x","pricing":{"prompt":"0.000003","completion":"0.000015"},"supported_parameters":["tools"]},
		 {"id":"meta/llama:free","pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools"]},
		 {"id":"deepseek/deepseek-chat:free","pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools","temperature"]},
		 {"id":"qwen/qwen3-coder:free","pricing":{"prompt":"0","completion":"0.0"},"supported_parameters":["tools"]},
		 {"id":"x/sem-ferramentas:free","pricing":{"prompt":"0","completion":"0"},"supported_parameters":["temperature"]}
		]}`)
	}))
	defer srv.Close()
	got, err := ListModels(context.Background(), Provider{BaseURL: srv.URL}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := "qwen/qwen3-coder:free,deepseek/deepseek-chat:free,meta/llama:free"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v", got)
	}
	all, _ := ListModels(context.Background(), Provider{BaseURL: srv.URL}, false)
	if len(all) != 5 {
		t.Fatalf("sem filtro: %v", all)
	}
}

func TestOllamaPull(t *testing.T) {
	var pedido string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		pedido = r.URL.Path + " " + string(b)
		fmt.Fprintln(w, `{"status":"pulling manifest"}`)
		fmt.Fprintln(w, `{"status":"pulling abc","total":100,"completed":40}`)
		fmt.Fprintln(w, `{"status":"success"}`)
	}))
	defer srv.Close()
	var passos []string
	err := OllamaPull(context.Background(), srv.URL+"/v1", "qwen3:8b", func(s string, done, total int64) {
		passos = append(passos, fmt.Sprintf("%s %d/%d", s, done, total))
	})
	if err != nil || !strings.HasPrefix(pedido, "/api/pull ") || !strings.Contains(pedido, `"qwen3:8b"`) || len(passos) != 3 {
		t.Fatalf("err=%v pedido=%q passos=%v", err, pedido, passos)
	}
}
