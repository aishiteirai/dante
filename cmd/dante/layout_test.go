package main

import "testing"

func TestComputeCobreATela(t *testing.T) {
	l := defaultLayout()
	rs := l.compute(0, 1, 150, 40, pLog, false)
	if len(rs) != 4 {
		t.Fatalf("rects = %v", rs)
	}
	area := 0
	for _, r := range rs {
		area += r.w * r.h
	}
	if area != 150*40 {
		t.Fatalf("área %d, esperava %d", area, 150*40)
	}
	if rs[pLog].x != 0 || rs[pFiles].y <= rs[pLog].y || rs[pChat].x+rs[pChat].w != 150 {
		t.Fatalf("posições: %v", rs)
	}
}

func TestVizinhoPorDirecao(t *testing.T) {
	rs := defaultLayout().compute(0, 0, 150, 40, pLog, false)
	cases := []struct {
		from   paneID
		dx, dy int
		want   paneID
	}{
		{pLog, 1, 0, pDiff}, {pFiles, 0, -1, pLog}, {pLog, 0, 1, pFiles},
		{pDiff, 1, 0, pChat}, {pChat, -1, 0, pDiff}, {pFiles, 1, 0, pDiff},
	}
	for _, c := range cases {
		if got, _ := neighbor(rs, c.from, c.dx, c.dy); got != c.want {
			t.Errorf("de %s (%d,%d) foi para %s, queria %s", paneNames[c.from], c.dx, c.dy, paneNames[got], paneNames[c.want])
		}
	}
	if _, ok := neighbor(rs, pLog, -1, 0); ok {
		t.Error("à esquerda do Commits não há nada")
	}
}

func TestMoverEmpilharMinimizar(t *testing.T) {
	l := defaultLayout()
	// Arquivos vai para a coluna do Diff, na mesma posição (embaixo dele, pois o Diff está sozinho).
	if !l.moveH(pFiles, 1) {
		t.Fatal("moveH falhou")
	}
	if ci, _ := l.find(pFiles); ci != 1 || len(l.Cols[0].Panes) != 1 || !l.valid() {
		t.Fatalf("layout = %+v", l.Cols)
	}
	// Chat sozinho na borda direita: não sai.
	if l.moveH(pChat, 1) {
		t.Fatal("coluna sozinha na borda não deveria mover")
	}
	// Commits sozinho, levado à direita, entra na coluna do meio.
	l.moveH(pLog, 1)
	if len(l.Cols) != 2 || len(l.Cols[0].Panes) != 3 {
		t.Fatalf("esperava 2 colunas: %+v", l.Cols)
	}
	// Painel que divide a coluna, na borda, vira coluna nova.
	l.moveH(pLog, -1)
	if len(l.Cols) != 3 || l.Cols[0].Panes[0] != pLog {
		t.Fatalf("coluna nova na borda: %+v", l.Cols)
	}
	// Empilhar: os não ativos ocupam uma linha.
	l = defaultLayout()
	l.toggleStack(pFiles)
	rs := l.compute(0, 0, 150, 40, pFiles, false)
	if rs[pLog].h != 1 || rs[pFiles].h != 39 {
		t.Fatalf("pilha: log=%v files=%v", rs[pLog], rs[pFiles])
	}
	// Minimizar some do layout; o último painel não minimiza.
	l = defaultLayout()
	l.minimize(pDiff)
	if _, ok := l.compute(0, 0, 150, 40, pLog, false)[pDiff]; ok {
		t.Fatal("minimizado ainda aparece")
	}
	l.minimize(pLog)
	l.minimize(pFiles)
	if l.minimize(pChat) {
		t.Fatal("o último painel visível não pode minimizar")
	}
	l.restore(pDiff)
	if l.isMinimized(pDiff) {
		t.Fatal("restore falhou")
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
