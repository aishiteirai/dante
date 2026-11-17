package main

import (
	"fmt"
	"strings"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- tela 1: confiar no repositório atual? ----

var trustOptions = []string{"Confiar e abrir este repositório", "Abrir outro repositório", "Sair"}

func (m model) updateTrust(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "up", "k":
		m.trustIdx = (m.trustIdx + len(trustOptions) - 1) % len(trustOptions)
	case "down", "j", "tab":
		m.trustIdx = (m.trustIdx + 1) % len(trustOptions)
	case "s", "y":
		m.trustIdx = 0
		return m.chooseTrust()
	case "o":
		m.trustIdx = 1
		return m.chooseTrust()
	case "q", "esc":
		return m, tea.Quit
	case "enter":
		return m.chooseTrust()
	}
	return m, nil
}

func (m model) chooseTrust() (tea.Model, tea.Cmd) {
	switch m.trustIdx {
	case 0:
		m.opening = true
		m.screen = scrRepo
		m.repoInput.SetValue(m.cwdRoot)
		return m, tea.Batch(openRepo(m.cwdRoot), m.spin.Tick)
	case 1:
		m.screen = scrRepo
		m.repoInput.SetValue("")
		m.repoInput.Focus()
		return m, textinput.Blink
	}
	return m, tea.Quit
}

func (m model) center(s string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, s)
}

func (m model) viewTrust() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("dante") + subtleStyle.Render("  revisor de código") + "\n\n")
	b.WriteString(boldStyle.Render("Abrir o repositório atual?") + "\n\n")
	b.WriteString("  " + labelStyle.Render(repoDisplayName(m.cwdRoot)) + "  " + subtleStyle.Render(prettyPath(m.cwdRoot)) + "\n")
	if m.cfg.Trusted(m.cwdRoot) {
		b.WriteString("  " + okStyle.Render("✓ você já confiou neste repositório antes") + "\n")
	}
	b.WriteString("\n" + subtleStyle.Render("O dante lê os arquivos deste repositório e os envia ao modelo\nque você escolher. Só abra repositórios em que você confia.") + "\n\n")
	for i, o := range trustOptions {
		if i == m.trustIdx {
			b.WriteString(selectedStyle.Render("▸ "+o) + "\n")
		} else {
			b.WriteString("  " + o + "\n")
		}
	}
	b.WriteString("\n" + helpStyle.Render("↑↓ escolher · enter confirmar · q sair"))
	return m.center(boxStyle.Render(b.String()))
}

// ---- tela 2: digitar outro repositório ----

func (m model) updateRepo(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.opening {
		return m, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			if m.repo.Root != "" {
				m.screen = scrMain
				m.status = ""
				return m, nil
			}
			if m.cwdRoot != "" {
				m.screen = scrTrust
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			m.opening = true
			m.status = ""
			m.completions = nil
			return m, tea.Batch(openRepo(m.repoInput.Value()), m.spin.Tick)
		case "up", "down":
			if n := len(m.recents); n > 0 {
				if k.String() == "down" {
					m.recentIdx = min(m.recentIdx+1, n-1)
				} else {
					m.recentIdx = max(m.recentIdx-1, 0)
				}
				m.repoInput.SetValue(prettyPath(m.recents[m.recentIdx]))
				m.repoInput.CursorEnd()
			}
			return m, nil
		case "tab":
			v, opts := completePath(m.repoInput.Value())
			m.repoInput.SetValue(v)
			m.repoInput.CursorEnd()
			m.completions = opts
			return m, nil
		}
		m.completions = nil
		m.status = ""
	}
	var cmd tea.Cmd
	m.repoInput, cmd = m.repoInput.Update(msg)
	return m, cmd
}

func (m model) viewRepo() string {
	w := min(m.width-8, 90)
	m.repoInput.Width = w - 4
	var b strings.Builder
	b.WriteString(titleStyle.Render("dante") + "\n\n")
	b.WriteString(boldStyle.Render("Qual repositório você quer revisar?") + "\n")
	b.WriteString(subtleStyle.Render("Pasta local, URL do git, ou dono/repo do GitHub (é clonado).") + "\n\n")
	if m.opening {
		b.WriteString(m.spin.View() + " abrindo " + boldStyle.Render(m.repoInput.Value()) + subtleStyle.Render("…") + "\n")
	} else {
		b.WriteString(m.repoInput.View() + "\n")
	}
	if len(m.completions) > 0 {
		shown := m.completions
		if len(shown) > 10 {
			shown = append(shown[:10:10], "…")
		}
		b.WriteString(subtleStyle.Render(truncateW("  "+strings.Join(shown, "  "), w)) + "\n")
	}
	if len(m.recents) > 0 {
		b.WriteString("\n" + labelStyle.Render("Recentes") + "\n")
		for i, r := range m.recents {
			if i >= 8 {
				break
			}
			cursor, name := "  ", repoDisplayName(r)
			if i == m.recentIdx {
				cursor, name = selectedStyle.Render("▸ "), selectedStyle.Render(name)
			}
			b.WriteString(truncateW(cursor+padTo(name, 22)+" "+subtleStyle.Render(prettyPath(r)), w) + "\n")
		}
	}
	if m.status != "" {
		b.WriteString("\n" + errStyle.Render(truncateW(m.status, w)) + "\n")
	}
	help := "enter abrir · tab completar · ↑↓ recentes · esc "
	if m.repo.Root != "" {
		help += "voltar"
	} else {
		help += "sair"
	}
	b.WriteString("\n" + helpStyle.Render(help))
	return m.center(boxStyle.Width(w + 6).Render(b.String()))
}

// ---- tela 3: escolher o modelo ----

type modelRow struct {
	header bool
	prov   config.Provider
	model  string
	note   string
	ok     bool
}

func (m *model) enterModelScreen() []tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range m.cfg.Provedores {
		if p.Listar {
			cmds = append(cmds, listModels(p))
		}
	}
	// Pré-seleciona o modelo atual ou o último usado (que pode chegar depois, na lista de um servidor local).
	m.modelWant = m.cfg.UltimoModelo
	if m.modelName != "" {
		m.modelWant = &config.Choice{Provedor: m.prov.ID, Modelo: m.modelName}
	}
	m.modelIdx = -1
	m.buildModelRows()
	if m.modelIdx < 0 {
		m.modelIdx = m.nextModel(-1, 1)
	}
	return cmds
}

func (m *model) buildModelRows() {
	var sel modelRow
	if m.modelIdx >= 0 && m.modelIdx < len(m.modelRows) {
		sel = m.modelRows[m.modelIdx]
	}
	var rows []modelRow
	for _, p := range m.cfg.Provedores {
		models := append([]string{}, p.Modelos...)
		h := modelRow{header: true, prov: p}
		switch {
		case p.Listar:
			if list, ok := m.listed[p.ID]; ok {
				models = mergeUnique(models, list)
				h.note, h.ok = fmt.Sprintf("rodando · %s", plural(len(list), "modelo", "modelos")), true
				if len(list) == 0 {
					h.note, h.ok = "rodando, mas sem modelos baixados", false
				}
			} else if _, failed := m.listErr[p.ID]; failed {
				h.note = "não está rodando em " + p.URL
			} else {
				h.note = "procurando…"
			}
		case p.HasKey():
			h.note, h.ok = "chave encontrada", true
		default:
			h.note = "defina " + p.ChaveEnv
		}
		rows = append(rows, h)
		for _, md := range models {
			rows = append(rows, modelRow{prov: p, model: md})
		}
	}
	m.modelRows = rows
	for i, r := range rows {
		if r.header {
			continue
		}
		if w := m.modelWant; w != nil && r.prov.ID == w.Provedor && r.model == w.Modelo {
			m.modelIdx = i
			break
		}
		// Mantém a seleção quando a lista de um servidor local chega.
		if m.modelWant == nil && r.prov.ID == sel.prov.ID && r.model == sel.model {
			m.modelIdx = i
		}
	}
	m.modelOff = scrollTo(max(m.modelIdx, 0), m.modelOff, m.modelListHeight())
}

func mergeUnique(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range append(a, b...) {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func (m model) nextModel(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(m.modelRows); i += dir {
		if !m.modelRows[i].header {
			return i
		}
	}
	return -1
}

func (m model) updateModel(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "up", "k":
		m.modelWant = nil
		if i := m.nextModel(m.modelIdx, -1); i >= 0 {
			m.modelIdx = i
		}
	case "down", "j":
		m.modelWant = nil
		if i := m.nextModel(m.modelIdx, 1); i >= 0 {
			m.modelIdx = i
		}
	case "e":
		m.cfg.Esforco = cycle(efforts, m.cfg.Esforco)
		_ = m.cfg.Save()
		m.chat = nil // vale a partir da próxima conversa
	case "esc":
		if m.modelName != "" {
			m.screen = scrMain
			return m, nil
		}
		return m.toRepoScreen()
	case "q":
		return m, tea.Quit
	case "enter":
		if m.modelIdx < 0 || m.modelIdx >= len(m.modelRows) {
			return m, nil
		}
		r := m.modelRows[m.modelIdx]
		if r.prov.ID != m.prov.ID || r.model != m.modelName || m.chat == nil {
			m.selectModel(r.prov, r.model)
		}
		m.screen = scrMain
		m.resize()
		return m, nil
	}
	m.modelOff = scrollTo(m.modelIdx, m.modelOff, m.modelListHeight())
	return m, nil
}

func (m model) modelListHeight() int { return max(m.height-14, 5) }

func (m model) viewModel() string {
	w := min(m.width-8, 80)
	var b strings.Builder
	b.WriteString(titleStyle.Render("dante") + subtleStyle.Render(" · "+repoDisplayName(m.repo.Root)) + "\n\n")
	b.WriteString(boldStyle.Render("Qual modelo vai revisar o código?") + "\n")
	b.WriteString(subtleStyle.Render("Esforço do Claude: ") + boldStyle.Render(m.cfg.Esforco) + subtleStyle.Render("  (e muda)") + "\n\n")
	h := m.modelListHeight()
	end := min(m.modelOff+h, len(m.modelRows))
	for i := m.modelOff; i < end; i++ {
		r := m.modelRows[i]
		if r.header {
			note := subtleStyle.Render(r.note)
			if r.ok {
				note = okStyle.Render("✓ " + r.note)
			} else if !strings.HasPrefix(r.note, "procurando") {
				note = warnStyle.Render(r.note)
			}
			if i > m.modelOff {
				b.WriteString("\n")
			}
			kind := "API"
			if r.prov.Local {
				kind = "local"
			} else if r.prov.Tipo == llm.KindOpenAI {
				kind = "API compatível com OpenAI"
			}
			b.WriteString(truncateW(labelStyle.Render(r.prov.Nome)+subtleStyle.Render(" · "+kind+" · ")+note, w) + "\n")
			continue
		}
		if i == m.modelIdx {
			b.WriteString(selectedStyle.Render("  ▸ "+r.model) + "\n")
		} else {
			b.WriteString("    " + r.model + "\n")
		}
	}
	if m.status != "" {
		b.WriteString("\n" + errStyle.Render(truncateW(m.status, w)) + "\n")
	}
	b.WriteString("\n" + subtleStyle.Render(truncateW("Provedores e modelos: "+prettyPath(config.Path()), w)) + "\n")
	b.WriteString(helpStyle.Render("↑↓ escolher · enter usar · e esforço · esc voltar · q sair"))
	return m.center(boxStyle.Width(w + 6).Render(b.String()))
}
