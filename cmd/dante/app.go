package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	scrTrust screen = iota // confiar no repositório atual?
	scrRepo                // digitar outro repositório
	scrModel               // escolher o modelo
	scrMain                // área de trabalho: commits | arquivos | chat
	scrKey                 // cadastrar a chave de API de um provedor
)

var efforts = []string{"low", "medium", "high", "xhigh", "max"}

// Linha do painel de commits: as mudanças locais (worktree) ou uma linha do git log --graph.
type logRow struct {
	worktree bool
	c        repo.Commit
}

func (r logRow) selectable() bool { return r.worktree || r.c.SHA != "" }

type model struct {
	cfg           config.Config
	width, height int
	screen        screen
	status        string
	statusErr     bool
	spin          spinner.Model

	// confiança
	cwdRoot  string
	trustIdx int

	// escolha do repositório
	repoInput   textinput.Model
	recents     []string
	recentIdx   int
	completions []string
	opening     bool

	// escolha do modelo
	modelRows  []modelRow
	modelIdx   int
	modelOff   int
	modelWant  *config.Choice
	pulling    string // modelo sendo baixado no Ollama
	pullStatus string
	pullCancel context.CancelFunc
	listed     map[string][]string
	listErr    map[string]string

	// cadastro de chave
	keyProv       config.Provider
	keyThen       *modelRow
	keyInput      textinput.Model
	keyChecking   bool
	keyErr        string
	keyWarn       string
	keyUnverified string

	// área de trabalho
	repo     repo.Repo
	branch   string
	rows     []logRow
	logIdx   int
	logOff   int
	dirty    int
	target   string // SHA em revisão; repo.WorkTree = mudanças locais
	files    []repo.FileChange
	filesFor string
	fileIdx  int
	fileOff  int
	focus    paneID
	lay      layout
	rects    map[paneID]rect
	zoom     bool
	mode     int
	drag     *dragState

	preview    viewport.Model
	previewKey string
	vtab       vTab   // aba do Visualizador
	diffPath   string // arquivo do diff mostrado
	diffRaw    string
	docTitle   string
	docRaw     string
	docIsDiff  bool
	diffSplit  int // splitAuto, splitOn, splitOff
	diffCtx    int // linhas de contexto do diff na tela
	hunkStarts []int
	hunkLines  []int
	stats      map[string]repo.CommitStat
	reviewed   map[string]bool

	// chat
	prov        config.Provider
	modelName   string
	chat        llm.Chat
	sendTo      *string   // alvo usado pelas ferramentas durante a resposta
	diffSent    string    // alvo/arquivo cujo diff já foi enviado nesta conversa
	task        taskMode  // revisar ou explicar
	level       levelMode // normal, aprendiz ou rápido
	diffPending string    // diff enviado na mensagem em andamento
	entries     []entry
	input       textarea.Model
	chatView    viewport.Model
	streaming   bool
	cancel      context.CancelFunc
	events      chan tea.Msg
	usage       llm.Usage
	convStart   time.Time
	thinking    bool

	// opções da linha de comando
	flagModel string
}

func newModel(cfg config.Config, repoFlag, modelFlag string) model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = selectedStyle

	ri := textinput.New()
	ri.Placeholder = "~/projetos/meu-app  ou  https://github.com/dono/repo"
	ri.CharLimit = 500
	ri.Prompt = "› "
	ri.Focus()

	ta := textarea.New()
	ta.Placeholder = "Pergunte sobre as mudanças…"
	ta.ShowLineNumbers = false
	ta.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "› "
		}
		return "  "
	})
	ta.CharLimit = 0
	ta.SetHeight(3)
	ta.KeyMap.InsertNewline.SetKeys("alt+enter", "ctrl+j")
	ta.FocusedStyle.CursorLine = ta.FocusedStyle.CursorLine.UnsetBackground()

	target := repo.WorkTree
	m := model{
		cfg:       cfg,
		spin:      sp,
		repoInput: ri,
		recents:   loadRecents(),
		recentIdx: -1,
		listed:    map[string][]string{},
		listErr:   map[string]string{},
		input:     ta,
		preview:   viewport.New(40, 10),
		chatView:  viewport.New(40, 10),
		sendTo:    &target,
		flagModel: modelFlag,
		lay:       loadLayout(),
		diffCtx:   defaultDiffCtx,
	}
	switch {
	case repoFlag != "":
		m.screen = scrRepo
		m.opening = true
		m.repoInput.SetValue(repoFlag)
	default:
		if wd, err := os.Getwd(); err == nil {
			if root, err := repo.Root(wd); err == nil {
				m.cwdRoot = root
			}
		}
		if m.cwdRoot != "" {
			m.screen = scrTrust
		} else {
			m.screen = scrRepo
		}
	}
	return m
}

func (m model) Init() tea.Cmd {
	if m.opening {
		return tea.Batch(openRepo(m.repoInput.Value()), m.spin.Tick)
	}
	return textinput.Blink
}

// ---- mensagens assíncronas ----

type logMsg struct {
	root    string
	commits []repo.Commit
	dirty   int
	branch  string
	err     error
}

type filesMsg struct {
	root, target string
	files        []repo.FileChange
	err          error
}

type previewMsg struct {
	key, path, content string
}

type modelsListedMsg struct {
	prov   string
	models []string
	err    error
}

type chatEventMsg struct{ ev llm.Event }

type chatDoneMsg struct {
	usage llm.Usage
	err   error
}

func loadLog(r repo.Repo) tea.Cmd {
	return func() tea.Msg {
		cs, err := r.Log(800)
		wt, _ := r.Files(repo.WorkTree)
		return logMsg{root: r.Root, commits: cs, dirty: len(wt), branch: r.Branch(), err: err}
	}
}

func loadFiles(r repo.Repo, target string) tea.Cmd {
	return func() tea.Msg {
		fs, err := r.Files(target)
		return filesMsg{root: r.Root, target: target, files: fs, err: err}
	}
}

func loadDiff(r repo.Repo, target, path string, ctxLines int) tea.Cmd {
	key := target + "\x00" + path
	return func() tea.Msg {
		d, err := r.DiffContext(target, path, ctxLines)
		if err != nil {
			d = "erro: " + err.Error()
		}
		if strings.TrimSpace(d) == "" {
			d = "(sem diferenças de texto)"
		}
		return previewMsg{key: key, path: path, content: d}
	}
}

func listModels(p config.Provider) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ms, err := llm.ListModels(ctx, p.LLM(), p.Filtro)
		return modelsListedMsg{prov: p.ID, models: ms, err: err}
	}
}

func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// ---- Update ----

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			if m.pullCancel != nil {
				m.pullCancel()
			}
			return m, tea.Quit
		}

	case spinner.TickMsg:
		if m.opening || m.streaming || m.keyChecking || m.pulling != "" {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			if m.streaming {
				m.refreshChat()
			}
			return m, cmd
		}
		return m, nil

	case repoOpenedMsg:
		return m.onRepoOpened(msg)

	case keyCheckedMsg:
		return m.onKeyChecked(msg)

	case pullProgressMsg:
		m.pullStatus = msg.text
		return m, waitFor(msg.ch)

	case pullDoneMsg:
		return m.onPullDone(msg)

	case modelsListedMsg:
		if msg.err != nil {
			m.listErr[msg.prov] = msg.err.Error()
		} else {
			m.listed[msg.prov] = msg.models
			delete(m.listErr, msg.prov)
		}
		m.buildModelRows()
		return m, nil

	case logMsg:
		if msg.root != m.repo.Root {
			return m, nil
		}
		if msg.err != nil {
			m.setErr("git log: " + msg.err.Error())
		}
		return m.onLog(msg)

	case filesMsg:
		if msg.root != m.repo.Root || msg.target != m.target {
			return m, nil
		}
		if msg.err != nil {
			m.setErr(msg.err.Error())
		}
		sortFiles(msg.files)
		m.files, m.filesFor = msg.files, msg.target
		m.fileIdx, m.fileOff = 0, 0
		return m, m.previewSelected()

	case statsMsg:
		if msg.root == m.repo.Root {
			m.stats = msg.stats
		}
		return m, nil

	case previewMsg:
		if msg.key != m.previewKey {
			return m, nil
		}
		m.setDiff(msg.path, msg.content)
		return m, nil

	case chatEventMsg:
		m.onChatEvent(msg.ev)
		return m, waitFor(m.events)

	case chatDoneMsg:
		return m.onChatDone(msg)

	case editorDoneMsg:
		if msg.err != nil {
			m.setErr("editor: " + msg.err.Error())
		}
		// O arquivo pode ter mudado: recarrega mudanças locais.
		return m, tea.Batch(loadLog(m.repo), m.reloadFilesIfWorktree())
	}

	switch m.screen {
	case scrTrust:
		return m.updateTrust(msg)
	case scrRepo:
		return m.updateRepo(msg)
	case scrModel:
		return m.updateModel(msg)
	case scrMain:
		return m.updateMain(msg)
	case scrKey:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m *model) setErr(s string)  { m.status, m.statusErr = s, true }
func (m *model) setInfo(s string) { m.status, m.statusErr = s, false }

// ---- repositório aberto: monta a área de trabalho ----

func (m model) onRepoOpened(msg repoOpenedMsg) (tea.Model, tea.Cmd) {
	m.opening = false
	if msg.err != nil {
		m.screen = scrRepo
		m.setErr(msg.err.Error())
		m.repoInput.Focus()
		return m, textinput.Blink
	}
	if m.streaming && m.cancel != nil {
		m.cancel()
	}
	m.cfg.Trust(msg.root)
	_ = m.cfg.Save()
	m.recents = saveRecent(msg.root)
	m.repo = repo.Repo{Root: msg.root, Bloquear: m.cfg.ArquivosBloqueados, Liberar: m.cfg.ArquivosLiberados}
	m.rows, m.files, m.filesFor = nil, nil, ""
	m.logIdx, m.logOff, m.fileIdx, m.fileOff = 0, 0, 0, 0
	m.stats, m.reviewed = nil, loadReviewed(msg.root)
	m.docRaw, m.docTitle = "", ""
	m.clearPreview()
	m.target = repo.WorkTree
	m.focus = pLog
	m.mode = modeNormal
	m.status = ""
	m.resetChat()
	cmds := []tea.Cmd{loadLog(m.repo)}

	// Modelo vindo de --modelo: pula a escolha.
	if m.flagModel != "" {
		prov, name, _ := strings.Cut(m.flagModel, "/")
		m.flagModel = ""
		if p, ok := m.cfg.Provider(prov); ok && name != "" {
			m.selectModel(p, name)
			m.screen = scrMain
			m.relayout()
			return m, tea.Batch(cmds...)
		}
		m.setErr("--modelo deve ser provedor/modelo, ex.: deepseek/deepseek-chat")
	}
	m.screen = scrModel
	cmds = append(cmds, m.enterModelScreen()...)
	return m, tea.Batch(cmds...)
}

func (m model) onLog(msg logMsg) (tea.Model, tea.Cmd) {
	m.branch, m.dirty = msg.branch, msg.dirty
	prevTarget := m.target
	var rows []logRow
	if msg.dirty > 0 {
		rows = append(rows, logRow{worktree: true})
	}
	for _, c := range msg.commits {
		rows = append(rows, logRow{c: c})
	}
	m.rows = rows
	// Mantém a seleção no mesmo alvo, se ele ainda existir.
	m.logIdx = -1
	for i, r := range rows {
		if (r.worktree && prevTarget == repo.WorkTree) || (!r.worktree && r.c.SHA != "" && r.c.SHA == prevTarget) {
			m.logIdx = i
			break
		}
	}
	if m.logIdx < 0 {
		m.logIdx = m.nextSelectable(-1, 1)
	}
	if m.logIdx < 0 {
		m.logIdx = 0
		m.files = nil
		return m, nil
	}
	m.fixLogScroll()
	nm, cmd := m.selectTarget()
	return nm, tea.Batch(cmd, loadStats(m.repo, rows))
}

func (m model) selectedTarget() string {
	if m.logIdx < 0 || m.logIdx >= len(m.rows) {
		return repo.WorkTree
	}
	if r := m.rows[m.logIdx]; !r.worktree {
		return r.c.SHA
	}
	return repo.WorkTree
}

func (m model) selectTarget() (tea.Model, tea.Cmd) {
	t := m.selectedTarget()
	if t == m.filesFor && m.files != nil {
		m.target = t
		return m, nil
	}
	m.target = t
	m.files = nil
	m.filesFor = ""
	return m, loadFiles(m.repo, t)
}

func (m model) reloadFilesIfWorktree() tea.Cmd {
	if m.target == repo.WorkTree {
		return loadFiles(m.repo, repo.WorkTree)
	}
	return nil
}

func (m model) nextSelectable(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(m.rows); i += dir {
		if m.rows[i].selectable() {
			return i
		}
	}
	return -1
}

func (m *model) previewSelected() tea.Cmd {
	if len(m.files) == 0 {
		m.previewKey = ""
		m.clearPreview()
		return nil
	}
	f := m.files[m.fileIdx]
	m.previewKey = m.target + "\x00" + f.Path
	return loadDiff(m.repo, m.target, f.Path, max(m.diffCtx, defaultDiffCtx))
}

// ---- área de trabalho: teclas dos painéis ----

func (m model) updateLogKeys(key string) (tea.Model, tea.Cmd) {
	moved := false
	switch key {
	case "esc":
		return m.toRepoScreen()
	case "up", "k":
		if i := m.nextSelectable(m.logIdx, -1); i >= 0 {
			m.logIdx, moved = i, true
		}
	case "down", "j":
		if i := m.nextSelectable(m.logIdx, 1); i >= 0 {
			m.logIdx, moved = i, true
		}
	case "pgup":
		for n := 0; n < m.logHeight(); n++ {
			if i := m.nextSelectable(m.logIdx, -1); i >= 0 {
				m.logIdx, moved = i, true
			}
		}
	case "pgdown":
		for n := 0; n < m.logHeight(); n++ {
			if i := m.nextSelectable(m.logIdx, 1); i >= 0 {
				m.logIdx, moved = i, true
			}
		}
	case "home", "g":
		if i := m.nextSelectable(-1, 1); i >= 0 {
			m.logIdx, moved = i, true
		}
	case "end", "G":
		if i := m.nextSelectable(len(m.rows), -1); i >= 0 {
			m.logIdx, moved = i, true
		}
	case "enter":
		return m.setFocus(pFiles)
	case "r":
		return m.quickReview("")
	}
	m.fixLogScroll()
	if moved {
		return m.selectTarget()
	}
	return m, nil
}

func (m model) updateFilesKeys(key string) (tea.Model, tea.Cmd) {
	n := len(m.files)
	prev := m.fileIdx
	switch key {
	case "esc":
		return m.setFocus(pLog)
	case "up", "k":
		m.fileIdx = max(m.fileIdx-1, 0)
	case "down", "j":
		m.fileIdx = min(m.fileIdx+1, max(n-1, 0))
	case "J", "ctrl+d", "pgdown", " ":
		m.preview.HalfPageDown()
		return m, nil
	case "K", "ctrl+u", "pgup":
		m.preview.HalfPageUp()
		return m, nil
	case "enter":
		return m.setFocus(pChat)
	case "r":
		if n > 0 {
			return m.quickReview(m.files[m.fileIdx].Path)
		}
	case "o", "e":
		if n > 0 {
			f := m.files[m.fileIdx]
			if f.Status == "D" {
				m.setErr("o arquivo foi removido neste alvo")
				return m, nil
			}
			return m, openInEditor(m.repo.Root, f.Path, firstHunkLine(m.diffRaw))
		}
	case "d", "[", "]":
		// Volta do documento aberto pelo modelo para o diff.
		m.vtab = vtDiff
		m.renderPreview()
		return m, nil
	}
	m.fixFilesScroll()
	if m.fileIdx != prev {
		return m, m.previewSelected()
	}
	return m, nil
}

// Primeira linha nova do primeiro hunk do diff ("@@ -a,b +N,c @@").
func firstHunkLine(diff string) int {
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "@@") {
			if i := strings.Index(l, "+"); i >= 0 {
				num := strings.FieldsFunc(l[i+1:], func(r rune) bool { return r == ',' || r == ' ' })
				if len(num) > 0 {
					n, _ := strconv.Atoi(num[0])
					return n
				}
			}
		}
	}
	return 1
}

// ---- trocar de repositório ----

func (m model) toRepoScreen() (tea.Model, tea.Cmd) {
	m.screen = scrRepo
	m.status = ""
	m.recents = loadRecents()
	m.recentIdx = -1
	m.completions = nil
	m.repoInput.SetValue(prettyPath(m.repo.Root))
	m.repoInput.CursorEnd()
	m.repoInput.Focus()
	return m, textinput.Blink
}

func repoDisplayName(root string) string { return filepath.Base(root) }

func (m model) targetLabel() string {
	if m.target == repo.WorkTree {
		return fmt.Sprintf("mudanças locais (%s)", plural(m.dirty, "arquivo", "arquivos"))
	}
	for _, r := range m.rows {
		if r.c.SHA == m.target {
			return r.c.Short + " " + r.c.Subject
		}
	}
	return m.target
}
