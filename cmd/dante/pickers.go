package main

import (
	"fmt"
	"strings"

	"github.com/aishiteirai/dante/internal/config"
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
//
// Os provedores prontos para usar (com chave, ou o servidor local rodando) vêm primeiro,
// uma linha cada, e só o provedor do cursor fica aberto com os modelos (sanfona). Os sem
// chave ficam numa seção no fim, uma linha cada: enter ou c pede a chave. / busca um modelo
// em todos os provedores.

type modelRow struct {
	header   bool
	label    string // título de seção (não selecionável)
	prov     config.Provider
	model    string
	note     string
	ok       bool
	ready    bool   // cabeçalho: provedor pronto para usar
	count    int    // cabeçalho: quantos modelos
	download bool   // sugestão ainda não baixada no servidor local
	size     string // tamanho do download
}

func (r modelRow) selectable() bool { return r.label == "" }

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
	m.modelFilter, m.modelFiltering = "", false
	m.modelExp = ""
	if m.modelWant != nil {
		m.modelExp = m.modelWant.Provedor
	}
	m.modelIdx = -1
	m.buildModelRows()
	if m.modelIdx < 0 {
		m.modelIdx = m.nextModel(-1, 1)
	}
	return cmds
}

// providerHeader monta a linha do provedor: a situação da chave ou do servidor e quantos
// modelos ele tem.
func (m *model) providerHeader(p config.Provider) (h modelRow, models []string, downloads []modelRow) {
	models = append([]string{}, p.Modelos...)
	h = modelRow{header: true, prov: p}
	list, listed := m.listed[p.ID]
	_, failed := m.listErr[p.ID]
	if p.Listar && listed {
		models = visibleModels(models, list)
	}
	switch {
	case p.Local && listed:
		for _, sg := range p.Sugeridos {
			if !hasModel(list, sg.Modelo) {
				downloads = append(downloads, modelRow{prov: p, model: sg.Modelo, download: true, size: sg.Tamanho, note: sg.Nota})
			}
		}
		h.note, h.ok, h.ready = "rodando · "+plural(len(list), "modelo baixado", "modelos baixados"), true, true
		if len(list) == 0 {
			h.note = "rodando · escolha um modelo para baixar"
		}
	case p.Local && failed:
		h.note = "não está rodando em " + p.URL
		if len(p.Sugeridos) > 0 {
			h.note = "não está rodando · instale em ollama.com e abra o Ollama"
		}
	case p.Local:
		h.note, h.ready = "procurando…", true
	default:
		switch p.KeySource() {
		case "dante":
			h.note, h.ok = "chave salva", true
		case "env":
			h.note, h.ok = "chave de "+p.ChaveEnv, true
		case "config":
			h.note, h.ok = "chave no config.json", true
		case "ant":
			h.note, h.ok = "login do ant", true
		}
		h.ready = h.ok
		if h.ok && p.Listar {
			switch {
			case listed && p.Filtro == "gratis":
				h.note = fmt.Sprintf("%d grátis · %s", len(list), h.note)
			case failed:
				h.note = "não consegui listar os modelos · " + h.note
			case !listed:
				h.note = "procurando modelos… · " + h.note
			}
		}
	}
	h.count = len(models)
	return h, models, downloads
}

func (m *model) buildModelRows() {
	var sel modelRow
	if m.modelIdx >= 0 && m.modelIdx < len(m.modelRows) {
		sel = m.modelRows[m.modelIdx]
	}
	var ready, waiting []modelRow
	var rows []modelRow
	filter := fold(strings.TrimSpace(m.modelFilter))
	for _, p := range m.cfg.Provedores {
		h, models, downloads := m.providerHeader(p)
		if filter != "" { // busca: lista plana, de todos os provedores
			for _, md := range models {
				if strings.Contains(fold(md), filter) || strings.Contains(fold(p.Nome), filter) {
					rows = append(rows, modelRow{prov: p, model: md, ready: h.ready})
				}
			}
			continue
		}
		if !h.ready {
			waiting = append(waiting, h)
			continue
		}
		if m.modelExp == "" {
			m.modelExp = p.ID // o primeiro pronto começa aberto
		}
		ready = append(ready, h)
		if p.ID == m.modelExp {
			for _, md := range models {
				ready = append(ready, modelRow{prov: p, model: md, ready: true})
			}
			ready = append(ready, downloads...)
		}
	}
	if filter == "" {
		if len(ready) > 0 {
			rows = append(rows, modelRow{label: "PRONTOS PARA USAR"})
			rows = append(rows, ready...)
		}
		if len(waiting) > 0 {
			rows = append(rows, modelRow{label: "AINDA NÃO CONFIGURADOS · enter ou c cadastra a chave"})
			rows = append(rows, waiting...)
		}
	}
	m.modelRows = rows
	m.modelIdx = -1
	want := m.modelWant
	for i, r := range rows {
		switch {
		case !r.selectable():
		case want != nil && !r.header && r.prov.ID == want.Provedor && r.model == want.Modelo:
			m.modelIdx = i
		case want == nil && m.modelIdx < 0 && r.prov.ID == sel.prov.ID && r.model == sel.model && r.header == sel.header:
			m.modelIdx = i // mantém a seleção quando uma lista chega
		}
	}
	if m.modelIdx < 0 {
		m.modelIdx = m.nextModel(-1, 1)
	}
	m.modelOff = scrollTo(max(m.modelIdx, 0), m.modelOff, m.modelListHeight())
}

// expandAtCursor abre o provedor do cursor (e fecha o anterior), mantendo o cursor nele.
func (m *model) expandAtCursor() {
	if m.modelIdx < 0 || m.modelIdx >= len(m.modelRows) || m.modelFilter != "" {
		return
	}
	r := m.modelRows[m.modelIdx]
	if !r.header || !r.ready || r.prov.ID == m.modelExp {
		return
	}
	m.modelExp = r.prov.ID
	m.modelWant = nil
	m.buildModelRows()
	for i, x := range m.modelRows {
		if x.header && x.prov.ID == r.prov.ID {
			m.modelIdx = i
		}
	}
	m.modelOff = scrollTo(m.modelIdx, m.modelOff, m.modelListHeight())
}

// O Ollama chama "qwen3" de "qwen3:latest".
func hasModel(list []string, name string) bool {
	for _, x := range list {
		if x == name || x == name+":latest" || strings.TrimSuffix(x, ":latest") == name {
			return true
		}
	}
	return false
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
		if m.modelRows[i].selectable() {
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
	key := k.String()
	// Busca: as letras vão para o filtro; setas e enter continuam valendo.
	if m.modelFiltering {
		switch key {
		case "esc":
			m.modelFilter, m.modelFiltering = "", false
			m.buildModelRows()
			return m, nil
		case "backspace":
			if r := []rune(m.modelFilter); len(r) > 0 {
				m.modelFilter = string(r[:len(r)-1])
			}
			m.modelWant = nil
			m.buildModelRows()
			return m, nil
		case "up", "down", "enter":
		default:
			if k.Type == tea.KeyRunes || key == " " {
				m.modelFilter += string(k.Runes)
				if key == " " {
					m.modelFilter += " "
				}
				m.modelWant = nil
				m.buildModelRows()
			}
			return m, nil
		}
	}
	cur := modelRow{}
	if m.modelIdx >= 0 && m.modelIdx < len(m.modelRows) {
		cur = m.modelRows[m.modelIdx]
	}
	switch key {
	case "up", "k":
		m.modelWant = nil
		if i := m.nextModel(m.modelIdx, -1); i >= 0 {
			m.modelIdx = i
		}
		m.expandAtCursor()
	case "down", "j":
		m.modelWant = nil
		if i := m.nextModel(m.modelIdx, 1); i >= 0 {
			m.modelIdx = i
		}
		m.expandAtCursor()
	case "right", "l":
		if cur.header && cur.ready {
			if i := m.nextModel(m.modelIdx, 1); i >= 0 && !m.modelRows[i].header {
				m.modelIdx = i
			}
		}
	case "left", "h":
		if !cur.header {
			for i := m.modelIdx; i >= 0; i-- {
				if m.modelRows[i].header {
					m.modelIdx = i
					break
				}
			}
		}
	case "/":
		m.modelFiltering = true
		return m, nil
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
	case "c":
		if cur.prov.ID != "" {
			return m.openKeyScreen(cur.prov, nil)
		}
	case "x":
		if cur.prov.ID != "" {
			return m.removeKey(cur.prov)
		}
	case "enter":
		if cur.prov.ID == "" {
			return m, nil
		}
		if cur.header {
			if !cur.ready && !cur.prov.Local { // sem chave: pede a chave e já usa o primeiro modelo
				var then *modelRow
				if len(cur.prov.Modelos) > 0 {
					then = &modelRow{prov: cur.prov, model: cur.prov.Modelos[0]}
				}
				return m.openKeyScreen(cur.prov, then)
			}
			if i := m.nextModel(m.modelIdx, 1); i >= 0 && !m.modelRows[i].header {
				m.modelIdx = i
			}
			break
		}
		if cur.download {
			return m.startPull(cur)
		}
		if !cur.prov.HasKey() { // pede a chave antes de usar o modelo
			return m.openKeyScreen(cur.prov, &cur)
		}
		if cur.prov.ID != m.prov.ID || cur.model != m.modelName || m.chat == nil {
			m.selectModel(cur.prov, cur.model)
		}
		m.modelFilter, m.modelFiltering = "", false
		m.screen = scrMain
		m.resize()
		return m, nil
	}
	m.modelOff = scrollTo(max(m.modelIdx, 0), m.modelOff, m.modelListHeight())
	return m, nil
}

func (m model) modelListHeight() int { return max(m.height-14, 5) }

func (m model) viewModel() string {
	w := min(m.width-8, 80)
	var b strings.Builder
	b.WriteString(titleStyle.Render("dante") + subtleStyle.Render(" · "+repoDisplayName(m.repo.Root)) + "\n\n")
	b.WriteString(boldStyle.Render("Qual modelo vai revisar o código?") + "\n")
	if m.modelName != "" {
		b.WriteString(subtleStyle.Render("Em uso: ") + m.prov.Nome + subtleStyle.Render(" · ") + boldStyle.Render(m.modelName) + "\n")
	}
	switch {
	case m.modelFiltering:
		b.WriteString(selectedStyle.Render("/ ") + m.modelFilter + selectedStyle.Render("▏") +
			subtleStyle.Render(fmt.Sprintf("  %s · esc limpa", plural(len(m.modelRows), "modelo", "modelos"))) + "\n")
	default:
		b.WriteString(subtleStyle.Render("/ busca um modelo em todos os provedores") + "\n")
	}
	b.WriteString("\n")
	h := m.modelListHeight()
	end := min(m.modelOff+h, len(m.modelRows))
	if len(m.modelRows) == 0 {
		b.WriteString(subtleStyle.Render("nenhum modelo encontrado") + "\n")
	}
	for i := m.modelOff; i < end; i++ {
		r := m.modelRows[i]
		sel := i == m.modelIdx
		switch {
		case r.label != "":
			if i > m.modelOff {
				b.WriteString("\n")
			}
			b.WriteString(subtleStyle.Render(r.label) + "\n")
		case r.header:
			name := r.prov.Nome
			note := subtleStyle.Render(r.note)
			switch {
			case r.ok:
				note = okStyle.Render("✓ " + r.note)
			case r.note != "" && !strings.HasPrefix(r.note, "procurando"):
				note = warnStyle.Render(r.note)
			}
			arrow := "  "
			if r.ready {
				arrow = "▸ "
				if r.prov.ID == m.modelExp {
					arrow = "▾ "
				}
			}
			if !r.ready && r.note == "" {
				note = ""
				if r.count > 0 {
					note = subtleStyle.Render(plural(r.count, "modelo", "modelos"))
				}
			}
			line := arrow + name
			if sel {
				line = selectedStyle.Render(line)
			} else if r.ready {
				line = labelStyle.Render(line)
			}
			if note != "" {
				line += subtleStyle.Render(" · ") + note
			}
			b.WriteString(truncateW(line, w) + "\n")
		case r.download:
			extra := "baixar · " + r.size
			if r.note != "" {
				extra += " · " + r.note
			}
			if m.pulling == r.model {
				extra = m.spin.View() + " " + m.pullStatus
			}
			name := "    ↓ " + padTo(r.model, 20)
			if sel {
				name = selectedStyle.Render(name)
			}
			b.WriteString(truncateW(name+" "+subtleStyle.Render(extra), w) + "\n")
		default:
			name := r.model
			if m.modelFilter != "" { // na busca, diz de qual provedor é
				name += subtleStyle.Render(" · " + r.prov.Nome)
				if !r.ready {
					name += warnStyle.Render(" · sem chave")
				}
			}
			cur := r.prov.ID == m.prov.ID && r.model == m.modelName
			mark := "    "
			if cur {
				mark = "  " + okStyle.Render("●") + " "
			}
			if sel {
				b.WriteString(truncateW(mark+selectedStyle.Render("▸ "+r.model)+strings.TrimPrefix(name, r.model), w) + "\n")
			} else {
				b.WriteString(truncateW(mark+"  "+name, w) + "\n")
			}
		}
	}
	if end < len(m.modelRows) {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  … mais %d abaixo", len(m.modelRows)-end)) + "\n")
	}
	if m.status != "" {
		st := okStyle
		if m.statusErr {
			st = errStyle
		}
		b.WriteString("\n" + st.Render(truncateW(m.status, w)) + "\n")
	}
	help := "enter usa · ↑↓ anda · / busca · c chave · x remove a chave"
	for _, p := range m.cfg.Provedores {
		if p.Tipo == "anthropic" && p.HasKey() {
			help += " · e esforço do Claude: " + m.cfg.Esforco
			break
		}
	}
	b.WriteString("\n" + helpStyle.Render(truncateW(help+" · esc volta", w)) + "\n")
	b.WriteString(subtleStyle.Render(truncateW("provedores em "+prettyPath(config.Path()), w)))
	return m.center(boxStyle.Render(b.String()))
}
