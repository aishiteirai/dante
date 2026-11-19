package repo

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Contexto de uma função, montado sem o modelo: o corpo, onde ela é usada e o que ela
// chama. Serve para "o que faz esta função?" numa requisição só, sem o modelo precisar
// explorar o repositório.

// Declarações de função (o grupo 1 é o nome), da mais comum para a menos comum.
var funcDecls = []*regexp.Regexp{
	declName, // Go, Python, JS function, Rust, Kotlin, Ruby (def), classes
	regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>`),
	regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|static|final|abstract|override|virtual|async|synchronized)\s+)+[\w<>\[\],.? ]*?\b([A-Za-z_]\w*)\s*\([^;]*$`),
	regexp.MustCompile(`^\s{2,}(?:async\s+)?([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*\{\s*$`), // método de classe em JS/TS
}

var notFuncs = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true,
	"func": true, "function": true, "else": true, "new": true, "typeof": true}

// declOf devolve o nome da função declarada na linha ("" se não for uma declaração).
func declOf(line string) string {
	for _, re := range funcDecls {
		if m := re.FindStringSubmatch(line); m != nil && !notFuncs[m[1]] {
			return m[1]
		}
	}
	return ""
}

// Usage é um lugar onde a função aparece.
type Usage struct {
	Path string
	Line int
	Text string
}

type FuncInfo struct {
	Name       string
	Path       string
	Start, End int    // linhas (1 = primeira)
	Code       string // corpo numerado
	Usages     []Usage
	MoreUsages int     // usos além dos listados
	Calls      []Usage // funções do projeto que ela chama: onde estão declaradas
	Whole      bool    // não achei uma função: é um trecho ao redor da linha
}

const (
	maxFuncLines = 250
	maxUsages    = 8
	maxCallees   = 8
)

// content lê o arquivo na versão do alvo (WorkTree = disco).
func (r Repo) content(sha, p string) ([]string, error) {
	clean, err := r.safePath(p)
	if err != nil {
		return nil, err
	}
	if r.Blocked(clean) || isGitDir(clean) {
		return nil, blockedErr(clean)
	}
	var data string
	if sha == WorkTree {
		b, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(clean)))
		if err != nil {
			return nil, fmt.Errorf("arquivo não encontrado: %s", clean)
		}
		data = string(b)
	} else {
		out, err := r.git("show", sha+":"+clean)
		if err != nil {
			return nil, err
		}
		data = out
	}
	data, _ = Redact(data)
	return strings.Split(strings.TrimSuffix(data, "\n"), "\n"), nil
}

// FuncAt acha a função que contém a linha e monta o contexto dela.
func (r Repo) FuncAt(sha, p string, line int) (FuncInfo, error) {
	lines, err := r.content(sha, p)
	if err != nil {
		return FuncInfo{}, err
	}
	line = min(max(line, 1), len(lines))
	fi := FuncInfo{Path: p}
	// Sobe até a declaração mais próxima.
	start := -1
	for i := line - 1; i >= 0 && i >= line-1-400; i-- {
		if name := declOf(lines[i]); name != "" {
			start, fi.Name = i, name
			break
		}
	}
	end := -1
	if start >= 0 {
		end = blockEnd(lines, start, path.Ext(p))
		if end < line-1 { // a linha está depois do fim dessa função: não está dentro dela
			start = -1
		}
	}
	if start < 0 { // fora de qualquer função: um trecho ao redor
		fi.Name, fi.Whole = "", true
		start, end = max(line-21, 0), min(line+19, len(lines)-1)
	}
	end = min(end, start+maxFuncLines-1)
	// O comentário logo acima da declaração costuma dizer a intenção: entra junto.
	if !fi.Whole {
		for i := 0; i < 15 && start > 0 && isComment(lines[start-1]); i++ {
			start--
		}
	}
	fi.Start, fi.End = start+1, end+1
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%5d  %s\n", i+1, lines[i])
	}
	fi.Code = b.String()
	if fi.Name != "" {
		fi.Usages, fi.MoreUsages = r.usages(sha, fi.Name, p, fi.Start, fi.End)
		fi.Calls = r.callees(sha, strings.Join(lines[start:end+1], "\n"), fi.Name, p)
	}
	return fi, nil
}

// blockEnd acha a última linha do bloco que começa em start: pelas chaves, ou pela
// indentação em Python; sem nenhum dos dois, até a próxima declaração.
func blockEnd(lines []string, start int, ext string) int {
	if ext == ".py" || ext == ".rb" {
		ind := indentOf(lines[start])
		last := start
		for i := start + 1; i < len(lines); i++ {
			t := strings.TrimSpace(lines[i])
			if t == "" {
				continue
			}
			if indentOf(lines[i]) <= ind && !(ext == ".rb" && t == "end") {
				break
			}
			last = i
		}
		return last
	}
	depth, opened := 0, false
	for i := start; i < len(lines) && i < start+2000; i++ {
		for _, c := range stripStrings(lines[i]) {
			switch c {
			case '{':
				depth++
				opened = true
			case '}':
				depth--
			}
		}
		if opened && depth <= 0 {
			return i
		}
		if !opened && i > start && strings.HasSuffix(strings.TrimSpace(lines[i-1]), ";") {
			return i - 1 // declaração sem corpo
		}
	}
	for i := start + 1; i < len(lines); i++ {
		if declOf(lines[i]) != "" {
			return i - 1
		}
	}
	return len(lines) - 1
}

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }

var strLit = regexp.MustCompile("\"(?:\\\\.|[^\"\\\\])*\"|'(?:\\\\.|[^'\\\\])*'|`[^`]*`|//.*$|#.*$")

// stripStrings tira strings e comentários de linha, para contar chaves.
func stripStrings(s string) string { return strLit.ReplaceAllString(s, "") }

// usages lista onde a função é chamada (fora dela mesma), com a linha de código.
func (r Repo) usages(sha, name, self string, from, to int) ([]Usage, int) {
	out, _ := r.git(grepArgs(sha, `(^|[^A-Za-z0-9_])`+regexp.QuoteMeta(name)+`\(`)...)
	var code, tests []Usage
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if sha != WorkTree {
			l = strings.TrimPrefix(l, sha+":")
		}
		parts := strings.SplitN(l, ":", 3)
		if len(parts) < 3 || r.Blocked(parts[0]) {
			continue
		}
		var n int
		fmt.Sscan(parts[1], &n)
		if parts[0] == self && n >= from && n <= to {
			continue // dentro da própria função (recursão, a declaração)
		}
		if declOf(parts[2]) == name {
			continue // outra declaração com o mesmo nome
		}
		u := Usage{Path: parts[0], Line: n, Text: shorten(strings.TrimSpace(parts[2]), 140)}
		if Kind(parts[0]) == KindTest {
			tests = append(tests, u)
		} else {
			code = append(code, u)
		}
	}
	all := append(code, tests...)
	more := max(len(all)-maxUsages, 0)
	return all[:min(len(all), maxUsages)], more
}

var callRe = regexp.MustCompile(`(\.)?\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

var goMethod = regexp.MustCompile(`^\s*func\s+\(`)

// Nomes comuns que não vale procurar (palavras da linguagem e funções embutidas).
var builtins = map[string]bool{"make": true, "len": true, "cap": true, "append": true, "new": true, "panic": true,
	"print": true, "println": true, "min": true, "max": true, "copy": true, "delete": true, "close": true, "string": true,
	"int": true, "int64": true, "float64": true, "byte": true, "rune": true, "bool": true, "error": true, "range": true,
	"str": true, "list": true, "dict": true, "set": true, "isinstance": true, "super": true, "require": true}

// callees acha as funções do projeto que o corpo chama e onde estão declaradas. Uma
// chamada sem prefixo (emit(…), e não b.emit(…)) é de uma função da mesma pasta, de uma
// função comum de outra pasta (nunca um método) ou de um parâmetro, que não se procura.
func (r Repo) callees(sha, body, self, file string) []Usage {
	seen := map[string]bool{self: true}
	type call struct {
		name      string
		qualified bool
	}
	var calls []call
	for _, m := range callRe.FindAllStringSubmatch(stripStrings(body), -1) {
		n := m[2]
		if seen[n] || notFuncs[n] || builtins[n] || len(n) < 3 {
			continue
		}
		seen[n] = true
		calls = append(calls, call{n, m[1] == "."})
	}
	dir := path.Dir(file)
	var out []Usage
	for _, c := range calls {
		if len(out) >= maxCallees {
			break
		}
		res, _ := r.git(grepArgs(sha, declGrep(c.name, false))...)
		var best *Usage
		for _, l := range strings.Split(strings.TrimSpace(res), "\n") {
			if sha != WorkTree {
				l = strings.TrimPrefix(l, sha+":")
			}
			parts := strings.SplitN(l, ":", 3)
			if len(parts) < 3 || r.Blocked(parts[0]) || declOf(parts[2]) != c.name {
				continue
			}
			same := path.Dir(parts[0]) == dir
			if !c.qualified && !same && goMethod.MatchString(parts[2]) {
				continue // método de outro tipo: a chamada sem prefixo não é ele
			}
			var ln int
			fmt.Sscan(parts[1], &ln)
			u := Usage{Path: parts[0], Line: ln, Text: shorten(strings.TrimSpace(parts[2]), 140)}
			if best == nil || (same && path.Dir(best.Path) != dir) {
				best = &u
			}
		}
		if best != nil {
			out = append(out, *best)
		}
	}
	return out
}

// declGrep é o padrão (POSIX ERE) que acha a declaração de uma função pelo nome. Só POSIX:
// o git grep -E do macOS usa o regex do sistema, sem \s nem \b (extensões do GNU).
func declGrep(name string, withClass bool) string {
	sp, end := `[[:space:]]`, `([^A-Za-z0-9_]|$)`
	n := regexp.QuoteMeta(name)
	kinds := `func` + sp + `+(\([^)]*\)` + sp + `*)?|def` + sp + `+|function` + sp + `+|fn` + sp + `+|fun` + sp + `+`
	if withClass {
		kinds += `|class` + sp + `+`
	}
	return `(` + kinds + `)` + n + end + `|(const|let|var)` + sp + `+` + n + sp + `*=`
}

// grepArgs monta um git grep no alvo: num commit, procura nele; no disco, inclui os
// arquivos novos ainda não versionados.
func grepArgs(sha, pattern string, opts ...string) []string {
	args := append([]string{"grep", "-n", "-I", "-E"}, opts...)
	if sha == WorkTree {
		args = append(args, "--untracked")
	}
	args = append(args, "-e", pattern)
	if sha != WorkTree {
		args = append(args, sha)
	}
	return args
}

// FindFunc acha onde uma função está declarada (arquivo e linha), para quando o usuário
// cola o código ou só o nome. Prefere código a testes.
func (r Repo) FindFunc(sha, name string) (string, int, bool) {
	out, _ := r.git(grepArgs(sha, declGrep(name, true))...)
	var best Usage
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if sha != WorkTree {
			l = strings.TrimPrefix(l, sha+":")
		}
		parts := strings.SplitN(l, ":", 3)
		if len(parts) < 3 || r.Blocked(parts[0]) || declOf(parts[2]) != name {
			continue
		}
		var n int
		fmt.Sscan(parts[1], &n)
		if best.Path == "" || (Kind(best.Path) == KindTest && Kind(parts[0]) != KindTest) {
			best = Usage{Path: parts[0], Line: n}
		}
	}
	return best.Path, best.Line, best.Path != ""
}

// DeclaredName devolve o nome da primeira função declarada num trecho colado.
func DeclaredName(code string) string {
	for _, l := range strings.Split(code, "\n") {
		if n := declOf(l); n != "" {
			return n
		}
	}
	return ""
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// UsagesOf lista onde uma função é chamada, pelo nome (para código colado).
func (r Repo) UsagesOf(sha, name string) ([]Usage, int) { return r.usages(sha, name, "", 0, 0) }

func isComment(l string) bool {
	t := strings.TrimSpace(l)
	for _, p := range []string{"//", "#", "/*", "*", "--", ";;", "@"} { // @: anotações e decoradores
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// FileContent devolve as linhas do arquivo na versão do alvo (WorkTree = disco), com as
// mesmas proteções das ferramentas: nada de arquivos sensíveis, e segredos mascarados.
func (r Repo) FileContent(sha, p string) ([]string, error) { return r.content(sha, p) }

// FileOutline devolve o esqueleto do arquivo (declarações com o número da linha).
func FileOutline(p string, lines []string) string { return outline(p, lines) }

// FileUsers lista onde outros arquivos citam este (pelo nome sem extensão, ou pela pasta
// num import), para dizer quem usa um arquivo.
func (r Repo) FileUsers(sha, p string, n int) []Usage {
	name := strings.TrimSuffix(path.Base(p), path.Ext(p))
	if len(name) < 3 {
		return nil
	}
	pat := `(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_]|$)`
	if d := path.Dir(p); d != "." && (path.Ext(p) == ".go") {
		pat = `"[^"]*` + regexp.QuoteMeta(d) + `"` // Go importa a pasta, não o arquivo
	}
	out, _ := r.git(grepArgs(sha, pat)...)
	var us []Usage
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if sha != WorkTree {
			l = strings.TrimPrefix(l, sha+":")
		}
		parts := strings.SplitN(l, ":", 3)
		if len(parts) < 3 || parts[0] == p || r.Blocked(parts[0]) || Kind(parts[0]) == KindDoc {
			continue
		}
		var ln int
		fmt.Sscan(parts[1], &ln)
		us = append(us, Usage{Path: parts[0], Line: ln, Text: shorten(strings.TrimSpace(parts[2]), 120)})
		if len(us) >= n {
			break
		}
	}
	return us
}

// Hit é um arquivo encontrado pela busca de Locate, com as linhas que bateram.
type Hit struct {
	Path  string
	Score float64
	Lines []Usage
}

// Locate procura os termos no código (sem diferenciar maiúsculas) e devolve os arquivos
// mais relevantes: termos raros valem mais, o nome do arquivo conta, testes e docs valem
// menos e gerados ficam de fora. Serve para "onde mexo para fazer X?" sem o modelo explorar.
func (r Repo) Locate(terms []string, n int) []Hit {
	type acc struct {
		score float64
		lines []Usage
		seen  map[int]bool
	}
	files := map[string]*acc{}
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if len(t) < 3 {
			continue
		}
		out, _ := r.git(grepArgs(WorkTree, regexp.QuoteMeta(t), "-i")...)
		perFile := map[string][]Usage{}
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			parts := strings.SplitN(l, ":", 3)
			if len(parts) < 3 || r.Blocked(parts[0]) || Kind(parts[0]) == KindGenerated {
				continue
			}
			var ln int
			fmt.Sscan(parts[1], &ln)
			perFile[parts[0]] = append(perFile[parts[0]], Usage{Path: parts[0], Line: ln, Text: shorten(strings.TrimSpace(parts[2]), 120)})
		}
		if len(perFile) == 0 || len(perFile) > 80 { // comum demais para ajudar
			continue
		}
		weight := 1.0 / (1.0 + float64(len(perFile))/5)
		for p, us := range perFile {
			a := files[p]
			if a == nil {
				a = &acc{seen: map[int]bool{}}
				files[p] = a
			}
			s := weight * float64(min(len(us), 3))
			if strings.Contains(strings.ToLower(p), strings.ToLower(t)) {
				s += weight
			}
			switch Kind(p) {
			case KindTest:
				s *= 0.5
			case KindDoc:
				s *= 0.3
			}
			a.score += s
			// Linhas de declaração primeiro: dizem mais que um uso qualquer.
			sort.SliceStable(us, func(i, j int) bool { return declOf(us[i].Text) != "" && declOf(us[j].Text) == "" })
			for _, u := range us[:min(len(us), 2)] {
				if !a.seen[u.Line] {
					a.seen[u.Line] = true
					a.lines = append(a.lines, u)
				}
			}
		}
	}
	var hits []Hit
	for p, a := range files {
		sort.Slice(a.lines, func(i, j int) bool { return a.lines[i].Line < a.lines[j].Line })
		hits = append(hits, Hit{Path: p, Score: a.score, Lines: a.lines[:min(len(a.lines), 5)]})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})
	return hits[:min(len(hits), n)]
}
