// Package tech descobre as tecnologias de um projeto sem gastar tokens: lê os manifestos
// (go.mod, package.json, pyproject.toml, requirements.txt, Cargo.toml, Gemfile), conta as
// linguagens pelos arquivos do git e acha onde cada biblioteca é usada com git grep.
package tech

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Tech é uma tecnologia do projeto.
type Tech struct {
	Name    string   // nome amigável ("Bubble Tea") ou o pacote, se não estiver no catálogo
	Package string   // como aparece no manifesto ("github.com/charmbracelet/bubbletea")
	Kind    string   // linguagem, framework, interface, teste, banco, http, ia, ferramenta, biblioteca
	What    string   // uma frase sobre o que é (vazio fora do catálogo)
	Source  string   // manifesto de onde veio ("go.mod")
	Version string   // versão pedida, quando há
	Dev     bool     // só para desenvolvimento (devDependencies, grupos de teste)
	Files   []string // arquivos que a usam, os que mais usam primeiro (até 20)
	Uses    int      // quantos arquivos a usam
}

// Language é uma linguagem do projeto, pelos arquivos versionados.
type Language struct {
	Name  string
	Files int
}

// Report é o que o dante sabe das tecnologias do projeto.
type Report struct {
	Languages []Language
	Techs     []Tech // do catálogo primeiro (por uso), depois as outras
}

// Detect lê o projeto em root.
func Detect(root string) Report {
	files := gitFiles(root)
	var r Report
	r.Languages = languages(files)
	for _, mf := range files {
		dir, base := path.Split(mf)
		if strings.Contains(dir, "node_modules/") || strings.Contains(dir, "vendor/") || strings.Count(dir, "/") > 2 {
			continue // manifestos de dependências e de exemplos muito fundos ficam de fora
		}
		parse, ok := parsers[base]
		if !ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, mf))
		if err != nil {
			continue
		}
		for _, t := range parse(string(b)) {
			t.Source = mf
			r.Techs = append(r.Techs, t)
		}
	}
	r.Techs = dedupe(r.Techs)
	for i := range r.Techs {
		known(&r.Techs[i])
		r.Techs[i].Files, r.Techs[i].Uses = usages(root, r.Techs[i])
	}
	sort.SliceStable(r.Techs, func(i, j int) bool {
		a, b := r.Techs[i], r.Techs[j]
		if (a.What != "") != (b.What != "") {
			return a.What != ""
		}
		if a.Uses != b.Uses {
			return a.Uses > b.Uses
		}
		return a.Name < b.Name
	})
	return r
}

// Find acha uma tecnologia pelo nome ou pelo pacote, sem diferenciar maiúsculas.
func (r Report) Find(q string) (Tech, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return Tech{}, false
	}
	for _, exact := range []bool{true, false} {
		for _, t := range r.Techs {
			n, p := strings.ToLower(t.Name), strings.ToLower(t.Package)
			if exact && (n == q || p == q || path.Base(p) == q) {
				return t, true
			}
			if !exact && (strings.Contains(n, q) || strings.Contains(p, q)) {
				return t, true
			}
		}
	}
	return Tech{}, false
}

// ---- manifestos ----

var parsers = map[string]func(string) []Tech{
	"go.mod":           parseGoMod,
	"package.json":     parsePackageJSON,
	"requirements.txt": parseRequirements,
	"pyproject.toml":   parsePyproject,
	"Cargo.toml":       parseCargo,
	"Gemfile":          parseGemfile,
}

func parseGoMod(s string) []Tech {
	var out []Tech
	inBlock := false
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "go ") && !inBlock:
			out = append(out, Tech{Name: "Go", Package: "go", Kind: "linguagem", Version: strings.TrimSpace(l[3:])})
			continue
		case l == "require (":
			inBlock = true
			continue
		case l == ")":
			inBlock = false
			continue
		case strings.HasPrefix(l, "require "):
			l = strings.TrimPrefix(l, "require ")
		case !inBlock:
			continue
		}
		if strings.Contains(l, "// indirect") {
			continue // só as dependências que o projeto usa direto
		}
		f := strings.Fields(l)
		if len(f) >= 2 {
			out = append(out, Tech{Package: f[0], Version: f[1]})
		}
	}
	return out
}

func parsePackageJSON(s string) []Tech {
	var pj struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal([]byte(s), &pj) != nil {
		return nil
	}
	var out []Tech
	for _, dev := range []bool{false, true} {
		deps := pj.Dependencies
		if dev {
			deps = pj.DevDependencies
		}
		names := make([]string, 0, len(deps))
		for n := range deps {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if strings.HasPrefix(n, "@types/") {
				continue
			}
			out = append(out, Tech{Package: n, Version: deps[n], Dev: dev})
		}
	}
	return out
}

var pyName = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)(\[[^\]]*\])?\s*(.*)$`)

func pyReq(l string) (Tech, bool) {
	l = strings.TrimSpace(strings.Split(l, "#")[0])
	l = strings.Trim(l, `"', `)
	if l == "" || strings.HasPrefix(l, "-") {
		return Tech{}, false
	}
	m := pyName.FindStringSubmatch(l)
	if m == nil {
		return Tech{}, false
	}
	return Tech{Package: strings.ToLower(m[1]), Version: strings.TrimSpace(strings.Split(m[3], ";")[0])}, true
}

func parseRequirements(s string) []Tech {
	var out []Tech
	for _, l := range strings.Split(s, "\n") {
		if t, ok := pyReq(l); ok {
			out = append(out, t)
		}
	}
	return out
}

// parsePyproject lê as listas de dependências ([project] dependencies e os grupos opcionais)
// e as tabelas do Poetry, sem um leitor de TOML completo.
func parsePyproject(s string) []Tech {
	out := []Tech{{Name: "Python", Package: "python", Kind: "linguagem"}}
	section, inList, dev := "", false, false
	for _, raw := range strings.Split(s, "\n") {
		l := strings.TrimSpace(raw)
		if strings.HasPrefix(l, "[") && !inList {
			section = strings.Trim(l, "[] ")
			continue
		}
		if inList {
			for _, item := range strings.Split(l, ",") {
				if strings.Contains(item, "]") {
					item = item[:strings.Index(item, "]")]
					inList = false
				}
				if t, ok := pyReq(item); ok {
					t.Dev = dev
					out = append(out, t)
				}
			}
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch {
		case section == "project" && k == "dependencies", section == "project.optional-dependencies",
			section == "dependency-groups":
			dev = section != "project"
			if strings.HasPrefix(v, "[") {
				inList = !strings.Contains(v, "]")
				for _, item := range strings.Split(strings.Trim(v, "[]"), ",") {
					if t, ok := pyReq(item); ok {
						t.Dev = dev
						out = append(out, t)
					}
				}
			}
		case strings.HasPrefix(section, "tool.poetry") && strings.HasSuffix(section, "dependencies") && k != "python":
			out = append(out, Tech{Package: strings.ToLower(k), Version: strings.Trim(v, `"'`),
				Dev: strings.Contains(section, "dev") || strings.Contains(section, "group")})
		}
	}
	return out
}

func parseCargo(s string) []Tech {
	out := []Tech{{Name: "Rust", Package: "rust", Kind: "linguagem"}}
	section := ""
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") {
			section = strings.Trim(l, "[] ")
			continue
		}
		if section != "dependencies" && section != "dev-dependencies" {
			continue
		}
		if k, v, ok := strings.Cut(l, "="); ok && !strings.HasPrefix(l, "#") {
			out = append(out, Tech{Package: strings.TrimSpace(k), Version: strings.Trim(strings.TrimSpace(v), `"`),
				Dev: section == "dev-dependencies"})
		}
	}
	return out
}

var gemRe = regexp.MustCompile(`^\s*gem\s+["']([^"']+)["']`)

func parseGemfile(s string) []Tech {
	out := []Tech{{Name: "Ruby", Package: "ruby", Kind: "linguagem"}}
	for _, l := range strings.Split(s, "\n") {
		if m := gemRe.FindStringSubmatch(l); m != nil {
			out = append(out, Tech{Package: m[1]})
		}
	}
	return out
}

func dedupe(ts []Tech) []Tech {
	seen := map[string]int{}
	var out []Tech
	for _, t := range ts {
		key := strings.ToLower(t.Package)
		if i, ok := seen[key]; ok {
			if out[i].Dev && !t.Dev {
				out[i].Dev = false
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, t)
	}
	return out
}

// ---- uso no código ----

// ImportNames são os textos que aparecem no código de quem usa a tecnologia (vazio para
// linguagens).
func ImportNames(t Tech) []string { return importNames(t) }

// importNames são os textos que aparecem no código de quem usa a tecnologia.
func importNames(t Tech) []string {
	p := t.Package
	switch {
	case t.Kind == "linguagem":
		return nil
	case strings.Contains(p, ".") && strings.Contains(p, "/"): // módulo Go
		return []string{`"` + p}
	case strings.HasSuffix(t.Source, "package.json"):
		return []string{`'` + p + `'`, `"` + p + `"`, `'` + p + `/`, `"` + p + `/`}
	case strings.HasSuffix(t.Source, ".toml") && strings.HasSuffix(t.Source, "Cargo.toml"):
		return []string{strings.ReplaceAll(p, "-", "_") + "::"}
	case strings.HasSuffix(t.Source, "Gemfile"):
		return []string{`require "` + p, `require '` + p}
	default: // Python: o módulo costuma ser o nome com _ no lugar de -
		mod := strings.ReplaceAll(strings.ToLower(p), "-", "_")
		if alias, ok := pyModules[mod]; ok {
			mod = alias
		}
		return []string{"import " + mod, "from " + mod}
	}
}

// Pacotes do Python cujo módulo tem outro nome.
var pyModules = map[string]string{"pyyaml": "yaml", "beautifulsoup4": "bs4", "scikit_learn": "sklearn",
	"pillow": "PIL", "python_dotenv": "dotenv", "opencv_python": "cv2", "psycopg2_binary": "psycopg2"}

func usages(root string, t Tech) ([]string, int) {
	names := importNames(t)
	if len(names) == 0 {
		return nil, 0
	}
	args := []string{"-C", root, "grep", "--untracked", "-c", "-F"}
	for _, n := range names {
		args = append(args, "-e", n)
	}
	args = append(args, "--", ".", ":!*.lock", ":!go.sum", ":!go.mod", ":!package.json", ":!*lock.json", ":!*.toml",
		":!requirements*.txt", ":!Gemfile*")
	out, _ := exec.Command("git", args...).Output()
	type fc struct {
		f string
		n int
	}
	var fs []fc
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		i := strings.LastIndex(l, ":")
		if i <= 0 {
			continue
		}
		n := 0
		for _, c := range l[i+1:] {
			n = n*10 + int(c-'0')
		}
		fs = append(fs, fc{l[:i], n})
	}
	// Os que mais usam primeiro; testes e exemplos depois, porque ensinam menos sobre o uso real.
	sort.SliceStable(fs, func(i, j int) bool {
		if ti, tj := isTest(fs[i].f), isTest(fs[j].f); ti != tj {
			return tj
		}
		return fs[i].n > fs[j].n
	})
	var files []string
	for i := 0; i < len(fs) && i < 20; i++ {
		files = append(files, fs[i].f)
	}
	return files, len(fs)
}

// ---- linguagens ----

var extLang = map[string]string{
	".go": "Go", ".py": "Python", ".ts": "TypeScript", ".tsx": "TypeScript", ".js": "JavaScript", ".jsx": "JavaScript",
	".mjs": "JavaScript", ".rs": "Rust", ".rb": "Ruby", ".java": "Java", ".kt": "Kotlin", ".swift": "Swift",
	".c": "C", ".h": "C", ".cpp": "C++", ".cc": "C++", ".hpp": "C++", ".cs": "C#", ".php": "PHP", ".scala": "Scala",
	".ex": "Elixir", ".exs": "Elixir", ".lua": "Lua", ".dart": "Dart", ".vue": "Vue", ".svelte": "Svelte",
	".sh": "Shell", ".bash": "Shell", ".ps1": "PowerShell", ".sql": "SQL", ".css": "CSS", ".scss": "CSS",
	".html": "HTML", ".zig": "Zig", ".hs": "Haskell", ".clj": "Clojure", ".r": "R", ".jl": "Julia",
}

func languages(files []string) []Language {
	count := map[string]int{}
	for _, f := range files {
		if l, ok := extLang[strings.ToLower(path.Ext(f))]; ok {
			count[l]++
		}
	}
	var out []Language
	for n, c := range count {
		out = append(out, Language{n, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func gitFiles(root string) []string {
	cmd := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		files = append(files, sc.Text())
	}
	return files
}

func isTest(f string) bool {
	b := strings.ToLower(path.Base(f))
	return strings.HasSuffix(b, "_test.go") || strings.HasPrefix(b, "test_") || strings.Contains(b, ".test.") ||
		strings.Contains(b, ".spec.") || strings.Contains(f, "/test/") || strings.Contains(f, "/tests/") ||
		strings.HasPrefix(f, "test/") || strings.HasPrefix(f, "tests/") || strings.Contains(f, "example")
}
