package main

import (
	"strings"
	"testing"
	"time"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/repo"
	"github.com/charmbracelet/x/ansi"
)

const amostraDiff = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -10,4 +10,5 @@ func Send(ctx context.Context) error {
 	start := len(c.messages)
-	emit("parei após 40 rodadas")
+	emit("parei: orçamento esgotado")
+	guard.add(u)
 	return nil
@@ -40,2 +41,2 @@ func outra() {
-	x := 1
+	y := 2
 	fim()
`

func TestParseDiff(t *testing.T) {
	d := parseDiff(amostraDiff)
	if len(d.hunks) != 2 || d.hunks[0].header != "func Send(ctx context.Context) error {" || d.hunks[1].newStart != 41 {
		t.Fatalf("trechos = %+v", d.hunks)
	}
	ls := d.hunks[0].lines
	if ls[0].old != 10 || ls[0].new != 10 || ls[1].kind != dlDel || ls[1].old != 11 || ls[2].new != 11 || ls[3].new != 12 || ls[4].old != 12 || ls[4].new != 13 {
		t.Fatalf("numeração errada: %+v", ls)
	}
	// Destaque palavra a palavra: só o texto entre as aspas mudou.
	del, add := ls[1], ls[2]
	if got := string([]rune(del.text)[del.hlFrom:del.hlTo]); got != `parei após 40 rodadas` {
		t.Fatalf("destaque removido = %q", got)
	}
	if got := string([]rune(add.text)[add.hlFrom:add.hlTo]); got != `parei: orçamento esgotado` {
		t.Fatalf("destaque adicionado = %q", got)
	}
	if ls[3].hlFrom != -1 {
		t.Fatal("linha adicionada sem par não deve ter destaque")
	}
}

func TestRenderUnifiedEDobras(t *testing.T) {
	lines, starts := renderUnified(parseDiff(amostraDiff), 90)
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
		if w := len([]rune(plain[i])); w > 90 {
			t.Fatalf("linha %d passa da largura (%d): %q", i, w, plain[i])
		}
	}
	if !strings.Contains(plain[0], "9 linhas antes") {
		t.Fatalf("primeira linha deveria dobrar as 9 linhas antes: %q", plain[0])
	}
	if len(starts) != 2 || !strings.Contains(plain[starts[0]], "func Send") || !strings.Contains(plain[starts[0]], "linha 10") {
		t.Fatalf("faixa do trecho: %v %q", starts, plain[starts[0]])
	}
	// Entre os trechos: 14..40 do antigo, 26 linhas iguais.
	if !strings.Contains(plain[starts[1]-1], "26 linhas iguais") {
		t.Fatalf("dobra entre trechos: %q", plain[starts[1]-1])
	}
	if !strings.Contains(plain[starts[0]+2], "  11      − ") {
		t.Fatalf("numeração e sinal da linha removida: %q", plain[starts[0]+2])
	}
}

func TestRenderSplit(t *testing.T) {
	lines, starts := renderSplit(parseDiff(amostraDiff), 101)
	if !strings.Contains(ansi.Strip(lines[0]), "ANTES") || !strings.Contains(ansi.Strip(lines[0]), "DEPOIS") || len(starts) != 2 {
		t.Fatalf("cabeçalho/trechos: %q %v", ansi.Strip(lines[0]), starts)
	}
	// "guard.add(u)" só existe depois: o lado de antes fica preenchido com ╱.
	found := false
	for _, l := range lines {
		p := ansi.Strip(l)
		if strings.Contains(p, "guard.add(u)") {
			found = true
			left, _, _ := strings.Cut(p, "│")
			if strings.Trim(left, "╱") != "" {
				t.Fatalf("lado de antes deveria ser ╱: %q", left)
			}
		}
	}
	if !found {
		t.Fatal("linha adicionada não apareceu")
	}
}

func TestDatasRelativas(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, time.Local)
	cases := map[time.Time]string{
		now.Add(-30 * time.Second):                      "agora",
		now.Add(-5 * time.Minute):                       "há 5 min",
		now.Add(-3 * time.Hour):                         "há 3 h",
		time.Date(2026, 11, 17, 9, 0, 0, 0, time.Local): "ontem",
		now.AddDate(0, 0, -4):                           "há 4 dias",
		time.Date(2026, 3, 2, 9, 0, 0, 0, time.Local):   "2 mar",
		time.Date(2024, 3, 2, 9, 0, 0, 0, time.Local):   "2 mar 2024",
	}
	for t0, want := range cases {
		if got := relTime(t0.Unix(), now); got != want {
			t.Errorf("relTime(%v) = %q, queria %q", t0, got, want)
		}
	}
	if dateGroup(now.Add(-time.Hour).Unix(), now) != "HOJE" || dateGroup(now.AddDate(0, 0, -20).Unix(), now) != "ESTE MÊS" {
		t.Error("grupos de data")
	}
}

func TestGrafoEBarra(t *testing.T) {
	if got := graphCont("* | "); got != "│ │ " {
		t.Fatalf("graphCont = %q", got)
	}
	if got := ansi.Strip(changeBar(30, 10, 8)); got != "████████" {
		t.Fatalf("changeBar = %q", got)
	}
	if changeBar(0, 0, 8) != "" {
		t.Fatal("sem mudanças, sem barra")
	}
}

func TestArvoreDeArquivos(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	fs := []repo.FileChange{
		{Path: "z.md", Status: "M", Add: 1},
		{Path: "internal/llm/openai.go", Status: "M", Add: 12, Del: 2},
		{Path: "internal/llm/guard.go", Status: "A", Add: 43},
		{Path: "cmd/novo.go", OldPath: "cmd/velho.go", Status: "R", Add: 3, Del: 3},
	}
	sortFiles(fs)
	m.files = fs
	var got []string
	for _, l := range m.fileLines(70) {
		got = append(got, strings.TrimRight(ansi.Strip(l.text), " "))
	}
	want := []string{
		"z.md", "▾ cmd/", "velho.go → novo.go", "▾ internal/llm/", "guard.go", "openai.go",
	}
	if len(got) != len(want) {
		t.Fatalf("linhas = %q", got)
	}
	for i := range want {
		if !strings.Contains(got[i], want[i]) {
			t.Errorf("linha %d = %q, queria conter %q", i, got[i], want[i])
		}
	}
	if !strings.Contains(got[4], "+ novo") || !strings.Contains(got[2], "→ renom.") {
		t.Errorf("situação por extenso: %q", got)
	}
}

func TestAbasDoVisualizadorERevisados(t *testing.T) {
	t.Setenv("DANTE_CONFIG", t.TempDir())
	t.Setenv("DANTE_HOME", t.TempDir())
	m := newModel(config.Defaults(), "", "")
	m.preview.Width, m.preview.Height = 80, 20
	m.setDiff("a.go", amostraDiff)
	if m.vtab != vtDiff || len(m.hunkStarts) != 2 {
		t.Fatal("diff deveria abrir na aba Diff com 2 trechos")
	}
	m.setDoc("b.go", "    1  package b\n", false)
	if m.vtab != vtDoc || !strings.Contains(ansi.Strip(m.tabsLabel()), "Documento") {
		t.Fatal("documento do modelo deveria abrir na aba Documento")
	}
	nm, _ := m.updateDiffKeys("[")
	if nm.(model).vtab != vtDiff {
		t.Fatal("[ deveria voltar ao diff")
	}
	root := t.TempDir()
	set := loadReviewed(root)
	set["abc"] = true
	saveReviewed(root, set)
	if !loadReviewed(root)["abc"] {
		t.Fatal("revisados não persistiram")
	}
}
