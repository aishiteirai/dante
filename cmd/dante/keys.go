package main

import (
	"context"
	"errors"
	"strings"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Cadastro da chave de API de um provedor, de dentro do dante.

type keyCheckedMsg struct {
	provID, key string
	err         error
}

func checkKey(p config.Provider, key string) tea.Cmd {
	return func() tea.Msg {
		err := llm.CheckKey(context.Background(), p.WithKey(key).LLM())
		return keyCheckedMsg{provID: p.ID, key: key, err: err}
	}
}

func newKeyInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "cole a chave aqui"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 1000
	ti.Prompt = "› "
	return ti
}

// openKeyScreen abre o cadastro; `then` é o modelo a usar depois de salvar (pode ser nil).
func (m model) openKeyScreen(p config.Provider, then *modelRow) (tea.Model, tea.Cmd) {
	if p.Local {
		m.setInfo(p.Nome + " roda na sua máquina e não precisa de chave")
		return m, nil
	}
	m.screen = scrKey
	m.keyProv, m.keyThen = p, then
	m.keyInput = newKeyInput()
	m.keyErr, m.keyWarn, m.keyUnverified = "", "", ""
	m.keyChecking = false
	m.status = ""
	return m, tea.Batch(m.keyInput.Focus(), textinput.Blink)
}

func (m model) updateKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.keyChecking {
		return m, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.screen = scrModel
			return m, nil
		case "ctrl+t": // mostrar/ocultar
			if m.keyInput.EchoMode == textinput.EchoPassword {
				m.keyInput.EchoMode = textinput.EchoNormal
			} else {
				m.keyInput.EchoMode = textinput.EchoPassword
			}
			return m, nil
		case "enter":
			key := strings.TrimSpace(m.keyInput.Value())
			if key == "" {
				m.keyErr = "cole a chave antes de confirmar"
				return m, nil
			}
			if key == m.keyUnverified { // segundo enter: salva mesmo sem verificar
				return m.saveKey(key, false)
			}
			m.keyChecking, m.keyErr, m.keyWarn = true, "", ""
			return m, tea.Batch(checkKey(m.keyProv, key), m.spin.Tick)
		}
		m.keyErr, m.keyWarn, m.keyUnverified = "", "", ""
	}
	var cmd tea.Cmd
	m.keyInput, cmd = m.keyInput.Update(msg)
	return m, cmd
}

func (m model) onKeyChecked(msg keyCheckedMsg) (tea.Model, tea.Cmd) {
	m.keyChecking = false
	if msg.provID != m.keyProv.ID || m.screen != scrKey {
		return m, nil
	}
	switch {
	case msg.err == nil:
		return m.saveKey(msg.key, true)
	case errors.Is(msg.err, llm.ErrInvalidKey):
		m.keyErr = "O provedor recusou esta chave. Confira se ela foi copiada inteira e se a conta tem créditos."
	default:
		m.keyWarn = "Não deu para verificar (" + msg.err.Error() + "). Pressione enter de novo para salvar mesmo assim."
		m.keyUnverified = msg.key
	}
	return m, nil
}

func (m model) saveKey(key string, verified bool) (tea.Model, tea.Cmd) {
	if err := m.cfg.SetKey(m.keyProv.CredID(), key); err != nil {
		m.keyErr = "não consegui salvar: " + err.Error()
		return m, nil
	}
	p, _ := m.cfg.Provider(m.keyProv.ID)
	note := "chave do " + p.Nome + " salva"
	if verified {
		note += " e verificada"
	}
	// Provedor em uso: o cliente precisa ser recriado com a chave nova.
	if m.prov.ID == p.ID && m.modelName != "" {
		m.selectModel(p, m.modelName)
		note += "; a conversa recomeça"
	}
	if t := m.keyThen; t != nil {
		m.selectModel(p, t.model)
		m.screen = scrMain
		m.resize()
		m.setInfo(note)
		return m, nil
	}
	m.screen = scrModel
	m.buildModelRows()
	m.setInfo(note)
	return m, nil
}

func (m model) removeKey(p config.Provider) (tea.Model, tea.Cmd) {
	if p.KeySource() != "dante" {
		m.setInfo("não há chave salva no dante para " + p.Nome)
		return m, nil
	}
	if err := m.cfg.SetKey(p.CredID(), ""); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	if m.prov.ID == p.ID {
		m.prov, _ = m.cfg.Provider(p.ID)
		m.chat = nil
	}
	m.buildModelRows()
	m.setInfo("chave do " + p.Nome + " removida do dante")
	return m, nil
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return "••••"
	}
	return k[:4] + "••••" + k[len(k)-4:]
}

func (m model) viewKey() string {
	p := m.keyProv
	w := min(m.width-8, 76)
	m.keyInput.Width = w - 4
	var b strings.Builder
	b.WriteString(titleStyle.Render("dante") + "\n\n")
	b.WriteString(boldStyle.Render("Chave da API · "+p.Nome) + "\n")
	if p.SiteChave != "" {
		b.WriteString(subtleStyle.Render("Gere em: ") + p.SiteChave + "\n")
	}
	switch p.KeySource() {
	case "dante":
		b.WriteString(subtleStyle.Render("Atual: chave salva no dante ("+maskKey(p.LLM().APIKey)+")") + "\n")
	case "env":
		b.WriteString(subtleStyle.Render("Atual: variável "+p.ChaveEnv+". A chave salva aqui passa a ter prioridade.") + "\n")
	case "config":
		b.WriteString(warnStyle.Render("Atenção: o campo \"chave\" do config.json tem prioridade sobre a chave salva aqui.") + "\n")
	}
	b.WriteString("\n" + m.keyInput.View() + "\n\n")
	switch {
	case m.keyChecking:
		b.WriteString(m.spin.View() + " testando a chave com o provedor…\n")
	case m.keyErr != "":
		b.WriteString(errStyle.Width(w).Render(m.keyErr) + "\n")
	case m.keyWarn != "":
		b.WriteString(warnStyle.Width(w).Render(m.keyWarn) + "\n")
	default:
		b.WriteString(subtleStyle.Width(w).Render("Ao confirmar, o dante testa a chave (sem gastar tokens) e a salva em "+
			prettyPath(config.CredentialsPath())+", legível só pelo seu usuário.") + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("enter testar e salvar · ctrl+t mostrar/ocultar · esc voltar"))
	return m.center(boxStyle.Width(w + 6).Render(b.String()))
}
