package main

import (
	"context"
	"fmt"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	tea "github.com/charmbracelet/bubbletea"
)

// Download de modelos no Ollama, de dentro da tela de modelos.

type pullProgressMsg struct {
	text string
	ch   chan tea.Msg
}

type pullDoneMsg struct {
	prov  config.Provider
	model string
	err   error
}

func (m model) startPull(r modelRow) (tea.Model, tea.Cmd) {
	if m.pulling != "" {
		m.setInfo("aguarde o download de " + m.pulling + " terminar")
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.pulling, m.pullStatus, m.pullCancel = r.model, "iniciando…", cancel
	m.status = ""
	ch := make(chan tea.Msg, 64)
	go func() {
		last := ""
		err := llm.OllamaPull(ctx, r.prov.URL, r.model, func(status string, done, total int64) {
			text := status
			if total > 0 {
				text = fmt.Sprintf("baixando %d%% (%s de %s)", done*100/total, gb(done), gb(total))
			}
			if text != last { // não inunda a interface com atualizações iguais
				last = text
				select {
				case ch <- pullProgressMsg{text: text, ch: ch}:
				default:
				}
			}
		})
		ch <- pullDoneMsg{prov: r.prov, model: r.model, err: err}
		close(ch)
	}()
	return m, tea.Batch(waitFor(ch), m.spin.Tick)
}

func gb(n int64) string { return fmt.Sprintf("%.1f GB", float64(n)/1e9) }

func (m model) onPullDone(msg pullDoneMsg) (tea.Model, tea.Cmd) {
	m.pulling, m.pullStatus, m.pullCancel = "", "", nil
	if msg.err != nil {
		if msg.err == context.Canceled {
			return m, nil
		}
		m.setErr("não consegui baixar " + msg.model + ": " + msg.err.Error())
		return m, nil
	}
	m.setInfo(msg.model + " baixado; pressione enter para usar")
	m.modelWant = &config.Choice{Provedor: msg.prov.ID, Modelo: msg.model}
	return m, listModels(msg.prov)
}
