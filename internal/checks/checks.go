// Package checks roda as ferramentas fixas que o projeto já usa (Qlty, Biome, Oxlint,
// ESLint, ruff, go vet, golangci-lint, tsc, clippy) nos arquivos alterados e devolve só os
// problemas que a mudança introduziu. O dante não tem linter próprio: usa os do projeto.
// Sem dependência da interface.
package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Issue é um problema apontado por uma ferramenta.
type Issue struct {
	Tool, Path string
	Line       int
	Rule       string
	Message    string
	Error      bool // erro (senão, aviso)
}

// Tool é uma ferramenta detectada no projeto e o comando que a roda nos arquivos.
type Tool struct {
	Name string
	Args []string // comando completo
	dir  string
	kind string // formato da saída: sarif, github, eslint, text, tsc
}

func (t Tool) Command() string { return strings.Join(t.Args, " ") }

const timeout = 90 * time.Second

func exists(root, p string) bool {
	_, err := os.Stat(filepath.Join(root, p))
	return err == nil
}

func anyExists(root string, ps ...string) bool {
	for _, p := range ps {
		if exists(root, p) {
			return true
		}
	}
	return false
}

// local acha o binário em node_modules/.bin (nunca baixa nada, ao contrário do npx).
func local(root, name string) string {
	p := filepath.Join(root, "node_modules", ".bin", name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func onPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func withExt(files []string, exts ...string) []string {
	var out []string
	for _, f := range files {
		e := strings.ToLower(path.Ext(f))
		for _, x := range exts {
			if e == x {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// Detect devolve as ferramentas do projeto que valem para os arquivos alterados. Com o Qlty
// configurado, ele basta: já roda os linters do projeto.
func Detect(root string, files []string) []Tool {
	if len(files) == 0 {
		return nil
	}
	if exists(root, ".qlty/qlty.toml") && onPath("qlty") {
		return []Tool{{Name: "qlty", kind: "sarif", dir: root,
			Args: append([]string{"qlty", "check", "--sarif", "--no-progress", "--no-fix", "--no-fail"}, files...)}}
	}
	var ts []Tool
	js := withExt(files, ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte", ".astro", ".json", ".css")
	if len(js) > 0 {
		switch {
		case anyExists(root, "biome.json", "biome.jsonc") && local(root, "biome") != "":
			ts = append(ts, Tool{Name: "biome", kind: "github", dir: root, Args: append([]string{local(root, "biome"), "lint", "--reporter=github"}, js...)})
		case anyExists(root, ".oxlintrc.json", "oxlint.json", "oxlintrc.json") && local(root, "oxlint") != "":
			ts = append(ts, Tool{Name: "oxlint", kind: "github", dir: root, Args: append([]string{local(root, "oxlint"), "--format=github"}, js...)})
		case anyExists(root, "eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts", ".eslintrc", ".eslintrc.js", ".eslintrc.json", ".eslintrc.cjs", ".eslintrc.yml") && local(root, "eslint") != "":
			ts = append(ts, Tool{Name: "eslint", kind: "eslint", dir: root, Args: append([]string{local(root, "eslint"), "--format", "json"}, withExt(js, ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte")...)})
		}
		if exists(root, "tsconfig.json") && local(root, "tsc") != "" && len(withExt(files, ".ts", ".tsx")) > 0 {
			ts = append(ts, Tool{Name: "tsc", kind: "tsc", dir: root, Args: []string{local(root, "tsc"), "--noEmit", "--pretty", "false", "-p", "."}})
		}
	}
	if py := withExt(files, ".py"); len(py) > 0 && onPath("ruff") {
		ts = append(ts, Tool{Name: "ruff", kind: "text", dir: root, Args: append([]string{"ruff", "check", "--output-format=concise", "--no-cache"}, py...)})
	}
	if gofiles := withExt(files, ".go"); len(gofiles) > 0 && exists(root, "go.mod") && onPath("go") {
		pkgs := goPackages(gofiles)
		if anyExists(root, ".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json") && onPath("golangci-lint") {
			ts = append(ts, Tool{Name: "golangci-lint", kind: "text", dir: root, Args: append([]string{"golangci-lint", "run"}, pkgs...)})
		} else {
			ts = append(ts, Tool{Name: "go vet", kind: "text", dir: root, Args: append([]string{"go", "vet"}, pkgs...)})
		}
	}
	if len(withExt(files, ".rs")) > 0 && exists(root, "Cargo.toml") && onPath("cargo") {
		ts = append(ts, Tool{Name: "clippy", kind: "text", dir: root, Args: []string{"cargo", "clippy", "--quiet", "--message-format=short"}})
	}
	return ts
}

// goPackages são as pastas (./x/y) dos arquivos Go alterados.
func goPackages(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		d := "./" + path.Dir(f)
		if d == "./." {
			d = "."
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// Result é o que uma ferramenta devolveu.
type Result struct {
	Tool   Tool
	Issues []Issue
	Err    string // a ferramenta não rodou (não é "achou problemas")
	Took   time.Duration
}

// Run roda a ferramenta e lê os problemas.
func Run(ctx context.Context, t Tool) Result {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, t.Args[0], t.Args[1:]...)
	cmd.Dir = t.dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "FORCE_COLOR=0", "CI=1")
	out, err := cmd.CombinedOutput()
	r := Result{Tool: t, Took: time.Since(start)}
	r.Issues = Parse(t.Name, t.kind, string(out), t.dir)
	if ctx.Err() == context.DeadlineExceeded {
		r.Err = fmt.Sprintf("passou de %s", timeout)
	} else if err != nil && len(r.Issues) == 0 {
		if _, ok := err.(*exec.ExitError); !ok || strings.TrimSpace(string(out)) == "" || looksBroken(string(out)) {
			r.Err = firstLine(strings.TrimSpace(string(out)) + " " + err.Error())
		}
	}
	return r
}

// looksBroken: a saída é de uma ferramenta que não rodou (configuração, instalação).
func looksBroken(out string) bool {
	l := strings.ToLower(out)
	for _, s := range []string{"command not found", "cannot find module", "no such file", "configuration error", "failed to load", "is not recognized"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// ---- leitura das saídas ----

var (
	textRe   = regexp.MustCompile(`^(?:\./)?([^\s:()][^:()]*?):(\d+):(?:(\d+):)?\s*(.+)$`)
	tscRe    = regexp.MustCompile(`^([^\s(]+)\((\d+),(\d+)\):\s*(error|warning)\s+(TS\d+):\s*(.+)$`)
	githubRe = regexp.MustCompile(`^::(error|warning|notice)\s+([^:]*)::(.*)$`)
)

// Parse lê a saída de uma ferramenta.
func Parse(tool, kind, out, root string) []Issue {
	switch kind {
	case "sarif":
		return parseSARIF(tool, out, root)
	case "eslint":
		return parseESLint(tool, out, root)
	}
	var is []Issue
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		switch {
		case kind == "github":
			if m := githubRe.FindStringSubmatch(l); m != nil {
				props := map[string]string{}
				for _, kv := range strings.Split(m[2], ",") {
					k, v, _ := strings.Cut(strings.TrimSpace(kv), "=")
					props[k] = v
				}
				n, _ := strconv.Atoi(props["line"])
				is = append(is, Issue{Tool: tool, Path: rel(root, props["file"]), Line: n, Rule: props["title"],
					Message: strings.TrimSpace(m[3]), Error: m[1] == "error"})
			}
		case kind == "tsc":
			if m := tscRe.FindStringSubmatch(l); m != nil {
				n, _ := strconv.Atoi(m[2])
				is = append(is, Issue{Tool: tool, Path: rel(root, m[1]), Line: n, Rule: m[5], Message: m[6], Error: m[4] == "error"})
			}
		default:
			if m := textRe.FindStringSubmatch(l); m != nil && !strings.HasPrefix(m[1], "#") {
				n, _ := strconv.Atoi(m[2])
				msg := strings.TrimSpace(m[4])
				low := strings.ToLower(msg)
				isErr := strings.HasPrefix(low, "error") || !strings.HasPrefix(low, "warning")
				msg = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(msg, "error:"), "warning:"))
				is = append(is, Issue{Tool: tool, Path: rel(root, m[1]), Line: n, Message: msg, Error: isErr})
			}
		}
	}
	return is
}

func rel(root, p string) string {
	p = strings.TrimPrefix(filepath.ToSlash(p), "file://")
	if filepath.IsAbs(p) {
		if r, err := filepath.Rel(root, p); err == nil {
			p = filepath.ToSlash(r)
		}
	}
	return strings.TrimPrefix(p, "./")
}

func parseSARIF(tool, out, root string) []Issue {
	var doc struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Name string `json:"name"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					Physical struct {
						Artifact struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if i := strings.Index(out, "{"); i >= 0 {
		out = out[i:]
	}
	if json.Unmarshal([]byte(out), &doc) != nil {
		return nil
	}
	var is []Issue
	for _, run := range doc.Runs {
		name := tool
		if run.Tool.Driver.Name != "" {
			name = tool + "/" + strings.ToLower(run.Tool.Driver.Name)
		}
		for _, r := range run.Results {
			if len(r.Locations) == 0 {
				continue
			}
			loc := r.Locations[0].Physical
			is = append(is, Issue{Tool: name, Path: rel(root, loc.Artifact.URI), Line: loc.Region.StartLine, Rule: r.RuleID,
				Message: r.Message.Text, Error: r.Level == "error"})
		}
	}
	return is
}

func parseESLint(tool, out, root string) []Issue {
	var files []struct {
		FilePath string `json:"filePath"`
		Messages []struct {
			RuleID   string `json:"ruleId"`
			Severity int    `json:"severity"`
			Message  string `json:"message"`
			Line     int    `json:"line"`
		} `json:"messages"`
	}
	if i := strings.Index(out, "["); i >= 0 {
		out = out[i:]
	}
	if json.Unmarshal([]byte(out), &files) != nil {
		return nil
	}
	var is []Issue
	for _, f := range files {
		for _, m := range f.Messages {
			is = append(is, Issue{Tool: tool, Path: rel(root, f.FilePath), Line: m.Line, Rule: m.RuleID, Message: m.Message, Error: m.Severity == 2})
		}
	}
	return is
}

// ---- só o que é novo ----

// OnlyNew deixa só os problemas em linhas que a mudança acrescentou ou alterou. changed é
// arquivo → linhas novas; num arquivo novo inteiro (fora do git), a linha 0 marca tudo.
func OnlyNew(is []Issue, changed map[string]map[int]bool) []Issue {
	var out []Issue
	seen := map[string]bool{}
	for _, i := range is {
		lines, ok := changed[i.Path]
		if !ok || (i.Line > 0 && !lines[i.Line] && !lines[0]) { // linha 0: o arquivo inteiro é novo
			continue
		}
		k := fmt.Sprintf("%s:%d:%s", i.Path, i.Line, i.Message)
		if !seen[k] {
			seen[k] = true
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Error != out[b].Error {
			return out[a].Error
		}
		if out[a].Path != out[b].Path {
			return out[a].Path < out[b].Path
		}
		return out[a].Line < out[b].Line
	})
	return out
}

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// ChangedLines lê um diff com 0 linhas de contexto (git diff -U0) e devolve as linhas novas
// de cada arquivo.
func ChangedLines(diff string) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	cur := ""
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ "):
			cur = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
			if cur == "/dev/null" {
				cur = ""
			} else if out[cur] == nil {
				out[cur] = map[int]bool{}
			}
		case strings.HasPrefix(l, "@@") && cur != "":
			m := hunkRe.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			start, _ := strconv.Atoi(m[1])
			n := 1
			if m[2] != "" {
				n, _ = strconv.Atoi(m[2])
			}
			for k := start; k < start+n; k++ {
				out[cur][k] = true
			}
		}
	}
	return out
}
