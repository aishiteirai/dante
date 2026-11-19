package repo

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestFuncAt(t *testing.T) {
	r := setup(t)
	write := func(p, c string) {
		os.MkdirAll(filepath.Join(r.Root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(r.Root, p), []byte(c), 0o644)
	}
	write("calc/calc.go", `package calc

// Somar soma dois números.
func Somar(a, b int) int {
	if a < 0 {
		return dobro(b) // "{" numa string não conta
	}
	return a + b
}

func dobro(x int) int { return 2 * x }
`)
	write("main.go", "package main\n\nfunc main() {\n\tprintln(calc.Somar(1, 2))\n}\n")
	write("calc/calc_test.go", "package calc\n\nfunc TestX() { Somar(0, 0) }\n")
	write("app.py", "def total(itens):\n    soma = 0\n    for i in itens:\n        soma += i\n    return soma\n\nprint(total([1]))\n")
	write("web/a.js", "export const formatar = (v) => {\n  return v.toFixed(2)\n}\nformatar(3)\n")

	fi, err := r.FuncAt(WorkTree, "calc/calc.go", 6)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Name != "Somar" || fi.Start != 3 || fi.End != 9 || fi.Whole || !strings.Contains(fi.Code, "Somar soma dois números") {
		t.Fatalf("função = %s %d-%d", fi.Name, fi.Start, fi.End)
	}
	if len(fi.Usages) != 2 || fi.Usages[0].Path != "main.go" || fi.Usages[1].Path != "calc/calc_test.go" ||
		!strings.Contains(fi.Usages[0].Text, "calc.Somar(1, 2)") {
		t.Fatalf("usos = %+v", fi.Usages)
	}
	if len(fi.Calls) != 1 || fi.Calls[0].Line != 11 || !strings.Contains(fi.Calls[0].Text, "func dobro") {
		t.Fatalf("chama = %+v", fi.Calls)
	}
	py, _ := r.FuncAt(WorkTree, "app.py", 4)
	if py.Name != "total" || py.Start != 1 || py.End != 5 || len(py.Usages) != 1 {
		t.Fatalf("python = %+v", py)
	}
	js, _ := r.FuncAt(WorkTree, "web/a.js", 2)
	if js.Name != "formatar" || js.End != 3 || len(js.Usages) != 1 {
		t.Fatalf("js = %+v", js)
	}
	// Fora de qualquer função: um trecho ao redor.
	out, _ := r.FuncAt(WorkTree, "app.py", 7)
	if !out.Whole || out.Name != "" {
		t.Fatalf("fora de função = %+v", out)
	}
	if p, l, ok := r.FindFunc(WorkTree, "Somar"); !ok || p != "calc/calc.go" || l != 4 {
		t.Fatalf("FindFunc = %s %d", p, l)
	}
	if DeclaredName("// colado\nfunc (m model) viewLog(w, h int) string {\n") != "viewLog" {
		t.Fatal("DeclaredName")
	}
}

func TestLocate(t *testing.T) {
	r := setup(t)
	write := func(p, c string) {
		os.MkdirAll(filepath.Join(r.Root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(r.Root, p), []byte(c), 0o644)
	}
	write("orders/list.go", "package orders\n\nfunc ListOrders(filter Filter) []Order {\n\treturn nil\n}\n")
	write("orders/filter.go", "package orders\n\ntype Filter struct{ Status string }\n")
	write("orders/list_test.go", "package orders\n\nfunc TestListOrders() { ListOrders(Filter{}) }\n")
	write("README.md", "Lista de pedidos (orders) com filtro.\n")
	hits := r.Locate([]string{"ListOrders", "filter", "pedidos"}, 5)
	if len(hits) < 3 || hits[0].Path != "orders/list.go" {
		t.Fatalf("hits = %+v", hits)
	}
	for _, h := range hits {
		if h.Path == "orders/list_test.go" && h.Score >= hits[0].Score {
			t.Fatal("teste não deveria valer mais que o código")
		}
	}
	if hits[0].Lines[0].Line != 3 { // a declaração vem primeiro
		t.Fatalf("linhas = %+v", hits[0].Lines)
	}
	if us := r.FileUsers(WorkTree, "orders/list.go", 5); len(us) != 0 {
		t.Fatalf("ninguém importa a pasta orders: %+v", us)
	}
}

// Os padrões do git grep -E precisam ser POSIX: no macOS não há \s, \b, \w nem \d.
func TestPadroesDoGrepSaoPOSIX(t *testing.T) {
	for _, p := range []string{declGrep("Somar", true), declGrep("x", false)} {
		for _, gnu := range []string{`\s`, `\b`, `\w`, `\d`} {
			if strings.Contains(p, gnu) {
				t.Errorf("padrão com %s (só GNU): %s", gnu, p)
			}
		}
	}
	re := regexp.MustCompile(declGrep("Somar", true))
	for _, l := range []string{"func Somar(a int)", "func (c *Calc) Somar(", "def Somar(x):", "const Somar = (a) =>", "class Somar:"} {
		if !re.MatchString(l) {
			t.Errorf("deveria achar %q", l)
		}
	}
	if re.MatchString("func SomarTudo(") {
		t.Error("não deveria achar SomarTudo")
	}
}
