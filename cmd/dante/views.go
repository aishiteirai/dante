package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Abaixo desta largura, só o painel com foco aparece (como na tela cheia).
const narrowWidth = 80

func truncateW(s string, w int) string { return ansi.Truncate(s, max(w, 1), "…") }

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	switch m.screen {
	case scrTrust:
		return m.viewTrust()
	case scrRepo:
		return m.viewRepo()
	case scrModel:
		return m.viewModel()
	case scrKey:
		return m.viewKey()
	}
	return m.viewMain()
}

// ---- geometria ----

func (m model) narrow() bool { return m.width < narrowWidth }

func (m model) zoomed() bool { return m.zoom || m.narrow() }

func (m model) areaHeight() int { return max(m.height-2, 6) } // header + rodapé

// relayout recalcula os retângulos e ajusta o tamanho dos componentes de cada painel.
func (m *model) relayout() {
	if m.width == 0 {
		return
	}
	if m.lay.isMinimized(m.focus) {
		if vs := m.lay.visiblePanes(); len(vs) > 0 {
			m.focus = vs[0]
		}
	}
	m.rects = m.lay.compute(0, 1, m.width, m.areaHeight(), m.focus, m.zoomed())
	if r, ok := m.rects[pDiff]; ok && r.h > 2 {
		m.preview.Width, m.preview.Height = max(r.w-4, 4), max(r.h-2, 1)
	}
	if r, ok := m.rects[pChat]; ok && r.h > 2 {
		m.chatView.Width = max(r.w-4, 10)
		m.chatView.Height = max(r.h-2-1-m.input.Height(), 1)
		m.input.SetWidth(max(r.w-4, 10))
	}
	m.logOff = scrollTo(max(m.logIdx, 0), m.logOff, m.logHeight())
	m.fileOff = scrollTo(m.fileIdx, m.fileOff, m.filesRows())
	m.renderPreview()
	m.refreshChat()
}

func (m *model) resize() { m.relayout() }

func (m model) innerRows(p paneID) int {
	if r, ok := m.rects[p]; ok && r.h > 2 {
		return r.h - 2
	}
	return max(m.areaHeight()-2, 1)
}

func (m model) logHeight() int { return m.innerRows(pLog) }
func (m model) filesRows() int { return m.innerRows(pFiles) }

// ---- prévia (diff ou documento aberto pelo modelo) ----

func (m *model) setPreview(title, content string, isDoc bool) {
	m.previewTitle, m.previewRaw, m.previewIsDoc = title, content, isDoc
	m.renderPreview()
	m.preview.GotoTop()
}

func (m *model) renderPreview() {
	w := max(m.preview.Width, 10)
	var out []string
	for _, l := range strings.Split(strings.TrimRight(m.previewRaw, "\n"), "\n") {
		l = strings.ReplaceAll(l, "\t", "    ")
		l = truncateW(l, w)
		if !m.previewIsDoc {
			switch {
			case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "diff --git"),
				strings.HasPrefix(l, "index "), strings.HasPrefix(l, "new file"), strings.HasPrefix(l, "deleted file"),
				strings.HasPrefix(l, "similarity"), strings.HasPrefix(l, "rename "):
				l = subtleStyle.Render(l)
			case strings.HasPrefix(l, "+"):
				l = addStyle.Render(l)
			case strings.HasPrefix(l, "-"):
				l = delStyle.Render(l)
			case strings.HasPrefix(l, "@@"):
				l = hunkStyle.Render(l)
			}
		} else if i := strings.Index(l, "  "); i > 0 && i <= 6 { // número de linha do read_file
			l = subtleStyle.Render(l[:i]) + l[i:]
		}
		out = append(out, l)
	}
	m.preview.SetContent(strings.Join(out, "\n"))
}

// ---- moldura dos painéis (título na borda, como no noct) ----

func (m model) paneTitle(p paneID) (title, extra string) {
	title = fmt.Sprintf("[%d] %s", p+1, paneNames[p])
	switch p {
	case pFiles:
		extra = m.targetLabel()
	case pDiff:
		if m.previewTitle != "" {
			title = fmt.Sprintf("[%d] %s", p+1, m.previewTitle)
		}
	case pChat:
		if m.modelName == "" {
			extra = "sem modelo (m)"
		} else if m.streaming {
			extra = m.spin.View() + " respondendo"
		}
	}
	return
}

func (m model) paneStatus(p paneID) string {
	if p != m.focus {
		return ""
	}
	switch m.mode {
	case modeMove:
		return "mover: setas · esc"
	case modeResize:
		return "redimensionar: setas · esc"
	}
	if m.zoom {
		return "⛶ tela cheia"
	}
	return ""
}

func (m model) frame(p paneID, r rect, content string) []string {
	focused := p == m.focus
	border := lipgloss.NewStyle().Foreground(faint)
	tst := boldStyle
	if focused {
		border = lipgloss.NewStyle().Foreground(accent)
		tst = titleStyle
	}
	title, extra := m.paneTitle(p)

	// Painel dobrado numa pilha: uma linha só.
	if r.h <= 2 {
		label := "▸ " + title
		if extra != "" {
			label += "  " + extra
		}
		line := tst.Render(truncateW(label, r.w-2)) + " "
		line += border.Render(strings.Repeat("─", max(r.w-lipgloss.Width(line), 0)))
		lines := []string{truncateW(line, r.w)}
		for len(lines) < r.h {
			lines = append(lines, strings.Repeat(" ", r.w))
		}
		return lines
	}

	inner := r.w - 4
	status := m.paneStatus(p)
	head := tst.Render(title)
	if extra != "" {
		head += " " + subtleStyle.Render(extra)
	}
	room := r.w - 5 // "╭─ " + " " + "╮"
	if status != "" {
		room -= lipgloss.Width(status) + 3
	}
	head = truncateW(head, max(room, 1))
	top := border.Render("╭─ ") + head + " "
	fill := r.w - lipgloss.Width(top) - 1
	if status != "" {
		fill -= lipgloss.Width(status) + 2
	}
	top += border.Render(strings.Repeat("─", max(fill, 0)))
	if status != "" {
		top += " " + warnStyle.Render(status) + " "
	}
	top += border.Render("╮")

	lines := []string{truncateW(top, r.w)}
	body := strings.Split(content, "\n")
	for i := 0; i < r.h-2; i++ {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		lines = append(lines, border.Render("│")+" "+padTo(truncateW(l, inner), inner)+" "+border.Render("│"))
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", max(r.w-2, 0))+"╯"))
	return lines
}

// ---- tela principal ----

func (m model) paneContent(p paneID, r rect) string {
	w, h := r.w-4, r.h-2
	switch p {
	case pLog:
		return m.viewLog(w, h)
	case pFiles:
		return m.viewFiles(w, h)
	case pDiff:
		return m.preview.View()
	case pChat:
		return m.viewChat(w)
	}
	return ""
}

func (m model) viewMain() string {
	H := m.areaHeight()
	type placed struct {
		r     rect
		lines []string
	}
	var ps []placed
	for p, r := range m.rects {
		content := ""
		if r.h > 2 {
			content = m.paneContent(p, r)
		}
		ps = append(ps, placed{r, m.frame(p, r, content)})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].r.x < ps[j].r.x })
	rows := make([]string, H)
	for y := 0; y < H; y++ {
		var b strings.Builder
		for _, pl := range ps {
			if yy := y + 1 - pl.r.y; yy >= 0 && yy < pl.r.h {
				b.WriteString(pl.lines[yy])
			}
		}
		rows[y] = b.String()
	}
	screen := append([]string{m.header()}, rows...)
	screen = append(screen, m.footer())
	if m.mode == modePrefix {
		screen = overlay(screen, m.commandsPopup(), m.width)
	}
	return strings.Join(screen, "\n")
}

// overlay desenha o popup centralizado por cima das linhas da tela.
func overlay(screen []string, popup string, width int) []string {
	pl := strings.Split(popup, "\n")
	pw := lipgloss.Width(popup)
	x := max((width-pw)/2, 0)
	y := max((len(screen)-len(pl))/2, 0)
	for i, l := range pl {
		if y+i >= len(screen) {
			break
		}
		base := padTo(screen[y+i], width)
		left := ansi.Truncate(base, x, "")
		right := ansi.Cut(base, x+lipgloss.Width(l), width)
		screen[y+i] = left + l + right
	}
	return screen
}

var commandHelp = [][2]string{
	{"←↑↓→ hjkl", "foco no painel ao lado"},
	{"a", "mover o painel (depois setas)"},
	{"n", "redimensionar (depois setas)"},
	{"w", "empilhar / separar a coluna"},
	{"f", "tela cheia"},
	{"z", "minimizar o painel"},
	{"1 2 3 4", "ir para o painel (restaura)"},
	{"0", "voltar ao layout padrão"},
	{"m", "trocar o modelo"},
	{"o", "abrir outro repositório"},
	{"u", "atualizar commits e arquivos"},
	{"q", "sair do dante"},
}

func (m model) commandsPopup() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("comandos") + "\n\n")
	for _, c := range commandHelp {
		b.WriteString(selectedStyle.Render(padTo(c[0], 11)) + " " + c[1] + "\n")
	}
	b.WriteString("\n" + subtleStyle.Render("Direto, sem o prefixo: Alt + tecla.") + "\n")
	b.WriteString(subtleStyle.Render("Alt+Shift+setas move o painel.") + "\n")
	b.WriteString(subtleStyle.Render("esc fecha"))
	return boxStyle.Padding(1, 2).Render(b.String())
}

func (m model) header() string {
	left := titleStyle.Render("dante") + subtleStyle.Render(" · ") + boldStyle.Render(repoDisplayName(m.repo.Root))
	if m.branch != "" {
		left += subtleStyle.Render(" (" + m.branch + ")")
	}
	right := ""
	if m.modelName != "" {
		right = subtleStyle.Render(m.prov.Nome+" · ") + boldStyle.Render(m.modelName)
		if m.prov.Tipo == "anthropic" {
			right += subtleStyle.Render(" · esforço " + m.cfg.Esforco)
		}
		if t := m.usage.InputTokens + m.usage.OutputTokens; t > 0 {
			right += subtleStyle.Render(fmt.Sprintf(" · %s tokens", humanInt(t)))
		}
	}
	if m.narrow() {
		var tabs []string
		for _, p := range m.lay.visiblePanes() {
			t := fmt.Sprintf("%d %s", p+1, paneNames[p])
			if p == m.focus {
				tabs = append(tabs, selectedStyle.Render(t))
			} else {
				tabs = append(tabs, subtleStyle.Render(t))
			}
		}
		right = strings.Join(tabs, subtleStyle.Render(" · "))
	}
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return truncateW(left+strings.Repeat(" ", gap)+right, m.width)
}

func humanInt(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

func (m model) footer() string {
	// À direita: painéis minimizados.
	dock := ""
	if len(m.lay.Minimized) > 0 {
		var ps []string
		for _, p := range m.lay.Minimized {
			ps = append(ps, fmt.Sprintf("[%d] %s", p+1, paneNames[p]))
		}
		dock = warnStyle.Render("▾ " + strings.Join(ps, "  "))
	}
	left := ""
	if m.status != "" {
		st := subtleStyle
		if m.statusErr {
			st = errStyle
		}
		left = st.Render(m.status)
	} else {
		left = helpStyle.Render(m.helpLine())
	}
	room := m.width - lipgloss.Width(dock) - 1
	left = truncateW(left, max(room, 1))
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(dock), 1)
	return truncateW(left+strings.Repeat(" ", gap)+dock, m.width)
}

func (m model) helpLine() string {
	switch m.mode {
	case modeMove:
		return "setas/hjkl movem o painel · enter ou esc termina"
	case modeResize:
		return "→↓ aumentam · ←↑ diminuem · enter ou esc termina"
	}
	base := "ctrl+space comandos · alt+setas foco · "
	switch m.focus {
	case pLog:
		return base + "↑↓ commit · r revisar · enter arquivos · q sair"
	case pFiles:
		return base + "↑↓ arquivo · r revisar · o editor · enter chat"
	case pDiff:
		return base + "↑↓ rolar · o editor · d diff · r revisar"
	case pChat:
		if m.streaming {
			return "esc cancela a resposta · pgup/pgdn rolar"
		}
		return base + "enter envia · alt+enter quebra linha · ctrl+n nova conversa"
	}
	return base
}

// ---- painel: commits ----

func (m model) viewLog(w, h int) string {
	if m.rows == nil {
		return subtleStyle.Render("carregando…")
	}
	if len(m.rows) == 0 {
		return subtleStyle.Render("Repositório sem commits.")
	}
	var b strings.Builder
	end := min(m.logOff+h, len(m.rows))
	for i := m.logOff; i < end; i++ {
		r := m.rows[i]
		var line string
		if r.worktree {
			line = warnStyle.Render("● ") + boldStyle.Render("Mudanças locais") + subtleStyle.Render(fmt.Sprintf(" (%d)", m.dirty))
		} else if r.c.SHA == "" {
			line = subtleStyle.Render(r.c.Graph)
		} else {
			line = subtleStyle.Render(r.c.Graph) + shaStyle.Render(r.c.Short) + " "
			if r.c.Refs != "" {
				line += refStyle.Render("("+compactRefs(r.c.Refs)+")") + " "
			}
			line += r.c.Subject
			if extra := subtleStyle.Render("  " + r.c.Author + ", " + r.c.When); lipgloss.Width(line)+lipgloss.Width(extra) <= w {
				line += extra
			}
		}
		line = truncateW(line, w)
		if i == m.logIdx {
			line = cursorBg.Render(padTo(line, w))
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func compactRefs(s string) string {
	s = strings.ReplaceAll(s, "HEAD -> ", "HEAD→")
	return strings.ReplaceAll(s, "tag: ", "#")
}

func padTo(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// ---- painel: arquivos ----

func (m model) viewFiles(w, h int) string {
	switch {
	case m.files == nil:
		return subtleStyle.Render("carregando…")
	case len(m.files) == 0:
		return subtleStyle.Render("nenhum arquivo alterado")
	}
	var lines []string
	end := min(m.fileOff+h, len(m.files))
	for i := m.fileOff; i < end; i++ {
		f := m.files[i]
		st, ok := statusStyle[f.Status]
		if !ok {
			st = subtleStyle
		}
		stats := addStyle.Render(fmt.Sprintf("+%d", f.Add)) + " " + delStyle.Render(fmt.Sprintf("-%d", f.Del))
		if f.Binary {
			stats = subtleStyle.Render("binário")
		}
		name := f.Path
		room := w - 2 - lipgloss.Width(stats) - 1
		if lipgloss.Width(name) > room {
			name = "…" + name[max(len(name)-room+1, 0):]
		}
		line := st.Render(f.Status) + " " + padTo(name, room) + " " + stats
		if i == m.fileIdx {
			line = cursorBg.Render(padTo(truncateW(line, w), w))
		}
		lines = append(lines, truncateW(line, w))
	}
	return strings.Join(lines, "\n")
}

// ---- painel: chat ----

func (m model) viewChat(w int) string {
	return m.chatView.View() + "\n" + subtleStyle.Render(strings.Repeat("─", w)) + "\n" + m.input.View()
}
