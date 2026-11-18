package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aishiteirai/dante/internal/repo"
	"github.com/charmbracelet/lipgloss"
)

// Painéis de commits e de arquivos: cada commit em duas linhas (assunto; autor, data e
// tamanho), datas relativas em português e arquivos em árvore por pasta.

// ---- datas ----

var meses = []string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}

func relTime(ts int64, now time.Time) string {
	if ts == 0 {
		return ""
	}
	t := time.Unix(ts, 0)
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "agora"
	case d < time.Hour:
		return fmt.Sprintf("há %d min", int(d.Minutes()))
	case d < 24*time.Hour && sameDay(t, now):
		return fmt.Sprintf("há %d h", int(d.Hours()))
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "ontem"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("há %d dias", max(int(d.Hours()/24), 2))
	case t.Year() == now.Year():
		return fmt.Sprintf("%d %s", t.Day(), meses[t.Month()-1])
	}
	return fmt.Sprintf("%d %s %d", t.Day(), meses[t.Month()-1], t.Year())
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// dateGroup agrupa commits em Hoje, Ontem, Esta semana, Este mês e Mais antigos.
func dateGroup(ts int64, now time.Time) string {
	t := time.Unix(ts, 0)
	switch {
	case sameDay(t, now):
		return "HOJE"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "ONTEM"
	case now.Sub(t) < 7*24*time.Hour:
		return "ESTA SEMANA"
	case now.Sub(t) < 31*24*time.Hour:
		return "ESTE MÊS"
	}
	return "MAIS ANTIGOS"
}

// ---- grafo ----

// graphCont é o prefixo do grafo na segunda linha do commit: as linhas verticais
// continuam e o resto vira espaço.
func graphCont(g string) string {
	var b strings.Builder
	for _, r := range g {
		switch r {
		case '*', '|':
			b.WriteRune('│')
		default:
			b.WriteRune(' ')
		}
	}
	return b.String()
}

func graphLine(g string) string { // linhas só de grafo e o prefixo da primeira linha
	return strings.NewReplacer("|", "│", "/", "╱", "\\", "╲", "_", "─").Replace(g)
}

func linearHistory(rows []logRow) bool {
	for _, r := range rows {
		if !r.worktree && strings.TrimSpace(r.c.Graph) != "*" {
			return false
		}
	}
	return true
}

func firstName(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return s
}

// ---- painel de commits ----

// logLine é uma linha desenhada do painel de commits; row aponta para m.rows (-1 = título
// de grupo de datas).
type logLine struct {
	row  int
	text string
}

// logLines monta as linhas do painel. Cada commit ocupa duas linhas.
func (m model) logLines(w int) []logLine {
	now := time.Now()
	linear := linearHistory(m.rows)
	var out []logLine
	group := ""
	for i, r := range m.rows {
		sel := i == m.logIdx
		mark := subtleStyle.Render("○")
		if sel {
			mark = selectedStyle.Render("●")
		}
		if r.worktree {
			l1 := warnStyle.Render("● ") + boldStyle.Render("Mudanças locais")
			l2 := "  " + subtleStyle.Render(plural(m.dirty, "arquivo alterado", "arquivos alterados"))
			out = append(out, m.logRow(i, l1, w, sel), m.logRow(i, l2, w, sel))
			continue
		}
		c := r.c
		if c.SHA == "" { // linha só de grafo, entre ramos
			out = append(out, logLine{row: i, text: subtleStyle.Render(graphLine(c.Graph))})
			continue
		}
		if linear {
			if g := dateGroup(c.Time, now); g != group {
				group = g
				out = append(out, logLine{row: -1, text: subtleStyle.Render(g)})
			}
		}
		// O marcador ●/○ ocupa o lugar do * do grafo, sem deslocar as colunas.
		l1 := mark + " "
		if !linear {
			g := graphLine(c.Graph)
			if k := strings.Index(g, "*"); k >= 0 {
				l1 = subtleStyle.Render(g[:k]) + mark + subtleStyle.Render(g[k+1:])
			}
		}
		l1 += shaStyle.Render(c.Short) + " "
		if c.Refs != "" {
			l1 += refStyle.Render("‹"+compactRefs(c.Refs)+"›") + " "
		}
		subj := c.Subject
		if sel {
			subj = boldStyle.Render(subj)
		}
		l1 += subj

		cont := "  "
		if !linear {
			cont = subtleStyle.Render(graphCont(c.Graph))
		}
		meta := []string{firstName(c.Author), relTime(c.Time, now)}
		l2 := cont + subtleStyle.Render(strings.Join(meta, " · "))
		if st, ok := m.stats[c.SHA]; ok {
			l2 += subtleStyle.Render(fmt.Sprintf(" · %d arq. ", st.Files)) + plusMinus(st.Add, st.Del) + " " + changeBar(st.Add, st.Del, 6)
		}
		if m.reviewed[c.SHA] {
			l2 += " " + okStyle.Render("✓ revisado")
		}
		out = append(out, m.logRow(i, l1, w, sel), m.logRow(i, l2, w, sel))
	}
	return out
}

func (m model) logRow(i int, s string, w int, sel bool) logLine {
	s = truncateW(s, w)
	if sel {
		s = cursorBg.Render(padTo(s, w))
	}
	return logLine{row: i, text: s}
}

// fixLogScroll mantém as duas linhas do commit selecionado visíveis.
func (m *model) fixLogScroll() {
	lines := m.logLines(80)
	first, last := -1, -1
	for i, l := range lines {
		if l.row == m.logIdx {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return
	}
	h := m.logHeight()
	// Mostra também o título do grupo logo acima, quando houver.
	if first > 0 && lines[first-1].row == -1 {
		first--
	}
	if first < m.logOff {
		m.logOff = first
	}
	if last >= m.logOff+h {
		m.logOff = last - h + 1
	}
	m.logOff = max(m.logOff, 0)
}

func (m model) viewLog(w, h int) string {
	if m.rows == nil {
		return subtleStyle.Render("carregando…")
	}
	if len(m.rows) == 0 {
		return subtleStyle.Render("Repositório sem commits.")
	}
	lines := m.logLines(w)
	var b strings.Builder
	for i := m.logOff; i < len(lines) && i < m.logOff+h; i++ {
		b.WriteString(lines[i].text + "\n")
	}
	return b.String()
}

// ---- painel de arquivos ----

// sortFiles ordena por pasta e nome, a ordem em que a árvore aparece.
func sortFiles(fs []repo.FileChange) {
	sort.SliceStable(fs, func(i, j int) bool {
		di, dj := path.Dir(fs[i].Path), path.Dir(fs[j].Path)
		if di != dj {
			return di < dj
		}
		return fs[i].Path < fs[j].Path
	})
}

var fileWords = map[string]string{
	"A": "+ novo", "M": "~ modif.", "D": "− removido", "R": "→ renom.", "C": "⧉ cópia",
	"T": "~ tipo", "U": "! conflito", "?": "+ novo",
}

type fileLine struct {
	file int // índice em m.files (-1 = título de pasta)
	text string
}

func (m model) fileLines(w int) []fileLine {
	var out []fileLine
	dir := "\x00"
	for i, f := range m.files {
		d := path.Dir(f.Path)
		if d != dir {
			dir = d
			if d != "." {
				out = append(out, fileLine{file: -1, text: truncateW(subtleStyle.Render("▾ "+d+"/"), w)})
			}
		}
		st, ok := statusStyle[f.Status]
		if !ok {
			st = subtleStyle
		}
		word, ok := fileWords[f.Status]
		if !ok {
			word = f.Status
		}
		name := path.Base(f.Path)
		if f.OldPath != "" && f.Status == "R" {
			name = path.Base(f.OldPath) + " → " + name
		}
		stats := plusMinus(f.Add, f.Del)
		if f.Binary {
			stats = subtleStyle.Render("binário")
		}
		indent := "  "
		if d == "." {
			indent = ""
		}
		left := indent + st.Render(padTo(word, 11)) + " "
		room := w - lipgloss.Width(left) - lipgloss.Width(stats) - 1
		if i == m.fileIdx {
			name = boldStyle.Render(name)
		}
		line := left + padTo(truncateW(name, max(room, 4)), max(room, 4)) + " " + stats
		line = truncateW(line, w)
		if i == m.fileIdx {
			line = cursorBg.Render(padTo(line, w))
		}
		out = append(out, fileLine{file: i, text: line})
	}
	return out
}

func (m *model) fixFilesScroll() {
	lines := m.fileLines(80)
	at := -1
	for i, l := range lines {
		if l.file == m.fileIdx {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	if at > 0 && lines[at-1].file == -1 { // mostra a pasta junto
		at--
	}
	h := m.filesRows()
	if at < m.fileOff {
		m.fileOff = at
	}
	if at+1 >= m.fileOff+h {
		m.fileOff = at + 2 - h
	}
	m.fileOff = max(m.fileOff, 0)
}

func (m model) viewFiles(w, h int) string {
	switch {
	case m.files == nil:
		return subtleStyle.Render("carregando…")
	case len(m.files) == 0:
		return subtleStyle.Render("nenhum arquivo alterado")
	}
	lines := m.fileLines(w)
	var b strings.Builder
	for i := m.fileOff; i < len(lines) && i < m.fileOff+h; i++ {
		b.WriteString(lines[i].text + "\n")
	}
	return b.String()
}
