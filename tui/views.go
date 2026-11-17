package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const wideBreakpoint = 110

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	var body, help string
	switch m.screen {
	case scrMenu:
		body, help = m.viewMenu(), "↑↓ navegar · enter escolher · e esforço · q sair"
	case scrBase:
		body, help = m.viewBase(), "enter revisar · esc voltar"
	case scrCommits:
		body, help = m.viewCommits(), "espaço marcar · a marcar não revisados até aqui · c limpar · v ver revisão anterior · enter revisar · esc voltar"
	case scrRunning:
		body, help = m.viewRunning(), "esc cancelar"
	case scrResults:
		return m.viewResults()
	case scrHistory:
		body, help = m.viewHistory(), "↑↓ navegar · enter abrir · r revisar o commit de novo · esc voltar"
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.header(), body, m.footer(help))
}

func (m model) header() string {
	left := titleStyle.Render("revisor") + subtleStyle.Render(" · "+m.cfg.RepoName)
	right := subtleStyle.Render("esforço ") + boldStyle.Render(m.cfg.Effort)
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right + "\n"
}

func (m model) footer(help string) string {
	out := ""
	if m.status != "" {
		out = errStyle.Render(ansi.Truncate(m.status, m.width, "…")) + "\n"
	}
	return out + helpStyle.Render(ansi.Truncate(help, m.width, "…"))
}

// Altura disponível para listas nas telas simples (header 2 + título 2 + footer 2).
func (m model) listHeight() int { return max(m.height-7, 3) }

func (m model) pad(content string) string {
	used := lipgloss.Height(content)
	extra := m.height - used - 3 // header(2) + footer(1)
	if m.status != "" {
		extra--
	}
	if extra > 0 {
		content += strings.Repeat("\n", extra)
	}
	return content
}

// ---- Menu ----

func (m model) viewMenu() string {
	var b strings.Builder
	b.WriteString(boldStyle.Render("O que revisar?") + "\n\n")
	for i, it := range menuItems {
		cursor, title := "  ", it.title
		if i == m.menuIdx {
			cursor, title = selectedStyle.Render("▸ "), selectedStyle.Render(title)
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n     %s\n", cursor, subtleStyle.Render(itoa(i+1)), title, subtleStyle.Render(it.desc)))
	}
	return m.pad(b.String())
}

func (m model) viewBase() string {
	s := boldStyle.Render("Comparar a branch atual com:") + "\n\n" + m.baseInput.View() + "\n\n" +
		subtleStyle.Render("Ex.: main, origin/main, v1.2.0")
	return m.pad(s)
}

// ---- Commits ----

func (m model) viewCommits() string {
	var b strings.Builder
	sel := len(m.selected)
	title := boldStyle.Render("Commits") + subtleStyle.Render(fmt.Sprintf("  %d selecionado(s) · ✓ = já revisado", sel))
	b.WriteString(title + "\n\n")
	if m.commits == nil {
		return m.pad(b.String() + subtleStyle.Render("carregando…"))
	}
	if len(m.commits) == 0 {
		return m.pad(b.String() + subtleStyle.Render("Nenhum commit."))
	}
	h := m.listHeight()
	end := min(m.commitOff+h, len(m.commits))
	for i := m.commitOff; i < end; i++ {
		c := m.commits[i]
		box := "[ ]"
		if m.selected[c.SHA] {
			box = selectedStyle.Render("[x]")
		}
		mark := "  "
		if r, ok := m.reviewedSHA[c.SHA]; ok {
			mark = okStyle.Render("✓ ")
			if s := sevCompact(r.Achados); s != "" {
				mark = s
			}
		}
		cursor := "  "
		subj := c.Subject
		if i == m.commitIdx {
			cursor = selectedStyle.Render("▸ ")
			subj = selectedStyle.Render(subj)
		}
		meta := subtleStyle.Render(fmt.Sprintf(" %s, %s", c.Author, c.When))
		line := fmt.Sprintf("%s%s %s %s %s%s", cursor, box, subtleStyle.Render(c.Short), padRight(mark, 10), subj, meta)
		b.WriteString(ansi.Truncate(line, m.width, "…") + "\n")
	}
	return m.pad(b.String())
}

// ---- Execução ----

func (m model) viewRunning() string {
	var b strings.Builder
	prog := ""
	if m.runTotal > 0 {
		prog = fmt.Sprintf("[%d/%d] ", max(m.runIdx, 1), m.runTotal)
	}
	elapsed := time.Since(m.runStart).Round(time.Second)
	b.WriteString(m.spin.View() + " " + boldStyle.Render(prog+m.runDesc) +
		subtleStyle.Render(fmt.Sprintf("  %s · US$ %.3f", elapsed, m.runCost)) + "\n\n")
	h := m.listHeight()
	start := max(len(m.runLog)-h, 0)
	for _, l := range m.runLog[start:] {
		b.WriteString(ansi.Truncate(l, m.width, "…") + "\n")
	}
	return m.pad(b.String())
}

// ---- Resultados ----

func (m model) resultsHeader() string {
	if len(m.reviews) == 0 {
		return ""
	}
	r := m.reviews[m.revIdx]
	pos := ""
	if len(m.reviews) > 1 {
		pos = fmt.Sprintf("%d/%d · ", m.revIdx+1, len(m.reviews))
	}
	title := titleStyle.Render("Revisão "+pos) + boldStyle.Render(r.Alvo.Descricao)
	meta := []string{}
	if s := sevSummary(r.Achados); s != "" {
		meta = append(meta, s)
	} else if r.Erro == nil {
		meta = append(meta, okStyle.Render("✓ nenhum problema encontrado"))
	}
	if r.CustoUSD != nil {
		meta = append(meta, subtleStyle.Render(fmt.Sprintf("US$ %.3f", *r.CustoUSD)))
	}
	if len(r.Data) >= 16 {
		meta = append(meta, subtleStyle.Render(strings.Replace(r.Data[:16], "T", " ", 1)+" UTC"))
	}
	if m.minSev < len(severities)-1 {
		meta = append(meta, selectedStyle.Render("filtro: "+severities[m.minSev]+" ou mais grave"))
	}
	out := ansi.Truncate(title, m.width, "…") + "\n" + strings.Join(meta, subtleStyle.Render("  ·  "))
	if r.Erro != nil {
		out += "\n" + errStyle.Render("⚠ A revisão não terminou: "+*r.Erro)
	}
	if r.Resumo != "" {
		resumo := lipgloss.NewStyle().Width(m.width).Render(r.Resumo)
		lines := strings.Split(resumo, "\n")
		if len(lines) > 4 {
			lines = append(lines[:4], subtleStyle.Render("…"))
		}
		out += "\n\n" + strings.Join(lines, "\n")
	}
	return out
}

func (m model) wide() bool { return m.width >= wideBreakpoint }

func (m model) resultsBodyHeight() int {
	footer := 1
	if m.status != "" {
		footer++
	}
	return max(m.height-lipgloss.Height(m.resultsHeader())-1-footer, 6)
}

func (m model) listWidth() int {
	if m.wide() {
		return m.width * 42 / 100
	}
	return m.width
}

// Linhas internas (sem borda) do painel da lista.
func (m model) resultsListHeight() int {
	body := m.resultsBodyHeight()
	if m.wide() {
		return body - 2
	}
	n := max(len(m.visibleFindings()), 1)
	return min(n, max(body/3, 3))
}

func (m *model) layoutDetail() {
	if m.width == 0 {
		return
	}
	body := m.resultsBodyHeight()
	if m.wide() {
		m.detail.Width = m.width - m.listWidth() - 4
		m.detail.Height = body - 2
	} else {
		m.detail.Width = m.width - 4
		m.detail.Height = max(body-m.resultsListHeight()-4, 3)
	}
	m.detail.SetContent(m.detailContent())
}

func (m model) detailContent() string {
	fs := m.visibleFindings()
	w := max(m.detail.Width, 20)
	wrap := lipgloss.NewStyle().Width(w)
	if len(fs) == 0 {
		if len(m.reviews) > 0 && len(m.reviews[m.revIdx].Achados) > 0 {
			return subtleStyle.Render("Nenhum achado com esse filtro (f muda o filtro).")
		}
		return subtleStyle.Render("Nada a mostrar.")
	}
	f := fs[min(m.findIdx, len(fs)-1)]
	var b strings.Builder
	b.WriteString(wrap.Inherit(boldStyle).Render(f.Titulo) + "\n")
	b.WriteString(sevStyle(f.Severidade).Render("● "+f.Severidade) + subtleStyle.Render(
		fmt.Sprintf("  %s  ·  %s:%d  ·  confiança %s", f.Categoria, f.Arquivo, f.Linha, f.Confianca)) + "\n\n")
	b.WriteString(wrap.Render(f.Descricao) + "\n")
	if f.Cenario != "" {
		b.WriteString("\n" + labelStyle.Render("Cenário") + "\n" + wrap.Render(f.Cenario) + "\n")
	}
	if f.Sugestao != "" {
		b.WriteString("\n" + labelStyle.Render("Sugestão") + "\n" + wrap.Render(f.Sugestao) + "\n")
	}
	return b.String()
}

func (m model) viewFindingList() string {
	fs := m.visibleFindings()
	inner := m.listWidth() - 4
	h := m.resultsListHeight()
	var lines []string
	if len(fs) == 0 {
		lines = append(lines, subtleStyle.Render("sem achados"))
	}
	end := min(m.findOff+h, len(fs))
	for i := m.findOff; i < end; i++ {
		f := fs[i]
		loc := fmt.Sprintf("%s:%d", shortPath(f.Arquivo), f.Linha)
		title := f.Titulo
		cursor := "  "
		if i == m.findIdx {
			cursor = selectedStyle.Render("▸ ")
			title = selectedStyle.Render(title)
		}
		line := cursor + sevDot(f.Severidade) + " " + subtleStyle.Render(loc) + " " + title
		lines = append(lines, ansi.Truncate(line, inner, "…"))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return panelStyle.Width(m.listWidth() - 2).Render(strings.Join(lines, "\n"))
}

func (m model) viewResults() string {
	list := m.viewFindingList()
	detail := panelStyle.Width(m.detail.Width + 2).Render(m.detail.View())
	var body string
	if m.wide() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
	} else {
		body = lipgloss.JoinVertical(lipgloss.Left, list, detail)
	}
	help := "↑↓ achado · o/enter abrir no editor · f filtro · pgup/pgdn rolar · esc voltar"
	if len(m.reviews) > 1 {
		help = "tab/←→ revisão · " + help
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.resultsHeader(), "", body, m.footer(help))
}

// ---- Histórico ----

func (m model) viewHistory() string {
	var b strings.Builder
	b.WriteString(boldStyle.Render("Histórico de revisões") + subtleStyle.Render(fmt.Sprintf("  %d revisão(ões)", len(m.history))) + "\n\n")
	if len(m.history) == 0 {
		return m.pad(b.String() + subtleStyle.Render("Nenhuma revisão ainda."))
	}
	h := m.listHeight()
	end := min(m.histOff+h, len(m.history))
	for i := m.histOff; i < end; i++ {
		r := m.history[i]
		date := ""
		if len(r.Data) >= 16 {
			date = strings.Replace(r.Data[:16], "T", " ", 1)
		}
		sev := sevCompact(r.Achados)
		if r.Erro != nil {
			sev = errStyle.Render("⚠ erro")
		} else if sev == "" {
			sev = okStyle.Render("✓")
		}
		cost := ""
		if r.CustoUSD != nil {
			cost = fmt.Sprintf("US$ %.3f", *r.CustoUSD)
		}
		cursor, desc := "  ", r.Alvo.Descricao
		if i == m.histIdx {
			cursor, desc = selectedStyle.Render("▸ "), selectedStyle.Render(desc)
		}
		line := fmt.Sprintf("%s%s  %s %s  %s", cursor, subtleStyle.Render(date), padRight(sev, 14), desc, subtleStyle.Render(cost))
		b.WriteString(ansi.Truncate(line, m.width, "…") + "\n")
	}
	return m.pad(b.String())
}

// ---- utilidades ----

func padRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func shortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		return "…/" + strings.Join(parts[len(parts)-2:], "/")
	}
	return p
}
