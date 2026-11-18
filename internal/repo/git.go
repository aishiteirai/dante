// Package repo lê o repositório git: log, arquivos alterados, diffs e as ferramentas
// (somente leitura) que o modelo usa para abrir arquivos e investigar mudanças.
package repo

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Árvore vazia do git: base de comparação para o commit raiz.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// WorkTree identifica as mudanças locais (não commitadas) como alvo.
const WorkTree = ""

type Repo struct {
	Root     string
	Bloquear []string // padrões extras de arquivos sensíveis
	Liberar  []string // exceções aos padrões
}

func (r Repo) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Root, "-c", "core.quotepath=off"}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return string(out), fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return string(out), err
	}
	return string(out), nil
}

func Root(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%s não é um repositório git", path)
	}
	return strings.TrimSpace(string(out)), nil
}

// ---- Log ----

type Commit struct {
	Graph   string // prefixo do grafo (ex.: "* | ")
	SHA     string // vazio em linhas só de grafo
	Short   string
	Subject string
	Author  string
	When    string
	Refs    string
	Time    int64 // data do autor (unix), para datas relativas em português
}

// CommitStat é o tamanho de um commit: arquivos alterados e linhas.
type CommitStat struct {
	Files, Add, Del int
}

func (r Repo) Log(n int) ([]Commit, error) {
	out, err := r.git("log", "--graph", "--all", "--date-order", fmt.Sprintf("-n%d", n),
		"--format=%x1f%H%x1f%h%x1f%s%x1f%an%x1f%ar%x1f%D%x1f%at")
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, err
	}
	var cs []Commit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "\x1f")
		if len(p) < 8 {
			cs = append(cs, Commit{Graph: line})
			continue
		}
		t, _ := strconv.ParseInt(strings.TrimSpace(p[7]), 10, 64)
		cs = append(cs, Commit{Graph: p[0], SHA: p[1], Short: p[2], Subject: p[3], Author: p[4], When: p[5], Refs: p[6], Time: t})
	}
	return cs, nil
}

// Stats devolve o tamanho de cada commit (arquivos, linhas adicionadas e removidas), numa
// chamada só ao git. Commits de merge contam contra o primeiro pai.
func (r Repo) Stats(shas []string) (map[string]CommitStat, error) {
	out := map[string]CommitStat{}
	if len(shas) == 0 {
		return out, nil
	}
	args := append([]string{"show", "--no-renames", "--first-parent", "-m", "--shortstat", "--format=%x1f%H"}, shas...)
	text, err := r.git(args...)
	if err != nil && text == "" {
		return out, err
	}
	sha := ""
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "\x1f"):
			sha = strings.TrimPrefix(l, "\x1f")
			if _, ok := out[sha]; !ok {
				out[sha] = CommitStat{}
			}
		case sha != "" && strings.Contains(l, "changed"):
			out[sha] = parseShortstat(l)
		}
	}
	return out, nil
}

// " 3 files changed, 10 insertions(+), 2 deletions(-)"
func parseShortstat(l string) CommitStat {
	var s CommitStat
	for _, part := range strings.Split(l, ",") {
		f := strings.Fields(part)
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.Atoi(f[0])
		switch {
		case strings.HasPrefix(f[1], "file"):
			s.Files = n
		case strings.HasPrefix(f[1], "insertion"):
			s.Add = n
		case strings.HasPrefix(f[1], "deletion"):
			s.Del = n
		}
	}
	return s
}

func (r Repo) Branch() string {
	out, _ := r.git("rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out)
}

func (r Repo) Head() string {
	out, _ := r.git("rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func (r Repo) HasHead() bool {
	_, err := r.git("rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// ---- Arquivos alterados ----

type FileChange struct {
	Path    string
	OldPath string // nome anterior, nas renomeações e cópias
	Status  string // M, A, D, R, ? (não rastreado)...
	Add     int
	Del     int
	Binary  bool
}

// base devolve o par (de, para) a comparar para um commit, ou para as mudanças locais.
func (r Repo) base(sha string) (from, to string) {
	if sha == WorkTree {
		if r.HasHead() {
			return "HEAD", ""
		}
		return emptyTree, ""
	}
	if _, err := r.git("rev-parse", "--verify", "-q", sha+"^1"); err == nil {
		return sha + "^1", sha
	}
	return emptyTree, sha
}

func (r Repo) diffArgs(sha string, extra ...string) []string {
	from, to := r.base(sha)
	args := append([]string{"diff", "-M"}, extra...)
	args = append(args, from)
	if to != "" {
		args = append(args, to)
	}
	return args
}

func (r Repo) Files(sha string) ([]FileChange, error) {
	ns, err := r.git(r.diffArgs(sha, "--name-status")...)
	if err != nil {
		return nil, err
	}
	num, _ := r.git(r.diffArgs(sha, "--numstat")...)
	stats := map[string][3]int{}
	for _, l := range strings.Split(num, "\n") {
		f := strings.Split(l, "\t")
		if len(f) < 3 {
			continue
		}
		path := f[len(f)-1]
		if strings.Contains(path, " => ") { // renomeação: usa o nome novo
			path = renamedTo(path)
		}
		a, errA := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		bin := 0
		if errA != nil {
			bin = 1
		}
		stats[path] = [3]int{a, d, bin}
	}
	var fs []FileChange
	for _, l := range strings.Split(ns, "\n") {
		f := strings.Split(l, "\t")
		if len(f) < 2 {
			continue
		}
		fc := FileChange{Status: f[0][:1], Path: f[len(f)-1]}
		if len(f) >= 3 {
			fc.OldPath = f[1]
		}
		s := stats[fc.Path]
		fc.Add, fc.Del, fc.Binary = s[0], s[1], s[2] == 1
		fs = append(fs, fc)
	}
	if sha == WorkTree {
		un, _ := r.git("ls-files", "--others", "--exclude-standard")
		for _, p := range strings.Split(strings.TrimSpace(un), "\n") {
			if p != "" {
				fs = append(fs, FileChange{Path: p, Status: "?", Add: r.countLines(p)})
			}
		}
	}
	return fs, nil
}

func renamedTo(p string) string {
	// "dir/{a => b}/x" ou "a => b"
	if i, j := strings.Index(p, "{"), strings.Index(p, "}"); i >= 0 && j > i {
		mid := p[i+1 : j]
		_, to, _ := strings.Cut(mid, " => ")
		return strings.ReplaceAll(p[:i]+to+p[j+1:], "//", "/")
	}
	_, to, _ := strings.Cut(p, " => ")
	return to
}

func (r Repo) countLines(path string) int {
	out, err := r.git("diff", "--no-index", "--numstat", "/dev/null", path)
	if err == nil || out != "" {
		if f := strings.Fields(out); len(f) > 0 {
			n, _ := strconv.Atoi(f[0])
			return n
		}
	}
	return 0
}

// Diff de um arquivo no alvo (commit ou mudanças locais). path vazio = alvo inteiro.
func (r Repo) Diff(sha, path string) (string, error) { return r.DiffContext(sha, path, 3) }

// DiffContext é o Diff com `lines` linhas de contexto ao redor de cada mudança.
func (r Repo) DiffContext(sha, path string, lines int) (string, error) {
	if sha == WorkTree && path != "" {
		if tracked, _ := r.git("ls-files", "--error-unmatch", "--", path); strings.TrimSpace(tracked) == "" {
			// Não rastreado: mostra o arquivo inteiro como adição.
			out, _ := r.git("diff", "--no-index", "--", "/dev/null", path)
			return out, nil
		}
	}
	args := r.diffArgs(sha, fmt.Sprintf("-U%d", lines))
	if path != "" {
		args = append(args, "--", path)
	}
	return r.git(args...)
}

// Mensagem completa de um commit.
func (r Repo) CommitMessage(sha string) string {
	out, _ := r.git("log", "-1", "--format=%an <%ae>, %ad%n%n%B", sha)
	return strings.TrimSpace(out)
}

// ListFiles devolve os arquivos do repositório (versionados e novos, sem os ignorados),
// relativos à raiz.
func (r Repo) ListFiles() ([]string, error) {
	out, err := r.git("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var fs []string
	seen := map[string]bool{}
	for _, f := range strings.Split(out, "\x00") {
		if f != "" && !seen[f] {
			seen[f] = true
			fs = append(fs, f)
		}
	}
	return fs, nil
}
