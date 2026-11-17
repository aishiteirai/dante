package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	scrRepo screen = iota
	scrMenu
	scrBase
	scrCommits
	scrRunning
	scrResults
	scrHistory
)

var efforts = []string{"low", "medium", "high", "xhigh", "max"}

type menuItem struct{ title, desc string }

const menuLen = 6

// Itens do menu da área de trabalho; as descrições usam o estado do repositório.
func (m model) menuItems() []menuItem {
	w := m.ws
	work := "nada alterado"
	if n := w.Unstaged + w.Staged + w.Untracked; n > 0 {
		work = fmt.Sprintf("%d arquivo(s) alterado(s) · working tree vs HEAD", n)
		if w.Untracked > 0 {
			work += fmt.Sprintf(" · %d novo(s) fora do git não entram", w.Untracked)
		}
	}
	staged := "nada em stage"
	if w.Staged > 0 {
		staged = fmt.Sprintf("%d arquivo(s) em stage", w.Staged)
	}
	commits := "escolha um ou mais commits do git log"
	if w.Commits != "" {
		commits = fmt.Sprintf("%s commits no total · escolha um ou mais", w.Commits)
	}
	hist := "nenhuma revisão ainda"
	if n := len(m.history); n > 0 {
		hist = fmt.Sprintf("%d revisão(ões) anteriores deste repositório", n)
	}
	return []menuItem{
		{"Mudanças não commitadas", work},
		{"Mudanças em stage", staged},
		{"Branch atual vs base…", "tudo que a branch acrescenta (ex.: main)"},
		{"Commits do histórico…", commits},
		{"Histórico de revisões", hist},
		{"Trocar de repositório", prettyPath(m.cfg.Repo)},
	}
}

type model struct {
	cfg           Config
	width, height int
	screen        screen
	status        string // mensagem de rodapé (erro/aviso)

	// abertura do repositório
	repoInput   textinput.Model
	recents     []string
	recentIdx   int
	completions []string
	opening     bool
	ws          workspaceInfo

	// menu
	menuIdx int

	// base
	baseInput textinput.Model

	// commits
	commits     []Commit
	commitIdx   int
	commitOff   int
	selected    map[string]bool
	reviewedSHA map[string]Review

	// execução
	run       *runner
	spin      spinner.Model
	runIdx    int
	runTotal  int
	runDesc   string
	runLog    []string
	runStart  time.Time
	runCost   float64
	runResult []Review

	// resultados
	reviews   []Review
	revIdx    int
	findIdx   int
	findOff   int
	minSev    int // mostra severidades com índice <= minSev
	detail    viewport.Model
	resultsBk screen

	// histórico
	history []Review
	histIdx int
	histOff int
}

func newModel(cfg Config) model {
	ti := textinput.New()
	ti.Placeholder = "main"
	ti.CharLimit = 200
	ri := textinput.New()
	ri.Placeholder = "~/projetos/meu-app  ou  https://github.com/dono/repo"
	ri.CharLimit = 500
	ri.Prompt = "› "
	ri.Focus()
	if cfg.Repo != "" {
		ri.SetValue(cfg.Repo)
	} else if wd, err := os.Getwd(); err == nil {
		if root, err := repoRoot(wd); err == nil {
			ri.SetValue(prettyPath(root))
		}
	}
	ri.CursorEnd()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = selectedStyle
	return model{
		cfg:         cfg,
		baseInput:   ti,
		repoInput:   ri,
		recents:     loadRecents(),
		recentIdx:   -1,
		opening:     cfg.Repo != "",
		spin:        sp,
		selected:    map[string]bool{},
		reviewedSHA: map[string]Review{},
		minSev:      len(severities) - 1,
		detail:      viewport.New(80, 20),
	}
}

func (m model) Init() tea.Cmd {
	if m.opening { // --repo na linha de comando: abre direto
		return tea.Batch(openRepo(m.cfg.Repo), m.spin.Tick)
	}
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layoutDetail()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.run != nil {
				m.run.cancel()
			}
			return m, tea.Quit
		}

	case spinner.TickMsg:
		if m.screen == scrRunning || m.opening {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil

	case eventMsg:
		return m.onEvent(msg.ev)

	case repoOpenedMsg:
		return m.onRepoOpened(msg)

	case workspaceMsg:
		if msg.root == m.cfg.Repo {
			m.ws = msg.info
		}
		return m, nil

	case procDoneMsg:
		return m.onProcDone(msg)

	case historyMsg:
		if msg.err != nil {
			m.status = "Erro ao ler o histórico: " + msg.err.Error()
			return m, nil
		}
		m.history = msg.reviews
		m.reviewedSHA = map[string]Review{}
		for i := len(msg.reviews) - 1; i >= 0; i-- { // o mais recente vence
			r := msg.reviews[i]
			if r.Alvo.Tipo == "commit" && r.Alvo.Commit != nil && r.Erro == nil {
				m.reviewedSHA[*r.Alvo.Commit] = r
			}
		}
		return m, nil

	case commitsMsg:
		if msg.err != nil {
			m.status = "Erro no git log: " + msg.err.Error()
			m.screen = scrMenu
			return m, nil
		}
		m.commits = msg.commits
		return m, nil

	case editorDoneMsg:
		if msg.err != nil {
			m.status = "Editor: " + msg.err.Error()
		}
		return m, nil
	}

	switch m.screen {
	case scrRepo:
		return m.updateRepo(msg)
	case scrMenu:
		return m.updateMenu(msg)
	case scrBase:
		return m.updateBase(msg)
	case scrCommits:
		return m.updateCommits(msg)
	case scrRunning:
		return m.updateRunning(msg)
	case scrResults:
		return m.updateResults(msg)
	case scrHistory:
		return m.updateHistory(msg)
	}
	return m, nil
}

// ---- Menu ----

func (m model) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	m.status = ""
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		return m.toRepoScreen()
	case "up", "k":
		m.menuIdx = (m.menuIdx + menuLen - 1) % menuLen
	case "down", "j":
		m.menuIdx = (m.menuIdx + 1) % menuLen
	case "e":
		m.cfg.Effort = cycle(efforts, m.cfg.Effort)
	case "u": // atualiza as informações do repositório
		return m, tea.Batch(loadWorkspace(m.cfg.Repo), loadHistory(m.cfg))
	case "1", "2", "3", "4", "5", "6":
		m.menuIdx, _ = strconv.Atoi(k.String())
		m.menuIdx--
		return m.selectMenu()
	case "enter":
		return m.selectMenu()
	}
	return m, nil
}

func (m model) selectMenu() (tea.Model, tea.Cmd) {
	switch m.menuIdx {
	case 0:
		return m.startRun(nil)
	case 1:
		return m.startRun([]string{"--staged"})
	case 2:
		m.screen = scrBase
		m.baseInput.Focus()
		m.baseInput.CursorEnd()
		return m, textinput.Blink
	case 3:
		m.screen = scrCommits
		m.selected = map[string]bool{}
		m.commitIdx, m.commitOff = 0, 0
		return m, tea.Batch(loadCommits(m.cfg), loadHistory(m.cfg))
	case 4:
		m.screen = scrHistory
		m.histIdx, m.histOff = 0, 0
		return m, loadHistory(m.cfg)
	case 5:
		return m.toRepoScreen()
	}
	return m, nil
}

// ---- Abertura do repositório ----

func (m model) toRepoScreen() (tea.Model, tea.Cmd) {
	m.screen = scrRepo
	m.status = ""
	m.recents = loadRecents()
	m.recentIdx = -1
	m.completions = nil
	m.repoInput.SetValue(prettyPath(m.cfg.Repo))
	m.repoInput.CursorEnd()
	m.repoInput.Focus()
	return m, textinput.Blink
}

func (m model) updateRepo(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.opening {
		return m, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			if m.cfg.Repo != "" { // volta para a área de trabalho aberta
				m.screen = scrMenu
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			m.opening = true
			m.status = ""
			m.completions = nil
			return m, tea.Batch(openRepo(m.repoInput.Value()), m.spin.Tick)
		case "up", "down":
			if n := len(m.recents); n > 0 {
				if k.String() == "down" {
					m.recentIdx = min(m.recentIdx+1, n-1)
				} else {
					m.recentIdx = max(m.recentIdx-1, 0)
				}
				m.repoInput.SetValue(prettyPath(m.recents[m.recentIdx]))
				m.repoInput.CursorEnd()
			}
			return m, nil
		case "tab":
			v, opts := completePath(m.repoInput.Value())
			m.repoInput.SetValue(v)
			m.repoInput.CursorEnd()
			m.completions = opts
			return m, nil
		}
		m.completions = nil
		m.status = ""
	}
	var cmd tea.Cmd
	m.repoInput, cmd = m.repoInput.Update(msg)
	return m, cmd
}

func (m model) onRepoOpened(msg repoOpenedMsg) (tea.Model, tea.Cmd) {
	m.opening = false
	if msg.err != nil {
		m.screen = scrRepo
		m.status = msg.err.Error()
		m.repoInput.Focus()
		return m, textinput.Blink
	}
	// Monta a área de trabalho do repositório escolhido.
	m.cfg.Repo = msg.root
	m.cfg.RepoName = filepath.Base(msg.root)
	m.recents = saveRecent(msg.root)
	m.baseInput.SetValue(defaultBase(msg.root))
	m.ws = workspaceInfo{}
	m.history, m.commits = nil, nil
	m.reviewedSHA = map[string]Review{}
	m.menuIdx = 0
	m.screen = scrMenu
	m.status = ""
	return m, tea.Batch(loadWorkspace(msg.root), loadHistory(m.cfg))
}

// ---- Base ----

func (m model) updateBase(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			m.screen = scrMenu
			return m, nil
		case "enter":
			base := strings.TrimSpace(m.baseInput.Value())
			if base == "" {
				base = "main"
			}
			return m.startRun([]string{"--base", base})
		}
	}
	var cmd tea.Cmd
	m.baseInput, cmd = m.baseInput.Update(msg)
	return m, cmd
}

// ---- Commits ----

func (m model) updateCommits(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	n := len(m.commits)
	switch k.String() {
	case "esc", "q":
		m.screen = scrMenu
	case "up", "k":
		m.commitIdx = max(m.commitIdx-1, 0)
	case "down", "j":
		m.commitIdx = min(m.commitIdx+1, max(n-1, 0))
	case "pgup":
		m.commitIdx = max(m.commitIdx-m.listHeight(), 0)
	case "pgdown":
		m.commitIdx = min(m.commitIdx+m.listHeight(), max(n-1, 0))
	case "home", "g":
		m.commitIdx = 0
	case "end", "G":
		m.commitIdx = max(n-1, 0)
	case " ", "x":
		if n > 0 {
			sha := m.commits[m.commitIdx].SHA
			m.selected[sha] = !m.selected[sha]
			if !m.selected[sha] {
				delete(m.selected, sha)
			}
			m.commitIdx = min(m.commitIdx+1, n-1)
		}
	case "a": // seleciona todos os ainda não revisados acima do cursor (inclusive)
		for i := 0; i <= m.commitIdx && i < n; i++ {
			if _, done := m.reviewedSHA[m.commits[i].SHA]; !done {
				m.selected[m.commits[i].SHA] = true
			}
		}
	case "c":
		m.selected = map[string]bool{}
	case "v": // abre a revisão anterior do commit sob o cursor
		if n > 0 {
			if r, ok := m.reviewedSHA[m.commits[m.commitIdx].SHA]; ok {
				return m.showResults([]Review{r}, scrCommits), nil
			}
		}
	case "enter":
		if n == 0 {
			return m, nil
		}
		// Do mais antigo para o mais novo, como no histórico.
		var args []string
		for i := n - 1; i >= 0; i-- {
			if m.selected[m.commits[i].SHA] {
				args = append(args, "--commit", m.commits[i].SHA)
			}
		}
		if len(args) == 0 {
			args = []string{"--commit", m.commits[m.commitIdx].SHA}
		}
		return m.startRun(args)
	}
	m.commitOff = scrollTo(m.commitIdx, m.commitOff, m.listHeight())
	return m, nil
}

// ---- Execução ----

func (m model) startRun(targetArgs []string) (tea.Model, tea.Cmd) {
	r, wait := startReview(m.cfg, targetArgs)
	m.run = r
	m.screen = scrRunning
	m.status = ""
	m.runIdx, m.runTotal, m.runDesc = 0, 0, "preparando…"
	m.runLog = nil
	m.runStart = time.Now()
	m.runCost = 0
	m.runResult = nil
	return m, tea.Batch(wait, m.spin.Tick)
}

func (m model) onEvent(ev Event) (tea.Model, tea.Cmd) {
	switch ev.Tipo {
	case "alvos":
		m.runTotal = len(ev.Alvos)
	case "inicio":
		m.runIdx, m.runTotal, m.runDesc = ev.Indice, ev.Total, ev.Descricao
		m.addLog(boldStyle.Render(fmt.Sprintf("[%d/%d] %s", ev.Indice, ev.Total, ev.Descricao)))
	case "ferramenta":
		m.addLog(subtleStyle.Render("  · ") + ev.Nome + " " + subtleStyle.Render(ev.Arg))
	case "texto":
		m.addLog("  » " + firstLine(ev.Texto))
	case "resultado":
		if r := ev.Revisao; r != nil {
			m.runResult = append(m.runResult, *r)
			if r.CustoUSD != nil {
				m.runCost += *r.CustoUSD
			}
			line := "  " + okStyle.Render("✓ concluída") + "  " + sevCompact(r.Achados)
			if r.Erro != nil {
				line = "  " + errStyle.Render("✗ "+*r.Erro)
			}
			m.addLog(line)
		}
	}
	if m.run != nil {
		return m, waitFor(m.run.ch)
	}
	return m, nil
}

func (m model) onProcDone(msg procDoneMsg) (tea.Model, tea.Cmd) {
	m.run = nil
	cancelled := errors.Is(msg.err, context.Canceled)
	switch {
	case len(m.runResult) > 0:
		if cancelled {
			m.status = "Cancelado; mostrando as revisões já concluídas."
		}
		return m.showResults(m.runResult, scrMenu), loadHistory(m.cfg)
	case cancelled:
		m.status = "Revisão cancelada."
	case msg.err != nil:
		m.status = "Falha: " + lastLines(msg.stderr, msg.err.Error())
	default:
		m.status = "Nada para revisar."
	}
	m.screen = scrMenu
	return m, nil
}

func (m *model) addLog(s string) {
	m.runLog = append(m.runLog, s)
	if len(m.runLog) > 500 {
		m.runLog = m.runLog[len(m.runLog)-500:]
	}
}

func (m model) updateRunning(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "esc" || k.String() == "q") {
		if m.run != nil {
			m.run.cancel()
			m.runDesc = "cancelando…"
		}
	}
	return m, nil
}

// ---- Resultados ----

func (m model) showResults(rs []Review, back screen) model {
	m.reviews = rs
	m.revIdx, m.findIdx, m.findOff = 0, 0, 0
	m.resultsBk = back
	m.screen = scrResults
	m.layoutDetail()
	return m
}

func (m model) visibleFindings() []Finding {
	if len(m.reviews) == 0 {
		return nil
	}
	var out []Finding
	for _, f := range m.reviews[m.revIdx].Achados {
		if sevIndex(f.Severidade) <= m.minSev {
			out = append(out, f)
		}
	}
	return out
}

func (m model) updateResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}
	fs := m.visibleFindings()
	switch k.String() {
	case "esc", "q":
		m.screen = m.resultsBk
		return m, nil
	case "up", "k":
		m.findIdx = max(m.findIdx-1, 0)
		m.detail.GotoTop()
	case "down", "j":
		m.findIdx = min(m.findIdx+1, max(len(fs)-1, 0))
		m.detail.GotoTop()
	case "tab", "right", "l", "n":
		if len(m.reviews) > 1 {
			m.revIdx = (m.revIdx + 1) % len(m.reviews)
			m.findIdx, m.findOff = 0, 0
		}
	case "shift+tab", "left", "h", "p":
		if len(m.reviews) > 1 {
			m.revIdx = (m.revIdx + len(m.reviews) - 1) % len(m.reviews)
			m.findIdx, m.findOff = 0, 0
		}
	case "f":
		m.minSev--
		if m.minSev < 0 {
			m.minSev = len(severities) - 1
		}
		m.findIdx, m.findOff = 0, 0
	case "pgdown", "ctrl+d", " ":
		m.detail.HalfPageDown()
		return m, nil
	case "pgup", "ctrl+u":
		m.detail.HalfPageUp()
		return m, nil
	case "o", "enter":
		if len(fs) > 0 {
			return m, openInEditor(m.cfg.Repo, fs[m.findIdx])
		}
	}
	m.findOff = scrollTo(m.findIdx, m.findOff, m.resultsListHeight())
	m.layoutDetail()
	return m, nil
}

// ---- Histórico ----

func (m model) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	n := len(m.history)
	switch k.String() {
	case "esc", "q":
		m.screen = scrMenu
	case "up", "k":
		m.histIdx = max(m.histIdx-1, 0)
	case "down", "j":
		m.histIdx = min(m.histIdx+1, max(n-1, 0))
	case "pgup":
		m.histIdx = max(m.histIdx-m.listHeight(), 0)
	case "pgdown":
		m.histIdx = min(m.histIdx+m.listHeight(), max(n-1, 0))
	case "enter":
		if n > 0 {
			return m.showResults([]Review{m.history[m.histIdx]}, scrHistory), nil
		}
	case "r": // revisa de novo o mesmo alvo, quando é um commit
		if n > 0 {
			if c := m.history[m.histIdx].Alvo.Commit; c != nil && m.history[m.histIdx].Alvo.Tipo == "commit" {
				return m.startRun([]string{"--commit", *c})
			}
			m.status = "Só dá para repetir revisões de commits."
		}
	}
	m.histOff = scrollTo(m.histIdx, m.histOff, m.listHeight())
	return m, nil
}

// ---- utilidades ----

func cycle(xs []string, cur string) string {
	for i, x := range xs {
		if x == cur {
			return xs[(i+1)%len(xs)]
		}
	}
	return xs[0]
}

func scrollTo(idx, off, height int) int {
	if height < 1 {
		height = 1
	}
	if idx < off {
		return idx
	}
	if idx >= off+height {
		return idx - height + 1
	}
	return off
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	return s
}

func lastLines(stderr, fallback string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return fallback
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " | ")
}

func itoa(i int) string { return strconv.Itoa(i) }
