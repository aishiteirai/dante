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
	"github.com/aishiteirai/dante/internal/mapa"
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
	rng      string // "base...branch": a branch atual inteira, como um PR
	ahead    int
}

func (r logRow) selectable() bool { return r.worktree || r.rng != "" || r.c.SHA != "" }

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
	modelRows      []modelRow
	modelIdx       int
	modelOff       int
	modelWant      *config.Choice
	modelExp       string // provedor aberto na tela de modelos
	modelFilter    string // busca na tela de modelos
	modelFiltering bool
	pulling        string // modelo sendo baixado no Ollama
	pullStatus     string
	pullCancel     context.CancelFunc
	listed         map[string][]string
	listErr        map[string]string

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
	navOpen  bool // Navegador: arquivos do commit selecionado abertos embaixo dele
	navFile  bool // Navegador: cursor num arquivo (senão, no commit)
	focus    paneID
	lay      layout
	rects    map[paneID]rect
	zoom     bool
	mode     int
	drag     *dragState

	preview      viewport.Model
	previewKey   string
	vtab         vTab   // aba do Visualizador
	diffPath     string // arquivo do diff mostrado
	diffRaw      string
	docTitle     string
	docRaw       string
	docIsDiff    bool
	diffSplit    int // splitAuto, splitOn, splitOff
	diffCtx      int // linhas de contexto do diff na tela
	hunkStarts   []int
	hunkLines    []int
	rowLines     []int // linha do arquivo em cada linha da tela do Visualizador
	markLine     int   // linha marcada (referência aberta)
	pendingLine  int   // linha a marcar quando o próximo conteúdo chegar
	refs         []refEntry
	refIdx       int    // seleção na lista de referências
	refDigits    string // primeiro dígito de um número de referência com dois dígitos
	showActivity bool   // ctrl+o: cada leitura do modelo numa linha
	stats        map[string]repo.CommitStat
	reviewed     map[string]bool

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
	agent       agentMode        // ler ou editar (sempre começa em ler)
	bus         *writeBus        // modo, permissões e aprovações das ferramentas de escrita
	approval    *pendingApproval // mudança esperando s/n/t/e
	undo        []undoGroup      // mudanças de cada resposta, para ctrl+z
	permIdx     int              // seleção no painel de permissões
	packed      *packedDiffs     // diffs por arquivo já enviados na conversa
	tipShown    bool             // dica de ctrl+n já mostrada nesta conversa
	isolated    bool             // a resposta em andamento é uma pergunta avulsa (fora da conversa)
	purpose     string           // o que fazer com a resposta avulsa ao terminar ("commits", "pr")
	plan        *commitPlan      // plano de commits (/commits)
	ftab        *fileTab         // aba Arquivo do Visualizador
	navProject  bool             // Navegador mostrando o projeto inteiro (p)
	proj        *projTree
	fileInput   string // linha de busca (/…) ou de ir para a linha (:…) da aba Arquivo
	planIdx     int
	mapData     *mapa.Map // mapa do projeto salvo
	mapGen      *mapState // confirmação ou geração do mapa em andamento
	mapChapter  int       // capítulo do tour na aba Mapa
	mapCache    string
	mapCacheKey string

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
		navOpen:   true,
		packed:    newPackedDiffs(),
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
	base    string // branch principal, para revisar a atual como um PR
	ahead   int    // commits da branch atual que a base não tem
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
		msg := logMsg{root: r.Root, commits: cs, dirty: len(wt), branch: r.Branch(), err: err}
		if base := r.BaseBranch(); base != "" && msg.branch != "" && base != msg.branch && base != "origin/"+msg.branch {
			msg.base, msg.ahead = base, len(r.RangeCommits(repo.Range(base, msg.branch), 500))
		}
		return msg
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
			if m.mapGen != nil && m.mapGen.cancel != nil {
				m.mapGen.cancel()
			}
			if m.pullCancel != nil {
				m.pullCancel()
			}
			return m, tea.Quit
		}

	case spinner.TickMsg:
		if m.opening || m.streaming || m.keyChecking || m.pulling != "" || (m.mapGen != nil && m.mapGen.running) {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			if m.streaming {
				m.refreshChat()
			}
			if m.mapGen != nil && m.mapGen.running && m.vtab == vtMap {
				m.renderPreview()
			}
			return m, cmd
		}
		return m, nil

	case repoOpenedMsg:
		return m.onRepoOpened(msg)

	case refDocMsg:
		return m.onRefDoc(msg)

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

	case changeMsg:
		return m.onChange(msg)

	case approvalEditedMsg:
		return m.onApprovalEdited(msg)

	case projMsg:
		return m.onProject(msg)

	case filePackMsg:
		return m.onFilePack(msg)

	case locatePackMsg:
		return m.onLocatePack(msg)

	case fileTabMsg:
		return m.onFileTab(msg)

	case commitEditedMsg:
		return m.onCommitEdited(msg)

	case funcPackMsg:
		return m.onFuncPack(msg)

	case mapScannedMsg:
		return m.onMapScanned(msg)

	case mapEventMsg:
		return m.onMapEvent(msg)

	case mapDoneMsg:
		return m.onMapDone(msg)

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
	m.navFile = false
	m.stats, m.reviewed = nil, loadReviewed(msg.root)
	m.mapData, _ = mapa.Load(mapPath(msg.root))
	m.mapGen, m.mapCacheKey, m.mapChapter = nil, "", 0
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
	if msg.ahead > 0 {
		rows = append(rows, logRow{rng: repo.Range(msg.base, msg.branch), ahead: msg.ahead})
	}
	for _, c := range msg.commits {
		rows = append(rows, logRow{c: c})
	}
	m.rows = rows
	// Mantém a seleção no mesmo alvo, se ele ainda existir.
	m.logIdx = -1
	for i, r := range rows {
		if (r.worktree && prevTarget == repo.WorkTree) || (r.rng != "" && r.rng == prevTarget) ||
			(!r.worktree && r.c.SHA != "" && r.c.SHA == prevTarget) {
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
	if r := m.rows[m.logIdx]; r.rng != "" {
		return r.rng
	} else if !r.worktree {
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

// moveCommit leva o cursor do Navegador ao commit anterior (-1) ou ao próximo (1).
func (m model) moveCommit(dir, steps int) (tea.Model, tea.Cmd) {
	moved := false
	for n := 0; n < steps; n++ {
		if i := m.nextSelectable(m.logIdx, dir); i >= 0 {
			m.logIdx, moved = i, true
		}
	}
	m.navFile = false
	m.fixLogScroll()
	if moved {
		return m.selectTarget()
	}
	return m, nil
}

// setFile põe o cursor do Navegador no arquivo i e mostra o diff dele.
func (m model) setFile(i int) (tea.Model, tea.Cmd) {
	m.navOpen, m.navFile = true, true
	prev := m.fileIdx
	m.fileIdx = min(max(i, 0), len(m.files)-1)
	m.fixLogScroll()
	if m.fileIdx != prev || m.vtab != vtDiff {
		m.vtab = vtDiff
		return m, m.previewSelected()
	}
	return m, nil
}

// updateLogKeys: teclas do Navegador. ↑↓ andam por commits e, com o commit aberto, pelos
// arquivos dele; → abre e ← fecha; enter num arquivo foca o diff.
func (m model) updateLogKeys(key string) (tea.Model, tea.Cmd) {
	if m.navProject {
		return m.updateProjectKeys(key)
	}
	if key == "p" {
		return m.toggleProject()
	}
	ready := m.filesFor == m.target && len(m.files) > 0
	switch key {
	case "esc":
		if m.navFile {
			m.navFile = false
			m.fixLogScroll()
			return m, nil
		}
		return m.toRepoScreen()
	case "down", "j":
		switch {
		case m.navFile && m.fileIdx < len(m.files)-1:
			return m.setFile(m.fileIdx + 1)
		case !m.navFile && m.navOpen && ready:
			return m.setFile(m.fileIdx)
		}
		return m.moveCommit(1, 1)
	case "up", "k":
		switch {
		case m.navFile && m.fileIdx > 0:
			return m.setFile(m.fileIdx - 1)
		case m.navFile:
			m.navFile = false
			m.fixLogScroll()
			return m, nil
		}
		return m.moveCommit(-1, 1)
	case "right", "l":
		if !m.navOpen {
			m.navOpen = true
			m.fixLogScroll()
			return m, nil
		}
		if !m.navFile && ready {
			return m.setFile(m.fileIdx)
		}
	case "left", "h":
		if m.navFile {
			m.navFile = false
		} else {
			m.navOpen = false
		}
		m.fixLogScroll()
	case "enter":
		if m.navFile {
			return m.focusPane(pDiff)
		}
		if ready {
			return m.setFile(m.fileIdx)
		}
	case "J", "K": // próximo / anterior arquivo, sem passar para outro commit
		if ready {
			if key == "J" {
				return m.setFile(m.fileIdx + 1)
			}
			return m.setFile(m.fileIdx - 1)
		}
	case "pgup":
		return m.moveCommit(-1, max(m.logHeight()/2, 1))
	case "pgdown":
		return m.moveCommit(1, max(m.logHeight()/2, 1))
	case "home", "g":
		return m.moveCommit(-1, len(m.rows))
	case "end", "G":
		return m.moveCommit(1, len(m.rows))
	case "c": // nas mudanças locais: planejar os commits
		if m.target == repo.WorkTree {
			return m.startCommitPlan()
		}
		if _, _, ok := repo.SplitRange(m.target); ok {
			return m.startPRDescription()
		}
	case "r":
		if m.navFile && ready {
			return m.quickReview(m.files[m.fileIdx].Path)
		}
		return m.quickReview("")
	case "v": // o arquivo inteiro
		if ready {
			return m.openFileTab(m.files[m.fileIdx].Path, repo.Tip(m.target), 0)
		}
	case "x": // o que é este arquivo?
		if ready && m.navFile {
			return m.explainFile(m.files[m.fileIdx].Path, repo.Tip(m.target))
		}
	case "o", "e":
		if ready {
			f := m.files[m.fileIdx]
			if f.Status == "D" {
				m.setErr("o arquivo foi removido neste alvo")
				return m, nil
			}
			return m, openInEditor(m.repo.Root, f.Path, firstHunkLine(m.diffRaw))
		}
	case "d", "[", "]":
		// Volta do documento aberto pelo modelo (ou do mapa) para o diff.
		m.vtab = vtDiff
		m.renderPreview()
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
	if base, tip, ok := repo.SplitRange(m.target); ok {
		return fmt.Sprintf("branch %s × %s (como um PR)", tip, base)
	}
	for _, r := range m.rows {
		if r.c.SHA == m.target {
			return r.c.Short + " " + r.c.Subject
		}
	}
	return m.target
}
