package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Visual do diff: o diff do git é interpretado em trechos (hunks) e desenhado com
// numeração antiga e nova, fundo colorido, destaque palavra a palavra e as linhas iguais
// entre trechos dobradas. Há dois modos: em sequência e lado a lado.

type dlKind int

const (
	dlCtx dlKind = iota
	dlAdd
	dlDel
)

type diffLine struct {
	kind     dlKind
	old, new int    // números de linha (0 = não existe deste lado)
	text     string // sem o sinal
	hlFrom   int    // trecho alterado dentro da linha, em runas; hlFrom < 0 = sem destaque
	hlTo     int
}

type diffHunk struct {
	oldStart, oldLen int
	newStart, newLen int
	header           string // contexto que o git põe depois do @@ (a função)
	lines            []diffLine
}

type parsedDiff struct {
	hunks  []diffHunk
	binary bool
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

func atoiDef(s string, def int) int {
	if s == "" {
		return def
	}
	n, _ := strconv.Atoi(s)
	return n
}

func parseDiff(raw string) parsedDiff {
	var d parsedDiff
	var cur *diffHunk
	var o, n int
	for _, l := range strings.Split(raw, "\n") {
		if strings.HasPrefix(l, "Binary files ") || strings.HasPrefix(l, "GIT binary patch") {
			d.binary = true
		}
		if m := hunkRe.FindStringSubmatch(l); m != nil {
			d.hunks = append(d.hunks, diffHunk{
				oldStart: atoiDef(m[1], 0), oldLen: atoiDef(m[2], 1),
				newStart: atoiDef(m[3], 0), newLen: atoiDef(m[4], 1),
				header: strings.TrimSpace(m[5]),
			})
			cur = &d.hunks[len(d.hunks)-1]
			o, n = cur.oldStart, cur.newStart
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			cur.lines = append(cur.lines, diffLine{kind: dlAdd, new: n, text: l[1:], hlFrom: -1})
			n++
		case strings.HasPrefix(l, "-"):
			cur.lines = append(cur.lines, diffLine{kind: dlDel, old: o, text: l[1:], hlFrom: -1})
			o++
		case strings.HasPrefix(l, " "):
			cur.lines = append(cur.lines, diffLine{kind: dlCtx, old: o, new: n, text: l[1:], hlFrom: -1})
			o++
			n++
		}
	}
	for i := range d.hunks {
		markWordChanges(d.hunks[i].lines)
	}
	return d
}

// markWordChanges compara cada linha removida com a adicionada correspondente (blocos
// "−" seguidos de "+") e marca só o trecho que mudou, pelo prefixo e sufixo em comum.
func markWordChanges(lines []diffLine) {
	for i := 0; i < len(lines); {
		if lines[i].kind != dlDel {
			i++
			continue
		}
		ds := i
		for i < len(lines) && lines[i].kind == dlDel {
			i++
		}
		as := i
		for i < len(lines) && lines[i].kind == dlAdd {
			i++
		}
		dels, adds := lines[ds:as], lines[as:i]
		for k := 0; k < len(dels) && k < len(adds); k++ {
			a, b := []rune(dels[k].text), []rune(adds[k].text)
			p := 0
			for p < len(a) && p < len(b) && a[p] == b[p] {
				p++
			}
			s := 0
			for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
				s++
			}
			// Só destaca quando há algo em comum (senão a linha inteira já está colorida).
			if p+s == 0 {
				continue
			}
			dels[k].hlFrom, dels[k].hlTo = wordBounds(a, p, len(a)-s)
			adds[k].hlFrom, adds[k].hlTo = wordBounds(b, p, len(b)-s)
		}
	}
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// wordBounds estende o trecho alterado até o começo e o fim das palavras que ele corta,
// para destacar "parei após" em vez de " após".
func wordBounds(r []rune, from, to int) (int, int) {
	for from > 0 && isWordRune(r[from-1]) {
		from--
	}
	for to < len(r) && to > 0 && isWordRune(r[to]) && isWordRune(r[to-1]) {
		to++
	}
	return from, to
}

// ---- cores ----

// Cores com reserva para terminais de 256 e de 16 cores: os tons escuros sutis do modo de
// cor completa viram o mesmo cinza na paleta de 256, então ali o verde e o vermelho são
// mais fortes.
func cc(lightTrue, light256, light16, darkTrue, dark256, dark16 string) lipgloss.CompleteAdaptiveColor {
	return lipgloss.CompleteAdaptiveColor{
		Light: lipgloss.CompleteColor{TrueColor: lightTrue, ANSI256: light256, ANSI: light16},
		Dark:  lipgloss.CompleteColor{TrueColor: darkTrue, ANSI256: dark256, ANSI: dark16},
	}
}

var (
	dAddBg   = cc("#e6ffec", "194", "10", "#11291b", "22", "2")
	dDelBg   = cc("#ffebe9", "224", "9", "#311519", "52", "1")
	dAddHl   = cc("#abf2bc", "157", "2", "#1f6b3a", "28", "10")
	dDelHl   = cc("#ffb8b8", "217", "1", "#7a2630", "88", "9")
	dAddFg   = cc("#116329", "22", "2", "#a8ecc4", "157", "10")
	dDelFg   = cc("#82071e", "88", "1", "#ffb3b3", "217", "9")
	dCtxFg   = cc("#24292f", "235", "0", "#c9ccd3", "252", "7")
	dGutter  = cc("#8c959f", "245", "8", "#5f6673", "242", "8")
	dBandBg  = cc("#ddf4ff", "195", "14", "#172033", "17", "4")
	dBandFg  = cc("#0969da", "25", "4", "#8fb8ff", "111", "15")
	dFillerF = cc("#d0d7de", "252", "7", "#2b303b", "237", "8")
)

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// styledText desenha o texto da linha com o trecho alterado destacado, cortado em w
// colunas e preenchido com o fundo até o fim.
func styledText(l diffLine, w int) string {
	var bg, hl, fg lipgloss.CompleteAdaptiveColor
	switch l.kind {
	case dlAdd:
		bg, hl, fg = dAddBg, dAddHl, dAddFg
	case dlDel:
		bg, hl, fg = dDelBg, dDelHl, dDelFg
	default:
		fg = dCtxFg
	}
	base := lipgloss.NewStyle().Foreground(fg)
	if l.kind != dlCtx {
		base = base.Background(bg)
	}
	strong := base.Background(hl)
	r := []rune(l.text)
	var parts []string
	if l.hlFrom >= 0 && l.hlTo > l.hlFrom && l.hlTo <= len(r) {
		parts = []string{
			base.Render(expandTabs(string(r[:l.hlFrom]))),
			strong.Render(expandTabs(string(r[l.hlFrom:l.hlTo]))),
			base.Render(expandTabs(string(r[l.hlTo:]))),
		}
	} else {
		parts = []string{base.Render(expandTabs(l.text))}
	}
	s := ansi.Truncate(strings.Join(parts, ""), max(w, 1), "…")
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += base.Render(strings.Repeat(" ", pad))
	}
	return s
}

func num(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func signOf(k dlKind) (string, lipgloss.Style) {
	switch k {
	case dlAdd:
		return "+", lipgloss.NewStyle().Foreground(dAddFg).Background(dAddBg).Bold(true)
	case dlDel:
		return "−", lipgloss.NewStyle().Foreground(dDelFg).Background(dDelBg).Bold(true)
	}
	return " ", lipgloss.NewStyle()
}

func band(h diffHunk, w int) string {
	label := fmt.Sprintf("linha %d", h.newStart)
	if h.header != "" {
		label = h.header + " · " + label
	}
	s := "── " + label + " "
	s = ansi.Truncate(s, w, "…")
	s += strings.Repeat("─", max(w-lipgloss.Width(s), 0))
	return lipgloss.NewStyle().Background(dBandBg).Foreground(dBandFg).Render(s)
}

func foldLine(n int, w int, before bool) string {
	what := fmt.Sprintf("⋯ %d linhas iguais · + mostra mais contexto", n)
	if before {
		what = fmt.Sprintf("⋯ %d linhas antes · + mostra mais contexto", n)
	}
	if n == 1 {
		what = strings.Replace(what, "linhas iguais", "linha igual", 1)
		what = strings.Replace(what, "linhas antes", "linha antes", 1)
	}
	return subtleStyle.Italic(true).Render(ansi.Truncate(strings.Repeat(" ", 12)+what, w, "…"))
}

// gaps: linhas iguais escondidas antes de cada trecho.
func hunkGap(hs []diffHunk, i int) int {
	if i == 0 {
		return hs[0].oldStart - 1
	}
	prev := hs[i-1]
	return hs[i].oldStart - (prev.oldStart + prev.oldLen)
}

// renderUnified desenha o diff em sequência. Devolve as linhas, onde começa cada trecho e,
// para cada linha da tela, o número da linha no arquivo novo (0 se não houver). A linha
// `mark` do arquivo novo ganha um ▶ na margem (referência aberta pelo chat).
func renderUnified(d parsedDiff, w, mark int) ([]string, []int, []int) {
	if d.binary && len(d.hunks) == 0 {
		return []string{subtleStyle.Render("arquivo binário: sem diff de texto")}, nil, []int{0}
	}
	var out []string
	var starts, rowLine []int
	add := func(s string, line int) { out = append(out, s); rowLine = append(rowLine, line) }
	gut := lipgloss.NewStyle().Foreground(dGutter)
	textW := max(w-13, 4) // "1234 1234 ± "
	for i, h := range d.hunks {
		if g := hunkGap(d.hunks, i); g > 0 {
			add(foldLine(g, w, i == 0), 0)
		}
		starts = append(starts, len(out))
		add(band(h, w), h.newStart)
		for _, l := range h.lines {
			sign, sst := signOf(l.kind)
			nums := gut.Render(fmt.Sprintf("%4s %4s ", num(l.old), num(l.new)))
			if mark > 0 && l.new == mark {
				nums = markArrow() + gut.Render(fmt.Sprintf("%3s %4s ", num(l.old), num(l.new)))
			}
			add(nums+sst.Render(sign)+sst.UnsetBold().Render(" ")+styledText(l, textW), l.new)
		}
	}
	return out, starts, rowLine
}

func markArrow() string { return lipgloss.NewStyle().Foreground(accent).Bold(true).Render("▶") }

// renderSplit desenha o diff lado a lado: antes à esquerda, depois à direita. Linhas que
// só existem de um lado deixam o outro preenchido com ╱.
func renderSplit(d parsedDiff, w, mark int) ([]string, []int, []int) {
	if d.binary && len(d.hunks) == 0 {
		return []string{subtleStyle.Render("arquivo binário: sem diff de texto")}, nil, []int{0}
	}
	half := (w - 1) / 2
	right := w - 1 - half
	gut := lipgloss.NewStyle().Foreground(dGutter)
	sep := subtleStyle.Render("│")
	filler := func(width int) string {
		return lipgloss.NewStyle().Foreground(dFillerF).Render(strings.Repeat("╱", max(width, 0)))
	}
	side := func(l *diffLine, n int, width int) string {
		if l == nil {
			return filler(width)
		}
		g := gut.Render(fmt.Sprintf("%4s ", num(n)))
		if mark > 0 && l.new == mark && n == l.new {
			g = markArrow() + gut.Render(fmt.Sprintf("%3s ", num(n)))
		}
		return g + styledText(*l, max(width-5, 4))
	}
	head := lipgloss.NewStyle().Foreground(dDelFg).Bold(true).Render(ansi.Truncate(" ANTES", half, "")) +
		strings.Repeat(" ", max(half-6, 0)) + sep +
		lipgloss.NewStyle().Foreground(dAddFg).Bold(true).Render(" DEPOIS")
	out := []string{head}
	rowLine := []int{0}
	var starts []int
	for i, h := range d.hunks {
		if g := hunkGap(d.hunks, i); g > 0 {
			out = append(out, foldLine(g, w, i == 0))
			rowLine = append(rowLine, 0)
		}
		starts = append(starts, len(out))
		out = append(out, band(h, w))
		rowLine = append(rowLine, h.newStart)
		ls := h.lines
		for j := 0; j < len(ls); {
			if ls[j].kind == dlCtx {
				out = append(out, side(&ls[j], ls[j].old, half)+sep+side(&ls[j], ls[j].new, right))
				rowLine = append(rowLine, ls[j].new)
				j++
				continue
			}
			var dels, adds []*diffLine
			for j < len(ls) && ls[j].kind == dlDel {
				dels = append(dels, &ls[j])
				j++
			}
			for j < len(ls) && ls[j].kind == dlAdd {
				adds = append(adds, &ls[j])
				j++
			}
			for k := 0; k < max(len(dels), len(adds)); k++ {
				var dl, al *diffLine
				on, nn := 0, 0
				if k < len(dels) {
					dl, on = dels[k], dels[k].old
				}
				if k < len(adds) {
					al, nn = adds[k], adds[k].new
				}
				out = append(out, side(dl, on, half)+sep+side(al, nn, right))
				rowLine = append(rowLine, nn)
			}
		}
	}
	return out, starts, rowLine
}

// ---- cabeçalho do arquivo no Visualizador ----

var statusWords = map[string]string{
	"A": "NOVO", "M": "MODIFICADO", "D": "REMOVIDO", "R": "RENOMEADO", "C": "COPIADO",
	"T": "TIPO MUDOU", "U": "EM CONFLITO", "?": "NOVO (fora do git)",
}

func statusBadge(status string) string {
	word, ok := statusWords[status]
	if !ok {
		word = status
	}
	fg, bg := cc("#7d4e00", "94", "3", "#f0c674", "222", "11"), cc("#fff8c5", "230", "11", "#3a2f12", "58", "3")
	switch status {
	case "A", "?":
		fg, bg = dAddFg, dAddBg
	case "D":
		fg, bg = dDelFg, dDelBg
	case "R", "C":
		fg, bg = dBandFg, dBandBg
	}
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(true).Render(" " + word + " ")
}

// changeBar desenha a proporção de linhas adicionadas e removidas em `width` células.
func changeBar(add, del, width int) string {
	if add+del == 0 || width <= 0 {
		return ""
	}
	a := (add*width + (add+del)/2) / (add + del)
	if add > 0 && a == 0 {
		a = 1
	}
	if del > 0 && a == width {
		a = width - 1
	}
	return lipgloss.NewStyle().Foreground(okColor).Render(strings.Repeat("█", a)) +
		lipgloss.NewStyle().Foreground(errColor).Render(strings.Repeat("█", width-a))
}

func plusMinus(add, del int) string {
	s := ""
	if add > 0 || del == 0 {
		s = addStyle.Render(fmt.Sprintf("+%d", add))
	}
	if del > 0 {
		if s != "" {
			s += " "
		}
		s += delStyle.Render(fmt.Sprintf("−%d", del))
	}
	return s
}
