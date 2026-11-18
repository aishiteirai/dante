package main

import "testing"

func TestComputeCobreATela(t *testing.T) {
	l := defaultLayout()
	rs := l.compute(0, 1, 150, 40, pLog, false)
	if len(rs) != 3 {
		t.Fatalf("rects = %v", rs)
	}
	area := 0
	for _, r := range rs {
		area += r.w * r.h
	}
	if area != 150*40 {
		t.Fatalf("área %d, esperava %d", area, 150*40)
	}
	// Navegador à esquerda; Visualizador em cima do chat, os dois com a mesma largura.
	if rs[pLog].x != 0 || rs[pChat].y <= rs[pDiff].y || rs[pChat].w != rs[pDiff].w || rs[pChat].x+rs[pChat].w != 150 {
		t.Fatalf("posições: %v", rs)
	}
	if rs[pDiff].w < 100 {
		t.Fatalf("o Visualizador deveria ser largo: %v", rs[pDiff])
	}
}

func TestVizinhoPorDirecao(t *testing.T) {
	rs := defaultLayout().compute(0, 0, 150, 40, pLog, false)
	cases := []struct {
		from   paneID
		dx, dy int
		want   paneID
	}{
		{pLog, 1, 0, pDiff}, {pDiff, 0, 1, pChat}, {pChat, 0, -1, pDiff},
		{pChat, -1, 0, pLog}, {pDiff, -1, 0, pLog},
	}
	for _, c := range cases {
		if got, _ := neighbor(rs, c.from, c.dx, c.dy); got != c.want {
			t.Errorf("de %s (%d,%d) foi para %s, queria %s", paneNames[c.from], c.dx, c.dy, paneNames[got], paneNames[c.want])
		}
	}
	if _, ok := neighbor(rs, pLog, -1, 0); ok {
		t.Error("à esquerda do Navegador não há nada")
	}
}

func TestMoverEmpilharMinimizar(t *testing.T) {
	l := defaultLayout()
	// O chat vai para a coluna do Navegador.
	if !l.moveH(pChat, -1) {
		t.Fatal("moveH falhou")
	}
	if ci, _ := l.find(pChat); ci != 0 || len(l.Cols[1].Panes) != 1 || !l.valid() {
		t.Fatalf("layout = %+v", l.Cols)
	}
	// O Visualizador sozinho na borda direita: não sai.
	if l.moveH(pDiff, 1) {
		t.Fatal("coluna sozinha na borda não deveria mover")
	}
	// Painel que divide a coluna, na borda, vira coluna nova.
	l.moveH(pLog, -1)
	if len(l.Cols) != 3 || l.Cols[0].Panes[0] != pLog {
		t.Fatalf("coluna nova na borda: %+v", l.Cols)
	}
	// Empilhar: os não ativos ocupam uma linha.
	l = defaultLayout()
	l.toggleStack(pChat)
	rs := l.compute(0, 0, 150, 40, pChat, false)
	if rs[pDiff].h != 1 || rs[pChat].h != 39 {
		t.Fatalf("pilha: diff=%v chat=%v", rs[pDiff], rs[pChat])
	}
	// Minimizar some do layout; o último painel não minimiza.
	l = defaultLayout()
	l.minimize(pDiff)
	if _, ok := l.compute(0, 0, 150, 40, pLog, false)[pDiff]; ok {
		t.Fatal("minimizado ainda aparece")
	}
	l.minimize(pLog)
	if l.minimize(pChat) {
		t.Fatal("o último painel visível não pode minimizar")
	}
	l.restore(pDiff)
	if l.isMinimized(pDiff) {
		t.Fatal("restore falhou")
	}
}

func TestLayoutAntigoViraOPadrao(t *testing.T) {
	// layout.json da versão com 4 painéis (Commits, Arquivos, Diff, Chat).
	old := layout{Cols: []column{
		{Panes: []paneID{0, 1}, Sizes: []float64{0.5, 0.5}, Width: 0.3},
		{Panes: []paneID{2}, Sizes: []float64{1}, Width: 0.4},
		{Panes: []paneID{3}, Sizes: []float64{1}, Width: 0.3},
	}}
	if old.valid() {
		t.Fatal("o layout antigo deveria ser recusado")
	}
}

func TestResize(t *testing.T) {
	l := defaultLayout()
	w := l.Cols[0].Width
	l.resizeH(pLog, 0.05)
	if l.Cols[0].Width <= w {
		t.Fatal("coluna não cresceu")
	}
	for i := 0; i < 50; i++ {
		l.resizeH(pLog, -0.05)
	}
	if l.Cols[0].Width < 0.079 {
		t.Fatalf("passou do mínimo: %v", l.Cols[0].Width)
	}
}

func TestSplitInts(t *testing.T) {
	got := splitInts(100, []float64{0.26, 0.38, 0.36}, 16)
	if got[0]+got[1]+got[2] != 100 {
		t.Fatalf("soma = %v", got)
	}
	got = splitInts(40, []float64{0.9, 0.1}, 4)
	if got[1] < 4 || got[0]+got[1] != 40 {
		t.Fatalf("mínimo = %v", got)
	}
}

func TestOverlayPreservaAsLaterais(t *testing.T) {
	screen := []string{"0123456789", "abcdefghij", "ABCDEFGHIJ"}
	out := overlay(screen, "XX\nYY", 10)
	if out[0] != "0123XX6789" || out[1] != "abcdYYghij" || out[2] != "ABCDEFGHIJ" {
		t.Fatalf("overlay = %q", out)
	}
}
