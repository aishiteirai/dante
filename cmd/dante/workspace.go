package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

// ---- Diretório de dados (mesmo do harness Python) ----

func dataHome() string {
	for _, k := range []string{"DANTE_HOME", "REVISOR_HOME"} {
		if v := os.Getenv(k); v != "" {
			return expandHome(v)
		}
	}
	xdg := os.Getenv("XDG_DATA_HOME")
	if xdg == "" {
		home, _ := os.UserHomeDir()
		xdg = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(xdg, "dante")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

func prettyPath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// ---- Repositórios recentes ----

func recentsFile() string { return filepath.Join(dataHome(), "recentes.json") }

func loadRecents() []string {
	var rs []string
	if b, err := os.ReadFile(recentsFile()); err == nil {
		_ = json.Unmarshal(b, &rs)
	}
	out := rs[:0]
	for _, r := range rs {
		if st, err := os.Stat(r); err == nil && st.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

func saveRecent(root string) []string {
	rs := []string{root}
	for _, r := range loadRecents() {
		if r != root {
			rs = append(rs, r)
		}
	}
	if len(rs) > 15 {
		rs = rs[:15]
	}
	if err := os.MkdirAll(dataHome(), 0o755); err == nil {
		b, _ := json.MarshalIndent(rs, "", "  ")
		_ = os.WriteFile(recentsFile(), b, 0o644)
	}
	return rs
}

// ---- Abrir repositório (caminho local ou URL, que é clonada) ----

var (
	urlPrefix = regexp.MustCompile(`^(https?://|ssh://|git@|git://)`)
	shortRepo = regexp.MustCompile(`^(github\.com/)?[\w.-]+/[\w.-]+$`)
)

// Converte o que foi digitado em URL de clone, se não for um caminho local.
func remoteURL(in string) (string, bool) {
	if urlPrefix.MatchString(in) {
		return in, true
	}
	if _, err := os.Stat(expandHome(in)); err == nil {
		return "", false
	}
	if shortRepo.MatchString(in) { // "dono/repo" ou "github.com/dono/repo"
		return "https://github.com/" + strings.TrimPrefix(in, "github.com/"), true
	}
	return "", false
}

func cloneDir(url string) string {
	u := strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	u = strings.ReplaceAll(u, ":", "/")
	parts := strings.Split(u, "/")
	name := parts[len(parts)-1]
	if len(parts) > 1 {
		name = parts[len(parts)-2] + "-" + name
	}
	return filepath.Join(dataHome(), "repos", name)
}

type repoOpenedMsg struct {
	root   string
	cloned string // URL, quando o repositório foi clonado/atualizado
	err    error
}

func openRepo(input string) tea.Cmd {
	return func() tea.Msg {
		in := strings.TrimSpace(input)
		if in == "" {
			return repoOpenedMsg{err: fmt.Errorf("digite o caminho ou a URL do repositório")}
		}
		if url, ok := remoteURL(in); ok {
			dest := cloneDir(url)
			var c *exec.Cmd
			if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
				c = exec.Command("git", "-C", dest, "pull", "--ff-only", "-q")
			} else {
				_ = os.MkdirAll(filepath.Dir(dest), 0o755)
				c = exec.Command("git", "clone", "-q", url, dest)
			}
			// Sem prompt de senha: travaria a interface.
			c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
			if out, err := c.CombinedOutput(); err != nil {
				if _, statErr := os.Stat(filepath.Join(dest, ".git")); statErr != nil {
					return repoOpenedMsg{err: fmt.Errorf("não consegui clonar %s: %s", url, lastLines(string(out), err.Error()))}
				}
				// pull falhou (ex.: sem rede) mas o clone existe: segue com o que há.
			}
			root, err := repo.Root(dest)
			return repoOpenedMsg{root: root, cloned: url, err: err}
		}
		path := expandHome(in)
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if st, err := os.Stat(path); err != nil || !st.IsDir() {
			return repoOpenedMsg{err: fmt.Errorf("pasta não encontrada: %s", in)}
		}
		root, err := repo.Root(path)
		return repoOpenedMsg{root: root, err: err}
	}
}

// ---- Completar caminho com tab ----

func completePath(in string) (string, []string) {
	dirPart, base := "", in
	if i := strings.LastIndex(in, "/"); i >= 0 {
		dirPart, base = in[:i+1], in[i+1:]
	}
	dir := expandHome(dirPart)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return in, nil
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, base) || (strings.HasPrefix(n, ".") && !strings.HasPrefix(base, ".")) {
			continue
		}
		if e.IsDir() || (e.Type()&os.ModeSymlink != 0 && isDir(filepath.Join(dir, n))) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	switch len(names) {
	case 0:
		return in, nil
	case 1:
		return dirPart + names[0] + "/", nil
	}
	prefix := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return dirPart + prefix, names
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
