package main

import (
	"context"
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/charmbracelet/x/ansi"
)

const achadoJSON = `{"arquivo":"internal/llm/openai.go","linha":111,"severidade":"média","categoria":"bug",
	"titulo":"O orçamento só é checado depois da requisição",
	"explicacao":"guard.add(u) roda após a resposta.","sugestao":"Estimar antes de enviar."}`

func TestParseFinding(t *testing.T) {
	f, err := parseFinding(achadoJSON)
	if err != nil || f.Severidade != "media" || f.Linha != 111 {
		t.Fatalf("f=%+v err=%v", f, err)
	}
	for _, bad := range []string{`{"arquivo":"a","linha":1,"severidade":"grave","titulo":"x","explicacao":"y"}`,
		`{"linha":1,"severidade":"alta","titulo":"x","explicacao":"y"}`, `{`} {
		if _, err := parseFinding(bad); err == nil {
			t.Errorf("deveria recusar %s", bad)
		}
	}
	exec := withFindings(func(context.Context, llm.ToolCall) (string, bool) { return "repo", false })
	if out, bad := exec(context.Background(), llm.ToolCall{Name: findingTool, Args: achadoJSON}); bad || !strings.Contains(out, "registrado") {
		t.Fatalf("executor: %q %v", out, bad)
	}
	if out, _ := exec(context.Background(), llm.ToolCall{Name: "read_file", Args: "{}"}); out != "repo" {
		t.Fatal("outras ferramentas vão para o repositório")
	}
}

func TestCartaoNoChat(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.chatView.Width, m.chatView.Height = 70, 40
	m.modelName = "deepseek-v4-pro"
	m.entries = []entry{{kind: eUser, text: "revise"}}
	m.onChatEvent(llm.Event{Kind: llm.EvText, Text: "O orçamento funciona; um ponto de atenção."})
	m.onChatEvent(llm.Event{Kind: llm.EvToolCall, Call: llm.ToolCall{Name: "read_file", Args: `{"path":"a.go"}`}})
	m.onChatEvent(llm.Event{Kind: llm.EvToolResult, Call: llm.ToolCall{Name: "read_file", Args: `{"path":"a.go"}`}, Text: "1 x"})
	m.onChatEvent(llm.Event{Kind: llm.EvToolCall, Call: llm.ToolCall{Name: findingTool, Args: achadoJSON}})
	m.onChatEvent(llm.Event{Kind: llm.EvToolResult, Call: llm.ToolCall{Name: findingTool, Args: achadoJSON}, Text: "ok"})
	nm, _ := m.onChatDone(chatDoneMsg{})
	m = nm.(model)
	out := ansi.Strip(m.chatView.View())
	for _, want := range []string{"RESUMO", "MÉDIA · bug", "openai.go:111", "O orçamento só é checado", "Sugestão: Estimar antes"} {
		if !strings.Contains(out, want) {
			t.Errorf("chat sem %q:\n%s", want, out)
		}
	}
	// A leitura continua marcada como concluída (o resultado do achado não a confunde).
	for _, e := range m.entries {
		if e.kind == eTool && (!e.done || !e.ok) {
			t.Fatalf("leitura não concluída: %+v", e)
		}
	}
	var f *finding
	for _, e := range m.entries {
		if e.kind == eFinding {
			f = e.f
		}
	}
	if f == nil || !strings.Contains(findingMarkdown(*f, 1), "### [1] MÉDIA") {
		t.Fatal("markdown da conversa salva")
	}
}

func TestLeiturasAgrupadas(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.chatView.Width, m.chatView.Height = 80, 30
	m.modelName = "m"
	m.entries = []entry{
		{kind: eUser, text: "revise"},
		{kind: eTool, text: "abriu a.go", done: true, ok: true},
		{kind: eTool, text: "abriu b.go", done: true, ok: true},
		{kind: eTool, text: "buscou \"x\"", done: true, ok: false},
		{kind: eAssistant, text: "pronto"},
	}
	m.refreshChat()
	out := ansi.Strip(m.chatView.View())
	if !strings.Contains(out, "✗ 3 leituras (1 com erro): abriu a.go · abriu b.go · buscou") || strings.Count(out, "abriu a.go") != 1 {
		t.Fatalf("agrupado:\n%s", out)
	}
	m.showActivity = true
	m.refreshChat()
	out = ansi.Strip(m.chatView.View())
	if strings.Contains(out, "3 leituras") || !strings.Contains(out, "✓ abriu b.go") {
		t.Fatalf("detalhado:\n%s", out)
	}
}
