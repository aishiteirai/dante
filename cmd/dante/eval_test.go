package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
)

func TestEvalRotas(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	m.files, _ = m.repo.Files("")
	casos := map[string][2]string{
		"oi":                    {"cumprimento", ""},
		"revise as mudanças":    {"conversa", "revisar"},
		"qual é o objetivo?":    {"conversa", "conversar"},
		"/explicar o que muda?": {"conversa", "explicar"},
	}
	for q, want := range casos {
		mm := m
		route, system, prompt, _ := evalRoute(&mm, q)
		got := [2]string{route, ""}
		if route == "conversa" {
			got[1] = taskNames[mm.task]
		}
		if got != want || system == "" || prompt == "" {
			t.Errorf("%q: %v, quer %v", q, got, want)
		}
	}
}

func TestEvalExpectativas(t *testing.T) {
	r := evalResult{Rota: "conversa", Tarefa: "revisar", Estimativa: 500, Entrada: 900, Saida: 50, Rodadas: 2, Achados: 1,
		Resposta: "Encontrei um problema em a.go:10."}
	ok := evalExpect{Rota: "conversa", Tarefa: "revisar", MaxEstimativa: 600, MaxEntrada: 1000, MaxRodadas: 2,
		MinAchados: ptr(1), Contem: []string{`a\.go:\d+`}, NaoContem: []string{"registrar_achado"}}
	if f := checkEval(ok, r, false); len(f) > 0 {
		t.Fatalf("deveria passar: %v", f)
	}
	bad := evalExpect{Rota: "cumprimento", MaxEntrada: 100, MaxAchados: ptr(0), Contem: []string{"oi"}}
	if f := checkEval(bad, r, false); len(f) != 4 {
		t.Fatalf("esperava 4 falhas: %v", f)
	}
	// Sem a IA, só a rota, a tarefa e a estimativa contam.
	if f := checkEval(bad, r, true); len(f) != 1 {
		t.Fatalf("sem IA, esperava 1 falha: %v", f)
	}
}

// Com um provedor falso, o eval manda as perguntas, lê o uso e confere as expectativas.
func TestEvalComModelo(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	t.Setenv("FALSO_KEY", "k")
	var systems []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) > 0 {
			systems = append(systems, fmt.Sprint(body.Messages[0].Content))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range []string{
			`{"choices":[{"delta":{"content":"Oi! Posso revisar as mudanças."}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":60,"completion_tokens":8}}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	m := newModel(config.Defaults(), "", "")
	m.repo = gitRepo(t)
	m.files, _ = m.repo.Files("")
	m.prov = config.Provider{ID: "falso", Nome: "falso", Tipo: llm.KindOpenAI, URL: srv.URL, ChaveEnv: "FALSO_KEY"}
	m.modelName = "x"
	dir := t.TempDir()
	casos := filepath.Join(dir, "eval.json")
	os.WriteFile(casos, []byte(`[
		{"nome":"oi","pergunta":"oi","espera":{"rota":"cumprimento","max_entrada":100,"contem":["revisar"],"max_achados":0}},
		{"nome":"falha","pergunta":"oi","espera":{"max_saida":2}},
		{"nome":"conversa","pergunta":"qual linguagem o projeto usa?","espera":{"rota":"conversa","tarefa":"conversar","max_rodadas":1}}
	]`), 0o644)
	if code := runEval(m, casos, false, true); code != 1 {
		t.Fatalf("o segundo caso deveria falhar (saída 8 > 2): código %d", code)
	}
	if len(systems) != 3 || !strings.Contains(systems[0], "cumprimento") || !strings.Contains(systems[2], "dante") {
		t.Fatalf("o cumprimento deveria ir com o prompt curto: %q", systems)
	}
}
