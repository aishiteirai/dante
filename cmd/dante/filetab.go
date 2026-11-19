package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Aba Arquivo do Visualizador: o arquivo inteiro na versão do alvo, com as linhas alteradas
// marcadas na margem. n/N andam entre as mudanças (ou entre os resultados da busca), / busca,
// : vai para uma linha, e clique + f explica a função.

type fileTab struct {
	path, ref string
	lines     []string
	changed   map[int]bool // linhas novas ou alteradas no alvo
	search    string
	matches   []int // linhas com a busca
}

type fileTabMsg struct {
	path, ref string
	lines     []string
	line      int
	err       error
}

// openFileTab carrega o arquivo e o mostra na aba Arquivo, rolado até a linha.
func (m model) openFileTab(path, ref string, line int) (tea.Model, tea.Cmd) {
	if path == "" {
		m.setInfo("escolha um arquivo")
		return m, nil
	}
	r := m.repo
	return m, func() tea.Msg {
		ls, err := r.FileContent(ref, path)
		return fileTabMsg{path: path, ref: ref, lines: ls, line: line, err: err}
	}
}

func (m model) onFileTab(msg fileTabMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setErr("não consegui abrir o arquivo: " + msg.err.Error())
		return m, nil
	}
	ft := &fileTab{path: msg.path, ref: msg.ref, lines: msg.lines, changed: map[int]bool{}}
	// Linhas alteradas: do diff do alvo, quando é o mesmo arquivo.
	if m.diffPath == msg.path && m.diffRaw != "" && msg.ref == repo.Tip(m.target) {
		for _, h := range parseDiff(m.diffRaw).hunks {
			for _, l := range h.lines {
				if l.kind == dlAdd {
					ft.changed[l.new] = true
				}
			}
		}
	}
	m.ftab = ft
	if m.lay.isMinimized(pDiff) {
		m.lay.restore(pDiff)
		m.layoutChanged()
	}
	m.vtab = vtFile
	m.markLine = msg.line
	m.renderPreview()
	m.scrollToLine(msg.line)
	return m, nil
}

// scrollToLine deixa a linha na tela, com 3 linhas acima.
func (m *model) scrollToLine(n int) {
	if n <= 0 {
		m.preview.GotoTop()
		return
	}
	m.preview.SetYOffset(max(n-4, 0))
}

func (m *model) renderFileTab(w int) []string {
	ft := m.ftab
	if ft == nil {
		return []string{subtleStyle.Render("Nenhum arquivo aberto: v no diff, ou enter num arquivo do explorador (p no Navegador).")}
	}
	digits := len(strconv.Itoa(len(ft.lines)))
	mark := lipgloss.NewStyle().Foreground(okColor).Render("▎")
	var out []string
	m.rowLines = m.rowLines[:0]
	for i, l := range ft.lines {
		n := i + 1
		gutter := " "
		if ft.changed[n] {
			gutter = mark
		}
		num := fmt.Sprintf("%*d ", digits, n)
		text := expandTabs(l)
		if ft.search != "" && strings.Contains(strings.ToLower(text), strings.ToLower(ft.search)) {
			text = highlightAll(text, ft.search)
		}
		line := gutter + subtleStyle.Render(num) + text
		if n == m.markLine {
			line = cursorBg.Render(padTo(truncateW(gutter+markArrow()+num[1:]+ansi.Strip(text), w), w))
		}
		out = append(out, truncateW(line, w))
		m.rowLines = append(m.rowLines, n)
	}
	return out
}

func highlightAll(s, term string) string {
	low, t := strings.ToLower(s), strings.ToLower(term)
	var b strings.Builder
	for {
		i := strings.Index(low, t)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i] + lipgloss.NewStyle().Reverse(true).Render(s[i:i+len(t)]))
		s, low = s[i+len(t):], low[i+len(t):]
	}
}

func (m model) fileTabHeader(w int) (string, string) {
	ft := m.ftab
	if ft == nil {
		return boldStyle.Render("Arquivo"), ""
	}
	where := "no disco"
	if ft.ref != repo.WorkTree {
		where = "em " + shortSHA(ft.ref)
	}
	l1 := boldStyle.Render(ft.path) + subtleStyle.Render(fmt.Sprintf(" · %d linhas · %s", len(ft.lines), where))
	if c := len(ft.changed); c > 0 {
		l1 += lipgloss.NewStyle().Foreground(okColor).Render(fmt.Sprintf(" · ▎%d alteradas", c))
	}
	l2 := "n/N mudança · / busca · : linha · clique + f função · i o que é · x o arquivo · [ ] abas"
	switch {
	case m.fileInput != "":
		l2 = m.fileInput + "▏"
		if m.fileInput[0] == '/' {
			l2 += "  enter busca · esc cancela"
		} else {
			l2 += "  enter vai · esc cancela"
		}
		return l1, selectedStyle.Render(l2)
	case ft.search != "":
		l2 = fmt.Sprintf("busca \"%s\": %s · n/N próximo · esc limpa", ft.search, plural(len(ft.matches), "linha", "linhas"))
	}
	return l1, subtleStyle.Render(l2)
}

// currentFileLine é a linha no topo da tela.
func (m model) currentFileLine() int {
	if m.preview.YOffset < len(m.rowLines) {
		return m.rowLines[m.preview.YOffset]
	}
	return 1
}

// updateFileTabKeys trata as teclas da aba Arquivo; ok=false deixa a tecla seguir.
func (m model) updateFileTabKeys(key string) (tea.Model, tea.Cmd, bool) {
	ft := m.ftab
	if ft == nil {
		return m, nil, false
	}
	jump := func(targets []int, dir int) {
		cur := m.currentFileLine() + 3
		best := 0
		for _, n := range targets {
			if (dir > 0 && n > cur && (best == 0 || n < best)) || (dir < 0 && n < cur-1 && n > best) {
				best = n
			}
		}
		if best > 0 {
			m.scrollToLine(best)
		}
	}
	switch key {
	case "n", "N":
		dir := 1
		if key == "N" {
			dir = -1
		}
		if ft.search != "" {
			jump(ft.matches, dir)
		} else {
			// Começo de cada bloco de linhas alteradas.
			var starts []int
			for n := range ft.changed {
				if !ft.changed[n-1] {
					starts = append(starts, n)
				}
			}
			jump(starts, dir)
		}
	case "/", ":":
		m.fileInput = key
	case "esc":
		if ft.search != "" {
			ft.search, ft.matches = "", nil
			m.renderPreview()
			return m, nil, true
		}
		return m, nil, false
	case "x":
		nm, cmd := m.explainFile(ft.path, ft.ref)
		return nm, cmd, true
	default:
		return m, nil, false
	}
	return m, nil, true
}

// updateFileInput é a linha de busca (/) ou de ir para a linha (:). Captura todas as
// teclas enquanto está aberta, para letras como q não fecharem o dante.
func (m model) updateFileInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	ft := m.ftab
	switch k.String() {
	case "esc":
		m.fileInput = ""
	case "backspace":
		if len(m.fileInput) > 1 {
			r := []rune(m.fileInput)
			m.fileInput = string(r[:len(r)-1])
		}
	case "enter":
		kind, text := m.fileInput[0], strings.TrimSpace(m.fileInput[1:])
		m.fileInput = ""
		if ft == nil || text == "" {
			return m, nil
		}
		if kind == ':' {
			if n, err := strconv.Atoi(text); err == nil {
				m.markLine = min(max(n, 1), len(ft.lines))
				m.renderPreview()
				m.scrollToLine(m.markLine)
			}
			return m, nil
		}
		ft.search, ft.matches = text, nil
		for i, l := range ft.lines {
			if strings.Contains(strings.ToLower(l), strings.ToLower(text)) {
				ft.matches = append(ft.matches, i+1)
			}
		}
		m.renderPreview()
		if len(ft.matches) == 0 {
			m.setInfo("nada encontrado para \"" + text + "\"")
		} else if m.currentFileLine() > ft.matches[0] || ft.matches[0] > m.currentFileLine()+m.preview.Height-1 {
			m.scrollToLine(ft.matches[0])
		}
	default:
		if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
			m.fileInput += string(k.Runes)
			if k.Type == tea.KeySpace {
				m.fileInput += " "
			}
		}
	}
	return m, nil
}

// viewFileOfDiff (v): o arquivo inteiro do diff ou do documento, na linha em que se estava.
func (m model) viewFileOfDiff() (tea.Model, tea.Cmd) {
	p, ref, line, ok := m.funcLocation()
	if !ok {
		m.setInfo("escolha um arquivo no Navegador")
		return m, nil
	}
	return m.openFileTab(p, ref, line)
}
