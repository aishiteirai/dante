package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Painel [3] Visualizador: mostra uma coisa por vez, escolhida pelas abas do título.
//   Diff      o arquivo selecionado (padrão)
//   Documento o arquivo que o modelo abriu ou o diff que ele viu

type vTab int

const (
	vtDiff vTab = iota
	vtDoc
)

const (
	vHeaderLines   = 3 // cabeçalho do arquivo: 2 linhas + 1 em branco
	defaultDiffCtx = 3
	maxDiffCtx     = 60
	splitMinWidth  = 120 // largura mínima do painel para o lado a lado automático
)

// Modo de exibição do diff: automático (lado a lado só quando expandido e largo) ou fixo.
const (
	splitAuto = iota
	splitOn
	splitOff
)

func (m *model) setDiff(path, raw string) {
	m.diffPath, m.diffRaw = path, raw
	m.vtab = vtDiff
	m.markLine, m.pendingLine = m.pendingLine, 0
	m.renderPreview()
	m.preview.GotoTop()
	m.scrollToMark()
}

func (m *model) setDoc(title, raw string, isDiff bool) {
	m.docTitle, m.docRaw, m.docIsDiff = title, raw, isDiff
	m.vtab = vtDoc
	m.markLine, m.pendingLine = m.pendingLine, 0
	m.renderPreview()
	m.preview.GotoTop()
	m.scrollToMark()
}

// scrollToMark rola até a linha marcada (referência aberta), deixando 3 linhas acima.
func (m *model) scrollToMark() {
	if m.markLine <= 0 {
		return
	}
	for i, l := range m.rowLines {
		if l >= m.markLine {
			m.preview.SetYOffset(max(i-3, 0))
			return
		}
	}
}

func (m *model) clearPreview() {
	m.diffPath, m.diffRaw, m.hunkStarts, m.hunkLines = "", "", nil, nil
	m.renderPreview()
}

func (m model) splitView() bool {
	switch m.diffSplit {
	case splitOn:
		return true
	case splitOff:
		return false
	}
	return m.zoomed() && m.preview.Width >= splitMinWidth
}

func (m *model) renderPreview() {
	w := max(m.preview.Width, 10)
	m.hunkStarts, m.hunkLines, m.rowLines = nil, nil, nil
	var lines []string
	switch {
	case m.vtab == vtDoc && !m.docIsDiff:
		for _, l := range strings.Split(strings.TrimRight(m.docRaw, "\n"), "\n") {
			l = truncateW(expandTabs(l), w)
			n, numEnd := lineNumberPrefix(l) // "    12  código", como o read_file numera
			if numEnd > 0 {
				if m.markLine > 0 && n == m.markLine {
					l = cursorBg.Render(padTo(markArrow()+l[1:], w))
				} else {
					l = subtleStyle.Render(l[:numEnd]) + l[numEnd:]
				}
			} else if strings.HasPrefix(l, "=== ") { // separador de read_files
				l = labelStyle.Render(l)
			}
			lines = append(lines, l)
			m.rowLines = append(m.rowLines, n)
		}
	default:
		raw := m.diffRaw
		if m.vtab == vtDoc {
			raw = m.docRaw
		}
		if strings.TrimSpace(raw) == "" {
			m.preview.SetContent("")
			return
		}
		d := parseDiff(raw)
		if len(d.hunks) == 0 && !d.binary {
			m.preview.SetContent(subtleStyle.Render(strings.TrimSpace(raw)))
			return
		}
		if m.splitView() {
			lines, m.hunkStarts, m.rowLines = renderSplit(d, w, m.markLine)
		} else {
			lines, m.hunkStarts, m.rowLines = renderUnified(d, w, m.markLine)
		}
		for _, h := range d.hunks {
			m.hunkLines = append(m.hunkLines, h.newStart)
		}
	}
	m.preview.SetContent(strings.Join(lines, "\n"))
}

// lineNumberPrefix lê o número no começo de uma linha numerada pelo read_file
// ("    12  código") e devolve o número e onde o código começa (0 se não houver número).
func lineNumberPrefix(l string) (int, int) {
	t := strings.TrimLeft(l, " ")
	j := strings.Index(t, "  ")
	if j <= 0 {
		return 0, 0
	}
	n, err := strconv.Atoi(t[:j])
	if err != nil {
		return 0, 0
	}
	return n, len(l) - len(t) + j
}

// currentHunk é o trecho no topo da área visível. No fim da rolagem, um trecho perto do
// fim não chega ao topo: aí vale o último trecho visível.
func (m model) currentHunk() int {
	cur := 0
	atEnd := m.preview.YOffset > 0 && m.preview.AtBottom()
	for i, s := range m.hunkStarts {
		if s <= m.preview.YOffset+1 || (atEnd && s < m.preview.YOffset+m.preview.Height) {
			cur = i
		}
	}
	return cur
}

// tabsLabel: "Diff · Documento", com a aba ativa destacada.
func (m model) tabsLabel() string {
	tab := func(name string, active, available bool) string {
		switch {
		case active:
			return lipgloss.NewStyle().Foreground(accent).Bold(true).Underline(true).Render(name)
		case available:
			return boldStyle.Render(name)
		}
		return subtleStyle.Render(name)
	}
	return tab("Diff", m.vtab == vtDiff, true) + subtleStyle.Render(" · ") + tab("Documento", m.vtab == vtDoc, m.docRaw != "")
}

func (m model) visualizerStatus() string {
	if len(m.hunkStarts) == 0 {
		return ""
	}
	s := fmt.Sprintf("trecho %d de %d", m.currentHunk()+1, len(m.hunkStarts))
	if m.splitView() {
		s += " · lado a lado"
	}
	return s
}

// visualizerHeader é o cabeçalho acima do conteúdo: o arquivo, a situação e o tamanho.
func (m model) visualizerHeader(w int) string {
	var l1, l2 string
	switch {
	case m.vtab == vtDoc:
		l1 = boldStyle.Render(m.docTitle)
		l2 = subtleStyle.Render("aberto pelo modelo · [ volta ao diff")
	case len(m.files) > 0 && m.fileIdx < len(m.files):
		f := m.files[m.fileIdx]
		name := f.Path
		if f.OldPath != "" && f.Status == "R" {
			name = f.OldPath + " → " + f.Path
		}
		left := statusBadge(f.Status) + " " + boldStyle.Render(name)
		right := plusMinus(f.Add, f.Del) + " " + changeBar(f.Add, f.Del, 8)
		if f.Binary {
			right = subtleStyle.Render("binário")
		}
		gap := max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
		l1 = truncateW(left+strings.Repeat(" ", gap)+right, w)
		l2 = subtleStyle.Render(m.targetLabel())
		if m.diffCtx != defaultDiffCtx {
			l2 += subtleStyle.Render(fmt.Sprintf(" · %d linhas de contexto", m.diffCtx))
		}
	default:
		l1 = subtleStyle.Render("Escolha um arquivo no painel [2].")
	}
	return truncateW(l1, w) + "\n" + truncateW(l2, w) + "\n"
}

func (m model) updateDiffKeys(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.preview.ScrollUp(1)
	case "down", "j":
		m.preview.ScrollDown(1)
	case "pgup", "ctrl+u":
		m.preview.HalfPageUp()
	case "pgdown", "ctrl+d", " ":
		m.preview.HalfPageDown()
	case "g", "home":
		m.preview.GotoTop()
	case "G", "end":
		m.preview.GotoBottom()
	case "n": // próximo trecho
		if len(m.hunkStarts) > 0 {
			i := m.currentHunk()
			if m.preview.YOffset < m.hunkStarts[i] { // ainda acima da faixa do trecho atual
				m.preview.SetYOffset(m.hunkStarts[i])
			} else if i+1 < len(m.hunkStarts) {
				m.preview.SetYOffset(m.hunkStarts[i+1])
			}
		}
	case "N": // trecho anterior
		if len(m.hunkStarts) > 0 {
			i := m.currentHunk()
			if m.preview.YOffset > m.hunkStarts[i] {
				m.preview.SetYOffset(m.hunkStarts[i])
			} else if i > 0 {
				m.preview.SetYOffset(m.hunkStarts[i-1])
			}
		}
	case "J", "K": // próximo / anterior arquivo
		if len(m.files) == 0 {
			return m, nil
		}
		prev := m.fileIdx
		if key == "J" {
			m.fileIdx = min(m.fileIdx+1, len(m.files)-1)
		} else {
			m.fileIdx = max(m.fileIdx-1, 0)
		}
		m.fixFilesScroll()
		if m.fileIdx != prev {
			return m, m.previewSelected()
		}
	case "[", "]":
		if m.vtab == vtDiff && m.docRaw != "" {
			m.vtab = vtDoc
		} else {
			m.vtab = vtDiff
		}
		m.renderPreview()
		m.preview.GotoTop()
	case "d":
		m.vtab = vtDiff
		m.renderPreview()
	case "s":
		if m.splitView() {
			m.diffSplit = splitOff
		} else {
			m.diffSplit = splitOn
		}
		m.renderPreview()
	case "+", "=":
		if m.diffCtx < maxDiffCtx {
			m.diffCtx = min(m.diffCtx*2+2, maxDiffCtx)
			return m, m.previewSelected()
		}
	case "-":
		if m.diffCtx > defaultDiffCtx {
			m.diffCtx = max((m.diffCtx-2)/2, defaultDiffCtx)
			return m, m.previewSelected()
		}
	case "o", "e":
		if len(m.files) > 0 {
			f := m.files[m.fileIdx]
			if f.Status == "D" {
				m.setErr("o arquivo foi removido neste alvo")
				return m, nil
			}
			line := 1
			if h := m.currentHunk(); h < len(m.hunkLines) {
				line = m.hunkLines[h]
			}
			return m, openInEditor(m.repo.Root, f.Path, line)
		}
	case "r":
		if len(m.files) > 0 {
			return m.quickReview(m.files[m.fileIdx].Path)
		}
	case "esc":
		return m.focusPane(pFiles)
	case "enter":
		return m.focusPane(pChat)
	}
	return m, nil
}

// ---- commits já revisados (guardados por repositório) ----

func reviewedPath(root string) string {
	h := sha1.Sum([]byte(root))
	return filepath.Join(dataHome(), "revisados", repoDisplayName(root)+"-"+hex.EncodeToString(h[:4])+".json")
}

func loadReviewed(root string) map[string]bool {
	out := map[string]bool{}
	var shas []string
	if b, err := os.ReadFile(reviewedPath(root)); err == nil {
		_ = json.Unmarshal(b, &shas)
	}
	for _, s := range shas {
		out[s] = true
	}
	return out
}

func saveReviewed(root string, set map[string]bool) {
	var shas []string
	for s := range set {
		shas = append(shas, s)
	}
	p := reviewedPath(root)
	if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
		b, _ := json.Marshal(shas)
		_ = os.WriteFile(p, b, 0o644)
	}
}

// ---- tamanho dos commits (carregado depois do log) ----

type statsMsg struct {
	root  string
	stats map[string]repo.CommitStat
}

func loadStats(r repo.Repo, rows []logRow) tea.Cmd {
	var shas []string
	for _, row := range rows {
		if row.c.SHA != "" {
			shas = append(shas, row.c.SHA)
		}
	}
	return func() tea.Msg {
		st, _ := r.Stats(shas)
		return statsMsg{root: r.Root, stats: st}
	}
}
