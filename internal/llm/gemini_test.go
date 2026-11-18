package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Servidor no formato da API do Gemini compatível com OpenAI: lista "models/…", manda as
// chamadas de ferramenta sem index e com a assinatura do raciocínio em extra_content.
func geminiServer(t *testing.T, bodies *[]map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer boa" {
			w.WriteHeader(400)
			fmt.Fprint(w, `[{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT"}}]`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			fmt.Fprint(w, `{"object":"list","data":[{"id":"models/gemini-2.5-flash"},{"id":"models/text-embedding-004"},
				{"id":"models/gemini-2.5-pro"},{"id":"models/gemini-2.5-flash-preview-tts"},{"id":"models/gemini-3-pro-preview"},
				{"id":"models/gemini-2.5-flash-lite"}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(b, &body)
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(*bodies) == 1 {
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"role":"assistant","tool_calls":[`+
				`{"id":"a1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.go\"}"},"extra_content":{"google":{"thought_signature":"SIG1"}}},`+
				`{"id":"b2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"b.go\"}"}}]},"finish_reason":"tool_calls"}]}`)
		} else {
			fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"pronto"},"finish_reason":"stop"}]}`)
		}
		fmt.Fprintf(w, "data: %s\n\n", `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":5}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestGemini(t *testing.T) {
	var bodies []map[string]any
	srv := geminiServer(t, &bodies)
	defer srv.Close()

	// Lista: só modelos de conversa, sem o "models/", pro primeiro e os mais novos antes.
	ms, err := ListModels(context.Background(), Provider{BaseURL: srv.URL, APIKey: "boa"}, FilterGemini)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ms, ","); got != "gemini-3-pro-preview,gemini-2.5-pro,gemini-2.5-flash,gemini-2.5-flash-lite" {
		t.Fatalf("modelos = %s", got)
	}
	// Chave inválida: o Gemini responde 400, e o dante entende como chave recusada.
	if err := CheckKey(context.Background(), Provider{Kind: KindOpenAI, BaseURL: srv.URL, APIKey: "ruim"}); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("chave ruim: %v", err)
	}

	// Duas chamadas sem index viram duas chamadas; a assinatura volta na rodada seguinte.
	var paths []string
	exec := func(_ context.Context, c ToolCall) (string, bool) { paths = append(paths, c.Args); return "ok", false }
	chat, _ := New(Provider{Kind: KindOpenAI, BaseURL: srv.URL, APIKey: "boa"}, Options{Model: "gemini-2.5-pro", Tools: []Tool{readTool}, Exec: exec})
	if _, err := chat.Send(context.Background(), "revise", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || !strings.Contains(paths[0], "a.go") || !strings.Contains(paths[1], "b.go") {
		t.Fatalf("chamadas = %v", paths)
	}
	msgs := bodies[1]["messages"].([]any)
	asst := msgs[1].(map[string]any)
	calls := asst["tool_calls"].([]any)
	first := calls[0].(map[string]any)
	if b, _ := json.Marshal(first["extra_content"]); !strings.Contains(string(b), "SIG1") {
		t.Fatalf("a assinatura do raciocínio deveria voltar: %v", first)
	}
	if _, ok := calls[1].(map[string]any)["extra_content"]; ok {
		t.Fatal("sem extra_content na resposta, nada deve ser inventado")
	}

	// Erro do provedor em lista é lido.
	if err := httpError(400, []byte(`[{"error":{"message":"API key not valid."}}]`), Provider{}); !strings.Contains(err.Error(), "chave de API recusada") {
		t.Fatalf("erro = %v", err)
	}
}
