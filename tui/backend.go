package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ---- Tipos que espelham o JSON do harness Python ----

type Finding struct {
	Arquivo    string `json:"arquivo"`
	Linha      int    `json:"linha"`
	Severidade string `json:"severidade"`
	Categoria  string `json:"categoria"`
	Titulo     string `json:"titulo"`
	Descricao  string `json:"descricao"`
	Cenario    string `json:"cenario"`
	Sugestao   string `json:"sugestao"`
	Confianca  string `json:"confianca"`
}

type Target struct {
	Tipo        string   `json:"tipo"`
	Descricao   string   `json:"descricao"`
	Commit      *string  `json:"commit"`
	ComandoDiff string   `json:"comando_diff"`
	Arquivos    []string `json:"arquivos"`
}

type Review struct {
	Data     string    `json:"data"`
	Alvo     Target    `json:"alvo"`
	Resumo   string    `json:"resumo"`
	Achados  []Finding `json:"achados"`
	Erro     *string   `json:"erro"`
	CustoUSD *float64  `json:"custo_usd"`
	Turnos   int       `json:"turnos"`
	Arquivo  string    `json:"_arquivo"`
}

type Event struct {
	Tipo      string  `json:"tipo"`
	Indice    int     `json:"indice"`
	Total     int     `json:"total"`
	Descricao string  `json:"descricao"`
	Nome      string  `json:"nome"`
	Arg       string  `json:"arg"`
	Texto     string  `json:"texto"`
	Revisao   *Review `json:"revisao"`
	Codigo    int     `json:"codigo"`
	Alvos     []struct {
		Descricao string `json:"descricao"`
	} `json:"alvos"`
}

type Commit struct {
	SHA, Short, Subject, Author, When string
}

// ---- Configuração ----

type Config struct {
	Repo     string
	Python   string
	Effort   string
	Budget   string
	FailOn   string
	Extra    []string // argumentos extras repassados a `revisar`
	RepoName string
}

func (c Config) baseArgs() []string {
	return []string{"-m", "revisor", "--repo", c.Repo}
}

// ---- Mensagens ----

type eventMsg struct{ ev Event }
type procDoneMsg struct {
	err    error
	stderr string
}
type historyMsg struct {
	reviews []Review
	err     error
}
type commitsMsg struct {
	commits []Commit
	err     error
}
type editorDoneMsg struct{ err error }

// ---- Execução de uma revisão (processo Python com --eventos) ----

type runner struct {
	ch     chan tea.Msg
	cancel context.CancelFunc
}

func startReview(cfg Config, targetArgs []string) (*runner, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	args := append(cfg.baseArgs(), "revisar", "--eventos", "--esforco", cfg.Effort)
	if cfg.Budget != "" {
		args = append(args, "--orcamento", cfg.Budget)
	}
	args = append(args, cfg.Extra...)
	args = append(args, targetArgs...)

	cmd := exec.CommandContext(ctx, cfg.Python, args...)
	// Cancelar manda SIGINT para o SDK encerrar o `claude` filho; se não sair em 5 s, kill.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	r := &runner{ch: make(chan tea.Msg, 64), cancel: cancel}

	stdout, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		cancel()
		go func() { r.ch <- procDoneMsg{err: err}; close(r.ch) }()
		return r, waitFor(r.ch)
	}

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
		for sc.Scan() {
			var ev Event
			if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Tipo != "" {
				r.ch <- eventMsg{ev}
			}
		}
		werr := cmd.Wait()
		// Código 1 = achados acima de --falhar-em; não é falha de execução.
		if ee, ok := werr.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			werr = nil
		}
		if ctx.Err() != nil {
			werr = context.Canceled
		}
		r.ch <- procDoneMsg{err: werr, stderr: stderr.String()}
		close(r.ch)
	}()
	return r, waitFor(r.ch)
}

func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m, ok := <-ch
		if !ok {
			return nil
		}
		return m
	}
}

// ---- Histórico e git ----

func loadHistory(cfg Config) tea.Cmd {
	return func() tea.Msg {
		args := append(cfg.baseArgs(), "historico", "--json", "--limite", "1000")
		out, err := exec.Command(cfg.Python, args...).Output()
		if err != nil {
			return historyMsg{err: cmdErr(err)}
		}
		var rs []Review
		if err := json.Unmarshal(out, &rs); err != nil {
			return historyMsg{err: err}
		}
		// Mais recentes primeiro.
		for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
			rs[i], rs[j] = rs[j], rs[i]
		}
		return historyMsg{reviews: rs}
	}
}

func loadCommits(cfg Config) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("git", "-C", cfg.Repo, "log", "--no-merges", "-n", "300",
			"--format=%H%x1f%h%x1f%s%x1f%an%x1f%ar").Output()
		if err != nil {
			return commitsMsg{err: cmdErr(err)}
		}
		var cs []Commit
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			p := strings.Split(line, "\x1f")
			if len(p) == 5 {
				cs = append(cs, Commit{p[0], p[1], p[2], p[3], p[4]})
			}
		}
		return commitsMsg{commits: cs}
	}
}

func repoRoot(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%s não é um repositório git", path)
	}
	return strings.TrimSpace(string(out)), nil
}

func cmdErr(err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}

// ---- Editor ----

func openInEditor(root string, f Finding) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	file := filepath.Join(root, f.Arquivo)
	line := fmt.Sprint(max(f.Linha, 1))
	switch filepath.Base(parts[0]) {
	case "code", "cursor", "codium":
		parts = append(parts, "-g", file+":"+line)
	case "zed", "subl", "hx", "helix":
		parts = append(parts, file+":"+line)
	default: // vim, nvim, nano, micro, emacs, helix...
		parts = append(parts, "+"+line, file)
	}
	c := exec.Command(parts[0], parts[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}
