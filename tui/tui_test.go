package main

import (
	"encoding/json"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestScrollTo(t *testing.T) {
	cases := []struct{ idx, off, h, want int }{
		{0, 0, 5, 0}, {4, 0, 5, 0}, {5, 0, 5, 1}, {2, 3, 5, 2}, {9, 0, 0, 9},
	}
	for _, c := range cases {
		if got := scrollTo(c.idx, c.off, c.h); got != c.want {
			t.Errorf("scrollTo(%d,%d,%d) = %d, quero %d", c.idx, c.off, c.h, got, c.want)
		}
	}
}

func TestEventoResultado(t *testing.T) {
	raw := `{"tipo":"resultado","indice":1,"revisao":{"data":"2026-11-17T17:11:00+00:00",
	"alvo":{"tipo":"commit","descricao":"commit abc","commit":"abc123","comando_diff":"git diff a b","arquivos":["db.py"]},
	"resumo":"ok","achados":[{"arquivo":"db.py","linha":14,"severidade":"critica","categoria":"seguranca",
	"titulo":"SQLi","descricao":"d","cenario":"c","sugestao":"s","confianca":"alta"}],
	"erro":null,"custo_usd":0.02,"turnos":3}}`
	var ev Event
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	m := newModel(Config{Effort: "high"})
	m.width, m.height = 120, 40
	m.screen = scrRunning
	nm, _ := m.onEvent(ev)
	m = nm.(model)
	if len(m.runResult) != 1 || m.runCost != 0.02 {
		t.Fatalf("resultado não registrado: %+v", m.runResult)
	}
	nm, _ = m.onProcDone(procDoneMsg{})
	m = nm.(model)
	if m.screen != scrResults || len(m.visibleFindings()) != 1 {
		t.Fatalf("esperava tela de resultados com 1 achado")
	}
	// Filtro: só crítica continua visível; "alta ou mais grave" mantém; tudo some se não houver.
	m.minSev = sevIndex("critica")
	if len(m.visibleFindings()) != 1 {
		t.Fatal("crítica deveria passar no filtro")
	}
	m.reviews[0].Achados[0].Severidade = "baixa"
	if len(m.visibleFindings()) != 0 {
		t.Fatal("baixa não deveria passar no filtro crítica")
	}
	if v := m.View(); v == "" {
		t.Fatal("View vazia")
	}
}

func TestMenuEsforco(t *testing.T) {
	m := newModel(Config{Effort: "high"})
	nm, _ := m.updateMenu(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if got := nm.(model).cfg.Effort; got != "xhigh" {
		t.Fatalf("esforço = %s, quero xhigh", got)
	}
}
