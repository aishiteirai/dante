// revisor-tui: interface de terminal para o harness revisor de código.
//
// Executa o harness Python (`python -m revisor ... --eventos`) e mostra o progresso
// e os achados de forma interativa.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Python padrão; o Makefile troca pelo do .venv com -ldflags "-X main.defaultPython=...".
var defaultPython = "python3"

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.Repo, "repo", ".", "repositório a revisar")
	flag.StringVar(&cfg.Python, "python", envOr("REVISOR_PYTHON", defaultPython), "interpretador Python com o pacote `revisor` instalado (env REVISOR_PYTHON)")
	flag.StringVar(&cfg.Effort, "esforco", envOr("REVISOR_ESFORCO", "high"), "esforço inicial: low, medium, high, xhigh, max")
	flag.StringVar(&cfg.Budget, "orcamento", os.Getenv("REVISOR_ORCAMENTO"), "teto de custo em US$ por revisão")
	modelo := flag.String("modelo", os.Getenv("REVISOR_MODELO"), "modelo (padrão do harness: claude-opus-5-5)")
	diretrizes := flag.String("diretrizes", "", "arquivo de diretrizes do time")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uso: revisor-tui [opções]\n\nInterface de terminal do revisor de código.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	root, err := repoRoot(cfg.Repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Erro:", err)
		os.Exit(2)
	}
	cfg.Repo = root
	cfg.RepoName = filepath.Base(root)
	if *modelo != "" {
		cfg.Extra = append(cfg.Extra, "--modelo", *modelo)
	}
	if *diretrizes != "" {
		abs, _ := filepath.Abs(*diretrizes)
		cfg.Extra = append(cfg.Extra, "--diretrizes", abs)
	}

	if err := exec.Command(cfg.Python, "-c", "import revisor").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Erro: o pacote `revisor` não foi encontrado em %q.\n"+
			"Rode `make install` na raiz do projeto, ou aponte --python / REVISOR_PYTHON para o Python do .venv.\n", cfg.Python)
		os.Exit(2)
	}

	p := tea.NewProgram(newModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Erro:", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Base padrão: a branch para onde origin/HEAD aponta; senão main ou master.
func defaultBase(repo string) string {
	if out, err := exec.Command("git", "-C", repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	for _, b := range []string{"main", "master"} {
		if exec.Command("git", "-C", repo, "rev-parse", "--verify", "-q", b).Run() == nil {
			return b
		}
	}
	return "main"
}
