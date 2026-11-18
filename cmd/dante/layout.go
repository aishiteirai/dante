package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/aishiteirai/dante/internal/config"
)

// Gerenciador de painéis inspirado no noct (e no i3): colunas de painéis; dentro de cada
// coluna os painéis dividem a altura ou ficam empilhados (só o ativo aberto, os outros
// como uma linha de título). Painéis podem ser minimizados (saem do layout e vão para a
// barra de baixo) ou mostrados em tela cheia.

type paneID int

const (
	pLog  paneID = iota // Navegador: commits e, abertos embaixo de cada um, os arquivos
	pDiff               // Visualizador: diff, documento e mapa
	pChat
	paneCount
)

var paneNames = [paneCount]string{"Navegador", "Visualizador", "Chat"}

type column struct {
	Panes   []paneID  `json:"paineis"`
	Sizes   []float64 `json:"alturas"`
	Width   float64   `json:"largura"`
	Stacked bool      `json:"empilhada,omitempty"`
	Active  paneID    `json:"ativo"` // painel aberto quando a coluna está empilhada
}

type layout struct {
	Cols      []column `json:"colunas"`
	Minimized []paneID `json:"minimizados,omitempty"`
}

type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

func defaultLayout() layout {
	// Como numa IDE: o Navegador à esquerda; à direita, o Visualizador largo em cima e o
	// chat embaixo, com a largura toda.
	return layout{Cols: []column{
		{Panes: []paneID{pLog}, Sizes: []float64{1}, Width: 0.30},
		{Panes: []paneID{pDiff, pChat}, Sizes: []float64{0.58, 0.42}, Width: 0.70},
	}}
}

// ---- persistência: ~/.config/dante/layout.json ----

func layoutPath() string { return filepath.Join(config.Dir(), "layout.json") }

func loadLayout() layout {
	var l layout
	b, err := os.ReadFile(layoutPath())
	if err != nil || json.Unmarshal(b, &l) != nil || !l.valid() {
		return defaultLayout()
	}
	l.normalize()
	return l
}

func (l layout) save() {
	b, _ := json.MarshalIndent(l, "", "  ")
	_ = os.MkdirAll(config.Dir(), 0o700)
	_ = os.WriteFile(layoutPath(), b, 0o644)
}

// Cada painel aparece exatamente uma vez.
func (l layout) valid() bool {
	seen := map[paneID]int{}
	for _, c := range l.Cols {
		for _, p := range c.Panes {
			if p < 0 || p >= paneCount {
				return false
			}
			seen[p]++
		}
	}
	for p := paneID(0); p < paneCount; p++ {
		if seen[p] != 1 {
			return false
		}
	}
	return true
}

// ---- consultas ----

func (l layout) find(p paneID) (ci, pi int) {
	for i, c := range l.Cols {
		for j, q := range c.Panes {
			if q == p {
				return i, j
			}
		}
	}
	return -1, -1
}

func (l layout) isMinimized(p paneID) bool {
	for _, q := range l.Minimized {
		if q == p {
			return true
		}
	}
	return false
}

// Painéis visíveis de uma coluna (os minimizados saem).
func (l layout) visible(c column) (panes []paneID, sizes []float64) {
	for i, p := range c.Panes {
		if !l.isMinimized(p) {
			panes = append(panes, p)
			sizes = append(sizes, c.Sizes[i])
		}
	}
	return
}

func (l layout) visiblePanes() []paneID {
	var out []paneID
	for _, c := range l.Cols {
		ps, _ := l.visible(c)
		out = append(out, ps...)
	}
	return out
}

// ---- edição ----

// normalize remove colunas vazias e acerta pesos.
func (l *layout) normalize() {
	var cols []column
	for _, c := range l.Cols {
		if len(c.Panes) == 0 {
			continue
		}
		if len(c.Sizes) != len(c.Panes) {
			c.Sizes = equal(len(c.Panes))
		}
		c.Sizes = scale(c.Sizes)
		if c.Width <= 0 {
			c.Width = 1
		}
		cols = append(cols, c)
	}
	l.Cols = cols
	ws := make([]float64, len(cols))
	for i, c := range cols {
		ws[i] = c.Width
	}
	ws = scale(ws)
	for i := range l.Cols {
		l.Cols[i].Width = ws[i]
	}
}

func equal(n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = 1 / float64(n)
	}
	return s
}

func scale(xs []float64) []float64 {
	sum := 0.0
	for _, x := range xs {
		sum += max(x, 0.01)
	}
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = max(x, 0.01) / sum
	}
	return out
}

func (l *layout) remove(p paneID) (ci, pi int) {
	ci, pi = l.find(p)
	if ci < 0 {
		return
	}
	c := &l.Cols[ci]
	c.Panes = append(c.Panes[:pi:pi], c.Panes[pi+1:]...)
	c.Sizes = append(c.Sizes[:pi:pi], c.Sizes[pi+1:]...)
	if c.Active == p && len(c.Panes) > 0 {
		c.Active = c.Panes[min(pi, len(c.Panes)-1)]
	}
	return
}

// moveH leva o painel para a coluna vizinha (dir = -1 esquerda, +1 direita), na mesma
// posição. Na borda, um painel que divide a coluna vira uma coluna nova.
func (l *layout) moveH(p paneID, dir int) bool {
	ci, pi := l.find(p)
	if ci < 0 {
		return false
	}
	target := ci + dir
	alone := len(l.Cols[ci].Panes) == 1
	if (target < 0 || target >= len(l.Cols)) && alone {
		return false // já é uma coluna sozinha na borda
	}
	l.remove(p)
	if target < 0 || target >= len(l.Cols) {
		nc := column{Panes: []paneID{p}, Sizes: []float64{1}, Width: 1 / float64(len(l.Cols)+1), Active: p}
		if target < 0 {
			l.Cols = append([]column{nc}, l.Cols...)
		} else {
			l.Cols = append(l.Cols, nc)
		}
	} else {
		t := &l.Cols[target]
		at := min(pi, len(t.Panes))
		t.Panes = append(t.Panes[:at:at], append([]paneID{p}, t.Panes[at:]...)...)
		t.Sizes = equal(len(t.Panes))
		t.Active = p
	}
	if c, _ := l.find(p); c >= 0 { // a coluna de origem volta a dividir por igual
		for i := range l.Cols {
			if len(l.Cols[i].Panes) > 0 && i != c {
				l.Cols[i].Sizes = equal(len(l.Cols[i].Panes))
			}
		}
	}
	l.normalize()
	return true
}

// moveV troca o painel de lugar com o de cima (-1) ou de baixo (+1) na coluna.
func (l *layout) moveV(p paneID, dir int) bool {
	ci, pi := l.find(p)
	if ci < 0 {
		return false
	}
	c := &l.Cols[ci]
	j := pi + dir
	if j < 0 || j >= len(c.Panes) {
		return false
	}
	c.Panes[pi], c.Panes[j] = c.Panes[j], c.Panes[pi]
	c.Sizes[pi], c.Sizes[j] = c.Sizes[j], c.Sizes[pi]
	return true
}

// resizeH aumenta (+) ou diminui (-) a largura da coluna do painel, tirando da vizinha.
func (l *layout) resizeH(p paneID, delta float64) bool {
	ci, _ := l.find(p)
	if ci < 0 || len(l.Cols) < 2 {
		return false
	}
	nb := ci + 1
	if nb >= len(l.Cols) {
		nb = ci - 1
	}
	return shift(&l.Cols[ci].Width, &l.Cols[nb].Width, delta, 0.08)
}

// resizeV muda a altura do painel dentro da coluna, tirando do vizinho.
func (l *layout) resizeV(p paneID, delta float64) bool {
	ci, pi := l.find(p)
	if ci < 0 || l.Cols[ci].Stacked || len(l.Cols[ci].Panes) < 2 {
		return false
	}
	c := &l.Cols[ci]
	nb := pi + 1
	if nb >= len(c.Panes) {
		nb = pi - 1
	}
	return shift(&c.Sizes[pi], &c.Sizes[nb], delta, 0.08)
}

func shift(a, b *float64, delta, minV float64) bool {
	if *a+delta < minV || *b-delta < minV {
		return false
	}
	*a += delta
	*b -= delta
	return true
}

func (l *layout) toggleStack(p paneID) bool {
	ci, _ := l.find(p)
	if ci < 0 || len(l.Cols[ci].Panes) < 2 {
		return false
	}
	l.Cols[ci].Stacked = !l.Cols[ci].Stacked
	l.Cols[ci].Active = p
	return true
}

// minimize tira o painel do layout (vai para a barra de baixo); o último visível fica.
func (l *layout) minimize(p paneID) bool {
	if l.isMinimized(p) || len(l.visiblePanes()) <= 1 {
		return false
	}
	l.Minimized = append(l.Minimized, p)
	return true
}

func (l *layout) restore(p paneID) {
	var out []paneID
	for _, q := range l.Minimized {
		if q != p {
			out = append(out, q)
		}
	}
	l.Minimized = out
}

// ---- geometria ----

// splitInts divide total em partes proporcionais aos pesos, cada uma com pelo menos minV.
func splitInts(total int, weights []float64, minV int) []int {
	n := len(weights)
	out := make([]int, n)
	if n == 0 {
		return out
	}
	ws := scale(weights)
	used := 0
	for i, w := range ws {
		out[i] = max(int(w*float64(total)), minV)
		used += out[i]
	}
	// Ajusta a sobra (ou o excesso) a partir dos maiores.
	for used != total {
		best := 0
		for i := range out {
			if (used < total && out[i] < out[best]) || (used > total && out[i] > out[best]) {
				best = i
			}
		}
		if used < total {
			out[best]++
			used++
		} else {
			if out[best] <= 1 {
				break
			}
			out[best]--
			used--
		}
	}
	return out
}

const (
	minColWidth  = 16
	minPaneRows  = 4
	collapsedRow = 1
)

// compute devolve o retângulo de cada painel visível na área (x0, y0, W, H). Em tela
// cheia, só o painel com foco aparece. Painéis dobrados numa pilha ocupam uma linha.
func (l layout) compute(x0, y0, W, H int, focus paneID, zoom bool) map[paneID]rect {
	out := map[paneID]rect{}
	if zoom {
		out[focus] = rect{x0, y0, W, H}
		return out
	}
	type vcol struct {
		panes []paneID
		sizes []float64
		c     column
	}
	var cols []vcol
	var widths []float64
	for _, c := range l.Cols {
		ps, ss := l.visible(c)
		if len(ps) == 0 {
			continue
		}
		cols = append(cols, vcol{ps, ss, c})
		widths = append(widths, c.Width)
	}
	ws := splitInts(W, widths, minColWidth)
	x := x0
	for i, vc := range cols {
		w := ws[i]
		if vc.c.Stacked && len(vc.panes) > 1 {
			active := vc.c.Active
			if !contains(vc.panes, active) {
				active = vc.panes[0]
			}
			y := y0
			open := H - collapsedRow*(len(vc.panes)-1)
			for _, p := range vc.panes {
				h := collapsedRow
				if p == active {
					h = open
				}
				out[p] = rect{x, y, w, h}
				y += h
			}
		} else {
			hs := splitInts(H, vc.sizes, minPaneRows)
			y := y0
			for j, p := range vc.panes {
				out[p] = rect{x, y, w, hs[j]}
				y += hs[j]
			}
		}
		x += w
	}
	return out
}

func contains(ps []paneID, p paneID) bool {
	for _, q := range ps {
		if q == p {
			return true
		}
	}
	return false
}

// neighbor acha o painel mais próximo na direção (dx, dy), pela geometria, como no i3.
func neighbor(rects map[paneID]rect, from paneID, dx, dy int) (paneID, bool) {
	cur, ok := rects[from]
	if !ok {
		return from, false
	}
	best, bestScore := from, 1<<30
	for p, r := range rects {
		if p == from {
			continue
		}
		var gap, off int
		switch {
		case dx < 0 && r.x+r.w <= cur.x:
			gap, off = cur.x-(r.x+r.w), overlapGap(cur.y, cur.h, r.y, r.h)
		case dx > 0 && r.x >= cur.x+cur.w:
			gap, off = r.x-(cur.x+cur.w), overlapGap(cur.y, cur.h, r.y, r.h)
		case dy < 0 && r.y+r.h <= cur.y:
			gap, off = cur.y-(r.y+r.h), overlapGap(cur.x, cur.w, r.x, r.w)
		case dy > 0 && r.y >= cur.y+cur.h:
			gap, off = r.y-(cur.y+cur.h), overlapGap(cur.x, cur.w, r.x, r.w)
		default:
			continue
		}
		// Empate (dois painéis encostados): vale o de cima, ou o da esquerda.
		score := gap*1000 + off
		if score < bestScore || (score == bestScore && r.y+r.x < rects[best].y+rects[best].x) {
			best, bestScore = p, score
		}
	}
	return best, best != from
}

// 0 quando os intervalos se sobrepõem; senão a distância entre eles.
func overlapGap(a, al, b, bl int) int {
	if a < b+bl && b < a+al {
		return 0
	}
	if b >= a+al {
		return b - (a + al) + 1
	}
	return a - (b + bl) + 1
}
