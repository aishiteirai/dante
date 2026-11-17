package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const narrowWidth = 110

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
	}
	return m.viewMain()
}

// ---- geometria ----

func (m model) narrow() bool { return m.width < narrowWidth }

// Larguras externas (com borda) dos três painéis.
func (m model) paneWidths() (int, int, int) {
	if m.narrow() {
		return m.width, m.width, m.width
	}
	wl := m.width * 28 / 100
	wf := m.width * 32 / 100
	return wl, wf, m.width - wl - wf
}

func (m model) paneHeight() int { return max(m.height-2, 8) } // header + rodapé

func (m model) innerHeight() int { return m.paneHeight() - 2 }

func (m model) logHeight() int { return max(m.innerHeight()-1, 1) } // menos o título

func (m model) filesListHeight() int {
	n := max(len(m.files), 1)
	return min(n, max(3, (m.innerHeight()-2)/3))
}

func (m *model) resize() {
	if m.width == 0 {
		return
	}
	_, wf, wc := m.paneWidths()
	m.preview.Width = wf - 4
	m.preview.Height = max(m.innerHeight()-m.filesListHeight()-2, 3)
	m.chatView.Width = wc - 4
	m.chatView.Height = max(m.innerHeight()-1-1-m.input.Height(), 3)
	m.input.SetWidth(wc - 4)
	m.renderPreview()
	m.refreshChat()
}

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

// ---- tela principal ----

func (m model) viewMain() string {
	wl, wf, wc := m.paneWidths()
	h := m.paneHeight()
	logP := m.box(m.viewLog(wl-4), wl, h, m.focus == pLog)
	filesP := m.box(m.viewFiles(wf-4), wf, h, m.focus == pFiles)
	chatP := m.box(m.viewChat(wc-4), wc, h, m.focus == pChat)
	var body string
	if m.narrow() {
		body = []string{logP, filesP, chatP}[m.focus]
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, logP, filesP, chatP)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.header(), body, m.footer())
}

func (m model) box(content string, w, h int, focused bool) string {
	st := paneStyle
	if focused {
		st = paneFocus
	}
	// Corta o conteúdo na altura exata para os painéis ficarem alinhados.
	lines := strings.Split(content, "\n")
	inner := h - 2
	if len(lines) > inner {
		lines = lines[:inner]
	}
	for len(lines) < inner {
		lines = append(lines, "")
	}
	return st.Width(w - 2).Height(inner).Render(strings.Join(lines, "\n"))
}

func (m model) paneTitle(n int, title, extra string, w int) string {
	num := subtleStyle.Render(fmt.Sprintf("[%d] ", n))
	t := boldStyle.Render(title)
	if pane(n-1) == m.focus {
		t = titleStyle.Render(title)
	}
	return truncateW(num+t+" "+subtleStyle.Render(extra), w)
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
		tabs := []string{"Commits", "Arquivos", "Chat"}
		for i, t := range tabs {
			if pane(i) == m.focus {
				tabs[i] = selectedStyle.Render(t)
			} else {
				tabs[i] = subtleStyle.Render(t)
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
	if m.status != "" {
		st := subtleStyle
		if m.statusErr {
			st = errStyle
		}
		return truncateW(st.Render(m.status), m.width)
	}
	var help string
	switch m.focus {
	case pLog:
		help = "↑↓ commit · enter arquivos · r revisar alvo · m modelo · u atualizar · esc trocar repositório · tab painel · q sair"
	case pFiles:
		help = "↑↓ arquivo · J/K rolar · r revisar arquivo · o abrir no editor · d voltar ao diff · enter chat · tab painel · q sair"
	case pChat:
		help = "enter enviar · alt+enter nova linha · pgup/pgdn rolar · ctrl+n nova conversa · esc sair do chat"
		if m.streaming {
			help = "esc cancela a resposta · pgup/pgdn rolar"
		}
	}
	return helpStyle.Render(truncateW(help, m.width))
}

// ---- painel 1: commits ----

func (m model) viewLog(w int) string {
	var b strings.Builder
	b.WriteString(m.paneTitle(1, "Commits", "", w) + "\n")
	if m.rows == nil {
		return b.String() + subtleStyle.Render("carregando…")
	}
	if len(m.rows) == 0 {
		return b.String() + subtleStyle.Render("Repositório sem commits.")
	}
	h := m.logHeight()
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

// ---- painel 2: arquivos + prévia ----

func (m model) viewFiles(w int) string {
	var b strings.Builder
	label := m.targetLabel()
	b.WriteString(m.paneTitle(2, "Arquivos", label, w) + "\n")
	lh := m.filesListHeight()
	var lines []string
	switch {
	case m.files == nil:
		lines = append(lines, subtleStyle.Render("carregando…"))
	case len(m.files) == 0:
		lines = append(lines, subtleStyle.Render("nenhum arquivo alterado"))
	}
	end := min(m.fileOff+lh, len(m.files))
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
	for len(lines) < lh {
		lines = append(lines, "")
	}
	b.WriteString(strings.Join(lines, "\n") + "\n")
	title := m.previewTitle
	if title == "" {
		title = "diff"
	}
	if m.previewIsDoc {
		title = labelStyle.Render(title)
	} else {
		title = subtleStyle.Render(title)
	}
	sep := subtleStyle.Render("── ") + title + " "
	b.WriteString(truncateW(sep+subtleStyle.Render(strings.Repeat("─", max(w-lipgloss.Width(sep), 0))), w) + "\n")
	b.WriteString(m.preview.View())
	return b.String()
}

// ---- painel 3: chat ----

func (m model) viewChat(w int) string {
	extra := ""
	if m.modelName == "" {
		extra = "sem modelo (m)"
	}
	var b strings.Builder
	b.WriteString(m.paneTitle(3, "Chat", extra, w) + "\n")
	b.WriteString(m.chatView.View() + "\n")
	b.WriteString(subtleStyle.Render(strings.Repeat("─", w)) + "\n")
	b.WriteString(m.input.View())
	return b.String()
}
