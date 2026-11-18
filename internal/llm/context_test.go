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

func TestOllamaNativo(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"qwen3:8b"},{"name":"deepseek-r1:8b"}]}`)
			return
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("caminho inesperado %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			fmt.Fprintln(w, `{"message":{"role":"assistant","thinking":"hmm"},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Abrindo."},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"read_file","arguments":{"path":"a.py"}}}]},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":300,"eval_count":12}`)
			return
		}
		// Segunda rodada: o prompt encheu o contexto (o Ollama cortaria sem avisar).
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Tudo certo."},"done":false}`)
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":4096,"eval_count":3}`)
	}))
	defer srv.Close()

	p := Provider{Kind: KindOllama, BaseURL: srv.URL}
	var calls []ToolCall
	var evs []Event
	chat, _ := New(p, Options{Model: "qwen3:8b", System: "s", Tools: []Tool{readTool}, Exec: execStub(&calls), ContextTokens: 4096})
	u, err := chat.Send(context.Background(), "revise", collect(&evs))
	if err != nil {
		t.Fatal(err)
	}
	if text(evs) != "Abrindo.Tudo certo." || len(calls) != 1 || calls[0].Args != `{"path":"a.py"}` {
		t.Fatalf("texto=%q chamadas=%+v", text(evs), calls)
	}
	if opts := bodies[0]["options"].(map[string]any); opts["num_ctx"] != float64(4096) {
		t.Fatalf("num_ctx = %v", opts)
	}
	msgs := bodies[1]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "tool" || last["tool_name"] != "read_file" {
		t.Fatalf("mensagem da ferramenta = %v", last)
	}
	avisou := false
	for _, e := range evs {
		if e.Kind == EvNotice && strings.Contains(e.Text, "encheu o contexto do Ollama") {
			avisou = true
		}
	}
	if !avisou || u.InputTokens != 4396 {
		t.Fatalf("aviso de contexto=%v uso=%+v", avisou, u)
	}
	models, err := ListModels(context.Background(), Provider{Kind: KindOllama, BaseURL: srv.URL + "/v1"}, FilterNone)
	if err != nil || strings.Join(models, ",") != "deepseek-r1:8b,qwen3:8b" {
		t.Fatalf("modelos = %v, %v", models, err)
	}
}

// Servidor compatível com OpenAI que sempre pede read_file na primeira rodada de cada
// mensagem e responde na segunda; o arquivo lido é grande.
func bigFileServer(t *testing.T, bodies *[]map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		*bodies = append(*bodies, body)
		msgs := body["messages"].([]any)
		lastRole := msgs[len(msgs)-1].(map[string]any)["role"]
		w.Header().Set("Content-Type", "text/event-stream")
		if lastRole == "user" {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestPodaDeResultadosAntigos(t *testing.T) {
	var bodies []map[string]any
	srv := bigFileServer(t, &bodies)
	defer srv.Close()
	grande := strings.Repeat("x", 2500) // ~830 tokens pela estimativa inicial
	exec := func(context.Context, ToolCall) (string, bool) { return grande, false }
	chat, _ := New(Provider{Kind: KindOpenAI, BaseURL: srv.URL}, Options{Model: "m", Tools: []Tool{readTool}, Exec: exec, ContextTokens: 4000})

	var evs []Event
	for i := 0; i < 4; i++ {
		if _, err := chat.Send(context.Background(), fmt.Sprintf("pergunta %d", i), collect(&evs)); err != nil {
			t.Fatal(err)
		}
	}
	// No último pedido, os resultados das rodadas antigas foram trocados pelo aviso, e o da
	// rodada atual continua inteiro.
	msgs := bodies[len(bodies)-1]["messages"].([]any)
	var tools []string
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "tool" {
			tools = append(tools, mm["content"].(string))
		}
	}
	if len(tools) != 4 || tools[0] != prunedResult || tools[3] != grande {
		var resumo []string
		for _, c := range tools {
			resumo = append(resumo, c[:min(len(c), 20)])
		}
		t.Fatalf("resultados no último pedido: %v", resumo)
	}
	avisos := 0
	for _, e := range evs {
		if e.Kind == EvNotice && strings.Contains(e.Text, "descartei") {
			avisos++
		}
	}
	if avisos != 1 {
		t.Fatalf("esperava 1 aviso de poda, veio %d", avisos)
	}
}

func TestCorteDeResultadoGrande(t *testing.T) {
	var bodies []map[string]any
	srv := bigFileServer(t, &bodies)
	defer srv.Close()
	enorme := strings.Repeat("y", 50000)
	exec := func(context.Context, ToolCall) (string, bool) { return enorme, false }
	chat, _ := New(Provider{Kind: KindOpenAI, BaseURL: srv.URL}, Options{Model: "m", Tools: []Tool{readTool}, Exec: exec, ContextTokens: 8000})
	if _, err := chat.Send(context.Background(), "oi", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	msgs := bodies[1]["messages"].([]any)
	got := msgs[len(msgs)-1].(map[string]any)["content"].(string)
	if len(got) > 7000 || !strings.Contains(got, "cortado para caber no contexto") {
		t.Fatalf("resultado não foi cortado: %d caracteres", len(got))
	}
}

func TestOrcamentoPorPergunta(t *testing.T) {
	// Modelo teimoso: sempre pede outra ferramenta, e cada rodada "custa" 1000 tokens.
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]},"finish_reason":"tool_calls"}]}`)
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[],"usage":{"prompt_tokens":900,"completion_tokens":100}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	executadas := 0
	exec := func(context.Context, ToolCall) (string, bool) { executadas++; return "conteúdo", false }
	chat, _ := New(Provider{Kind: KindOpenAI, BaseURL: srv.URL}, Options{Model: "m", Tools: []Tool{readTool}, Exec: exec, TurnBudget: 2500})
	var evs []Event
	u, err := chat.Send(context.Background(), "revise", collect(&evs))
	if err != nil {
		t.Fatal(err)
	}
	// O orçamento é conferido antes da rodada: a rodada 1 executa (gasto 1000 + próxima
	// entrada ~900 < 2500); na 2ª (2000 + 900 ≥ 2500) o modelo recebe o aviso; na 3ª ele
	// insiste e o laço para.
	if executadas != 1 || len(bodies) != 3 || u.InputTokens+u.OutputTokens != 3000 || u.Rounds != 3 {
		t.Fatalf("executadas=%d requisições=%d gasto=%d rodadas=%d", executadas, len(bodies), u.InputTokens+u.OutputTokens, u.Rounds)
	}
	msgs := bodies[2]["messages"].([]any)
	if last := msgs[len(msgs)-1].(map[string]any); !strings.Contains(last["content"].(string), "Orçamento de tokens desta pergunta esgotado") {
		t.Fatalf("o modelo deveria receber o aviso: %v", last)
	}
	var avisos []string
	for _, e := range evs {
		if e.Kind == EvNotice {
			avisos = append(avisos, e.Text)
		}
	}
	if len(avisos) != 2 || !strings.Contains(avisos[0], "pedi ao modelo para concluir") || !strings.Contains(avisos[1], "parei a resposta") {
		t.Fatalf("avisos = %v", avisos)
	}
	// O histórico continua válido: a próxima pergunta funciona.
	if _, err := chat.Send(context.Background(), "e agora?", func(Event) {}); err != nil {
		t.Fatal(err)
	}
}

func TestLimiteDeRodadasDeLeitura(t *testing.T) {
	// A cada requisição, o modelo pede uma leitura e um registrar_achado (livre).
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Content-Type", "text/event-stream")
		if n > 3 {
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"fim"},"finish_reason":"stop"}]}`)
		} else {
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"read_file","arguments":"{}"}},{"index":1,"id":"b","function":{"name":"registrar_achado","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		}
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":60}}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	ran := map[string]int{}
	exec := func(_ context.Context, c ToolCall) (string, bool) { ran[c.Name]++; return "ok", false }
	chat, _ := New(Provider{Kind: KindOpenAI, BaseURL: srv.URL}, Options{Model: "m", Tools: []Tool{readTool}, Exec: exec})
	ctx := WithLimits(context.Background(), Limits{ReadRounds: 1, Free: []string{"registrar_achado"}})
	u, err := chat.Send(ctx, "revise", func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	// Só 1 rodada de leitura; os achados (livres) rodam em todas as rodadas.
	if ran["read_file"] != 1 || ran["registrar_achado"] != 3 {
		t.Fatalf("executadas: %v", ran)
	}
	if u.CachedTokens != 180 || u.Rounds != 3 { // insistiu na leitura depois do aviso: parou na 3ª
		t.Fatalf("uso: %+v", u)
	}
}
