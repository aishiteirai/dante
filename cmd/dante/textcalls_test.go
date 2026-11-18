package main

import (
	"strings"
	"testing"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/charmbracelet/x/ansi"
)

// Texto como o nemotron-3-nano devolveu: as chamadas escritas, sem usar a ferramenta.
const respostaComChamadas = `O commit implementa a tela de modelos em sanfona com busca, mas introduz bugs.

registar_achado( arquivo="cmd/dante/pickers.go", linha=189, categoria="bug", titulo="Provedor marcado como pronto sem modelos disponíveis", explicacao="Em provedores locais com lista vazia, o código define h.ready = true (mesmo sem modelos).", severidade="alta", sugesto="Definir h.ready = true apenas quando a lista não estiver vazia." ) registar_achado(
arquivo="cmd/dante/pickers.go", linha=219, categoria="bug", titulo="Cabeçalhos tratados como selecionáveis", explicacao="A condição passou a usar selectable().", severidade="media", sugesto="Restaurar a verificação." )`

func TestAchadosEscritosComoTexto(t *testing.T) {
	rest, fs := extractTextFindings(respostaComChamadas)
	if len(fs) != 2 || fs[0].Linha != 189 || fs[0].Severidade != "alta" || !strings.HasPrefix(fs[0].Sugestao, "Definir") ||
		!strings.Contains(fs[0].Explicacao, "(mesmo sem modelos)") || fs[1].Severidade != "media" {
		t.Fatalf("achados = %+v", fs)
	}
	if strings.Contains(rest, "registar") || !strings.HasPrefix(rest, "O commit implementa") {
		t.Fatalf("texto que sobrou = %q", rest)
	}
	// Forma com JSON.
	_, fs = extractTextFindings(`Resumo.` + "\n" + `registrar_achado({"arquivo": "a.go", "linha": 3, "severidade": "baixa", "titulo": "t", "explicacao": "e"})`)
	if len(fs) != 1 || fs[0].Linha != 3 {
		t.Fatalf("JSON: %+v", fs)
	}
	// Texto sem chamadas, ou com uma chamada inválida, fica como está.
	for _, s := range []string{"nada aqui", "veja registrar_achado(arquivo=\"a.go\") no código"} {
		if got, fs := extractTextFindings(s); got != s || len(fs) != 0 {
			t.Fatalf("%q mudou para %q", s, got)
		}
	}
}

func TestChatMostraCartoesDosAchadosEmTexto(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.chatView.Width, m.chatView.Height = 100, 60
	m.modelName = "nemotron"
	m.entries = []entry{{kind: eUser, text: "do que se trata?"}, {kind: eAssistant, text: respostaComChamadas}}
	nm, _ := m.onChatDone(chatDoneMsg{})
	m = nm.(model)
	out := ansi.Strip(m.chatView.View())
	for _, want := range []string{"RESUMO", "ALTA · bug", "Provedor marcado como pronto", "MÉDIA · bug"} {
		if !strings.Contains(out, want) {
			t.Errorf("chat sem %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "registar_achado") {
		t.Fatalf("a chamada crua continua no chat:\n%s", out)
	}
}
