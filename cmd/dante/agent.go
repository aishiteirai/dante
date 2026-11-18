package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Agente que lê ou edita. As ferramentas de escrita são oferecidas em toda conversa (a lista
// de ferramentas não pode mudar no meio dela); no modo ler, elas recusam e a proposta vira
// só uma sugestão no chat. No modo editar, cada ação segue a permissão do config.json:
// perguntar (cartão de aprovação), permitir (aplica direto) ou negar. Só o desenvolvedor
// troca o modo.

type agentMode int

const (
	agentLer agentMode = iota
	agentEditar
)

var agentNames = map[agentMode]string{agentLer: "ler", agentEditar: "editar"}

// Ferramenta de escrita → ação (a chave da permissão).
var writeTools = map[string]repo.WriteOp{"edit_file": repo.OpEdit, "write_file": repo.OpCreate, "delete_file": repo.OpDelete}

type changeState int

const (
	chPending   changeState = iota // aguardando aprovação
	chApplied                      // aplicada
	chEdited                       // aplicada depois de ajustada no editor
	chDeclined                     // recusada pelo desenvolvedor
	chDenied                       // negada pela permissão
	chSuggested                    // modo ler: só sugestão
	chFailed                       // não deu para planejar ou aplicar
	chReverted                     // desfeita com ctrl+z
)

type changeCard struct {
	id    int
	c     repo.Change
	diff  string // diff unificado, com cabeçalho do arquivo
	state changeState
	note  string
	auto  bool // aplicada sem perguntar
}

// changeMsg leva um cartão do executor à interface; com reply, é um pedido de aprovação.
type changeMsg struct {
	card  changeCard
	reply chan approval
}

type approval struct {
	ok, all bool
	edited  []byte // conteúdo ajustado no editor (nil = como proposto)
}

type undoGroup struct {
	changes []repo.Change
	ids     []int
}

// writeBus é o estado compartilhado entre a interface e o executor, que roda na goroutine
// da resposta.
type writeBus struct {
	mu      sync.Mutex
	mode    agentMode
	perms   map[string]string
	auto    bool // "t": aplica as próximas desta resposta sem perguntar
	out     chan tea.Msg
	applied undoGroup
	next    int
}

func newWriteBus(cfg config.Config) *writeBus {
	b := &writeBus{}
	b.setPerms(cfg)
	return b
}

func (b *writeBus) setPerms(cfg config.Config) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.perms = map[string]string{}
	for _, op := range config.PermissionOps {
		b.perms[op] = cfg.Permission(op)
	}
}

func (b *writeBus) setMode(a agentMode) {
	b.mu.Lock()
	b.mode = a
	b.mu.Unlock()
}

// startTurn prepara uma nova resposta.
func (b *writeBus) startTurn(out chan tea.Msg) {
	b.mu.Lock()
	b.out, b.auto, b.applied = out, false, undoGroup{}
	b.mu.Unlock()
}

// endTurn devolve as mudanças aplicadas na resposta.
func (b *writeBus) endTurn() undoGroup {
	b.mu.Lock()
	defer b.mu.Unlock()
	g := b.applied
	b.out, b.auto, b.applied = nil, false, undoGroup{}
	return g
}

func (b *writeBus) emit(ctx context.Context, out chan tea.Msg, msg tea.Msg) bool {
	if out == nil {
		return false
	}
	select {
	case out <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}

func planCall(r repo.Repo, call llm.ToolCall) (repo.Change, string, error) {
	var a struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal([]byte(call.Args), &a); err != nil {
		return repo.Change{}, "", fmt.Errorf("argumentos inválidos (JSON): %v", err)
	}
	var c repo.Change
	var err error
	switch call.Name {
	case "edit_file":
		c, err = r.PlanEdit(a.Path, a.OldString, a.NewString, a.ReplaceAll)
	case "write_file":
		c, err = r.PlanCreate(a.Path, a.Content)
	case "delete_file":
		c, err = r.PlanDelete(a.Path)
	}
	return c, a.Path, err
}

// wrap envolve o executor: as ferramentas de escrita passam pelo modo, pela permissão e
// pela aprovação; as outras seguem direto.
func (b *writeBus) wrap(r repo.Repo, next llm.Executor) llm.Executor {
	return func(ctx context.Context, call llm.ToolCall) (string, bool) {
		op, ok := writeTools[call.Name]
		if !ok {
			return next(ctx, call)
		}
		b.mu.Lock()
		b.next++
		id, mode, perm, auto, out := b.next, b.mode, b.perms[string(op)], b.auto, b.out
		b.mu.Unlock()

		c, path, err := planCall(r, call)
		card := changeCard{id: id, c: c}
		if err != nil {
			card.c = repo.Change{Op: op, Path: path}
			card.state, card.note = chFailed, err.Error()
			b.emit(ctx, out, changeMsg{card: card})
			return err.Error(), true
		}
		card.diff = changeDiff(c)
		switch {
		case mode == agentLer:
			card.state = chSuggested
			b.emit(ctx, out, changeMsg{card: card})
			return "Modo ler: você não altera arquivos, e nada foi feito. A proposta foi mostrada ao desenvolvedor " +
				"como sugestão; continue a resposta mostrando o trecho proposto no texto. Só ele pode passar para o " +
				"modo editar (/editar).", true
		case perm == config.PermDeny:
			card.state = chDenied
			b.emit(ctx, out, changeMsg{card: card})
			return fmt.Sprintf("A permissão para %s arquivos está como negar no dante; nada foi feito. Mostre a "+
				"proposta no texto, se ainda fizer sentido.", op), true
		}
		edited := false
		if perm != config.PermAllow && !auto {
			reply := make(chan approval, 1)
			card.state = chPending
			if !b.emit(ctx, out, changeMsg{card: card, reply: reply}) {
				return "Resposta cancelada; nada foi feito.", true
			}
			var a approval
			select {
			case a = <-reply:
			case <-ctx.Done():
				return "Resposta cancelada; nada foi feito.", true
			}
			if !a.ok {
				card.state = chDeclined
				b.emit(ctx, out, changeMsg{card: card})
				return fmt.Sprintf("O desenvolvedor recusou a mudança em %s; nada foi feito. Não insista na mesma "+
					"mudança: siga com o resto ou pergunte o que ele prefere.", c.Path), true
			}
			if a.all {
				b.mu.Lock()
				b.auto = true
				b.mu.Unlock()
			}
			if a.edited != nil {
				c.After, edited = a.edited, true
				card.c, card.diff = c, changeDiff(c)
			}
		} else {
			card.auto = true
		}
		if err := r.Apply(c); err != nil {
			card.state, card.note = chFailed, err.Error()
			b.emit(ctx, out, changeMsg{card: card})
			return err.Error(), true
		}
		b.mu.Lock()
		b.applied.changes = append(b.applied.changes, c)
		b.applied.ids = append(b.applied.ids, id)
		b.mu.Unlock()
		card.state = chApplied
		msg := fmt.Sprintf("Mudança aplicada: %s %s.", op, c.Path)
		if edited {
			card.state = chEdited
			msg = fmt.Sprintf("O desenvolvedor ajustou a mudança no editor antes de aplicar, então %s está diferente "+
				"da sua proposta: leia o arquivo de novo antes de outra edição nele.", c.Path)
		}
		b.emit(ctx, out, changeMsg{card: card})
		return msg, false
	}
}

// changeDiff é o diff da mudança com o caminho do repositório no cabeçalho.
func changeDiff(c repo.Change) string {
	raw := c.Diff()
	i := strings.Index(raw, "\n@@")
	if i < 0 {
		return ""
	}
	a, b := "a/"+c.Path, "b/"+c.Path
	if c.Before == nil {
		a = "/dev/null"
	}
	if c.After == nil {
		b = "/dev/null"
	}
	return fmt.Sprintf("diff --git a/%s b/%s\n--- %s\n+++ %s%s", c.Path, c.Path, a, b, raw[i:])
}

// diffCounts conta as linhas adicionadas e removidas.
func diffCounts(d string) (add, del int) {
	for _, l := range strings.Split(d, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			add++
		case strings.HasPrefix(l, "-"):
			del++
		}
	}
	return
}

// ---- interface ----

func (m *model) writes() *writeBus {
	if m.bus == nil {
		m.bus = newWriteBus(m.cfg)
		m.bus.setMode(m.agent)
	}
	return m.bus
}

func (m *model) setAgent(a agentMode) {
	m.agent = a
	m.writes().setMode(a)
}

// onChange recebe um cartão do executor.
func (m model) onChange(msg changeMsg) (tea.Model, tea.Cmd) {
	follow := m.chatView.AtBottom()
	card := msg.card
	found := false
	for i := range m.entries {
		if m.entries[i].kind == eChange && m.entries[i].chg.id == card.id {
			c := card
			m.entries[i].chg, found = &c, true
		}
	}
	if !found {
		c := card
		m.entries = append(m.entries, entry{kind: eChange, chg: &c})
	}
	cmds := []tea.Cmd{waitFor(m.events)}
	switch {
	case msg.reply != nil:
		m.approval = &pendingApproval{id: card.id, c: card.c, reply: msg.reply}
		m.showChange(card, "proposta")
		m.setInfo("o modelo quer " + string(card.c.Op) + " " + card.c.Path + ": s aplica · n recusa · t todas · e ajusta")
	case card.state == chSuggested:
		m.showChange(card, "sugestão (modo ler)")
	case card.state == chApplied || card.state == chEdited:
		m.showChange(card, "aplicada")
		cmds = append(cmds, loadLog(m.repo), m.reloadFilesIfWorktree())
	}
	m.refreshChat()
	if follow || msg.reply != nil {
		m.chatView.GotoBottom()
	}
	return m, tea.Batch(cmds...)
}

func (m *model) showChange(card changeCard, what string) {
	if card.diff == "" {
		return
	}
	if m.lay.isMinimized(pDiff) {
		m.lay.restore(pDiff)
		m.layoutChanged()
	}
	m.previewKey = fmt.Sprintf("change\x00%d", card.id)
	m.setDoc(fmt.Sprintf("%s · %s %s", card.c.Path, string(card.c.Op), what), card.diff, true)
}

type pendingApproval struct {
	id      int
	c       repo.Change
	reply   chan approval
	editing string // arquivo temporário aberto no editor
}

type approvalEditedMsg struct {
	tmp string
	err error
}

// updateApprovalKeys trata as teclas do cartão de aprovação; ok=false deixa a tecla seguir.
func (m model) updateApprovalKeys(key string) (tea.Model, tea.Cmd, bool) {
	a := m.approval
	if a.editing != "" {
		return m, nil, false
	}
	switch key {
	case "s", "y", "S", "Y":
		return m.answer(approval{ok: true}, "mudança aplicada"), nil, true
	case "t", "T":
		return m.answer(approval{ok: true, all: true}, "aplicando as mudanças desta resposta sem perguntar"), nil, true
	case "n", "N", "esc":
		return m.answer(approval{}, "mudança recusada"), nil, true
	case "e", "E":
		if a.c.After == nil {
			m.setInfo("não há conteúdo para ajustar numa exclusão")
			return m, nil, true
		}
		dir, err := os.MkdirTemp("", "dante-ajuste-")
		if err != nil {
			m.setErr(err.Error())
			return m, nil, true
		}
		tmp := filepath.Join(dir, filepath.Base(a.c.Path))
		if err := os.WriteFile(tmp, a.c.After, 0o600); err != nil {
			m.setErr(err.Error())
			return m, nil, true
		}
		a.editing = tmp
		return m, editTemp(tmp), true
	}
	return m, nil, false
}

func editTemp(tmp string) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	parts := editorCommand(editor, runtime.GOOS, tmp, 1)
	c := exec.Command(parts[0], parts[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return approvalEditedMsg{tmp: tmp, err: err} })
}

func (m model) onApprovalEdited(msg approvalEditedMsg) (tea.Model, tea.Cmd) {
	defer os.RemoveAll(filepath.Dir(msg.tmp))
	a := m.approval
	if a == nil || a.editing != msg.tmp {
		return m, nil
	}
	a.editing = ""
	if msg.err != nil {
		m.setErr("editor: " + msg.err.Error() + " · a proposta continua esperando")
		return m, nil
	}
	b, err := os.ReadFile(msg.tmp)
	if err != nil {
		m.setErr("não consegui ler o ajuste: " + err.Error())
		return m, nil
	}
	if bytes.Equal(b, a.c.After) { // salvo sem mudar: vale como proposto
		return m.answer(approval{ok: true}, "mudança aplicada como proposta"), nil
	}
	return m.answer(approval{ok: true, edited: b}, "mudança ajustada e aplicada"), nil
}

func (m model) answer(a approval, info string) model {
	if m.approval == nil {
		return m
	}
	m.approval.reply <- a
	m.approval = nil
	m.setInfo(info)
	return m
}

// undoLast desfaz as mudanças da última resposta que mudou arquivos.
func (m model) undoLast() (tea.Model, tea.Cmd) {
	if m.streaming {
		m.setInfo("aguarde a resposta terminar para desfazer")
		return m, nil
	}
	if len(m.undo) == 0 {
		m.setInfo("nada para desfazer")
		return m, nil
	}
	g := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	var problems []string
	done := 0
	for i := len(g.changes) - 1; i >= 0; i-- {
		if err := m.repo.Revert(g.changes[i]); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		done++
		for j := range m.entries {
			if m.entries[j].kind == eChange && m.entries[j].chg.id == g.ids[i] {
				c := *m.entries[j].chg
				c.state = chReverted
				m.entries[j].chg = &c
			}
		}
	}
	text := "↶ desfeita " + plural(done, "mudança", "mudanças") + " da última resposta"
	if done != 1 {
		text = "↶ desfeitas " + plural(done, "mudança", "mudanças") + " da última resposta"
	}
	m.entries = append(m.entries, entry{kind: eNotice, text: text})
	for _, p := range problems {
		m.entries = append(m.entries, entry{kind: eError, text: p})
	}
	if len(problems) > 0 {
		m.setErr(fmt.Sprintf("%d mudança(s) não foram desfeitas: o arquivo mudou depois", len(problems)))
	} else {
		m.setInfo(text)
	}
	m.refreshChat()
	m.chatView.GotoBottom()
	return m, tea.Batch(loadLog(m.repo), m.reloadFilesIfWorktree())
}

// ---- desenho ----

var opLabel = map[repo.WriteOp]string{repo.OpEdit: "✎ EDITAR", repo.OpCreate: "+ CRIAR", repo.OpDelete: "✕ APAGAR"}

var stateLabel = map[changeState]string{
	chPending: "aguardando você", chApplied: "aplicada", chEdited: "ajustada e aplicada", chDeclined: "recusada",
	chDenied: "negada pela permissão", chSuggested: "sugestão · modo ler", chFailed: "não aplicada", chReverted: "desfeita",
}

func stateColor(s changeState) lipgloss.TerminalColor {
	switch s {
	case chApplied, chEdited:
		return okColor
	case chPending:
		return warnCol
	case chDeclined, chDenied, chFailed:
		return errColor
	}
	return faint
}

const miniDiffLines = 8

func renderChange(c changeCard, w int) string {
	col := stateColor(c.state)
	bar := lipgloss.NewStyle().Foreground(col).Render("▌")
	inner := max(w-2, 10)
	head := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(opLabel[c.c.Op]) + " " + boldStyle.Render(c.c.Path)
	if c.diff != "" {
		a, d := diffCounts(c.diff)
		head += " " + okStyle.Render(fmt.Sprintf("+%d", a)) + " " + errStyle.Render(fmt.Sprintf("-%d", d))
	}
	st := stateLabel[c.state]
	if c.auto && (c.state == chApplied) {
		st = "aplicada sem perguntar"
	}
	state := lipgloss.NewStyle().Foreground(col).Render(st)
	if gap := inner - lipgloss.Width(head) - lipgloss.Width(state); gap >= 1 {
		head += strings.Repeat(" ", gap) + state
	} else {
		head += "\n" + state
	}
	body := []string{head}
	if c.note != "" {
		body = append(body, lipgloss.NewStyle().Width(inner).Render(subtleStyle.Render(c.note)))
	}
	if c.diff != "" && c.state != chFailed {
		body = append(body, miniDiff(c.diff, inner)...)
	}
	switch c.state {
	case chPending:
		body = append(body, warnStyle.Width(inner).Render("s aplicar · n recusar · t aplicar todas desta resposta · e ajustar no editor"))
	case chSuggested:
		body = append(body, subtleStyle.Width(inner).Render("/editar deixa o modelo aplicar mudanças (com a sua aprovação)"))
	}
	var out []string
	for _, l := range strings.Split(strings.Join(body, "\n"), "\n") {
		out = append(out, bar+" "+l)
	}
	return strings.Join(out, "\n")
}

// miniDiff mostra as primeiras linhas alteradas da mudança.
func miniDiff(d string, w int) []string {
	var out []string
	more := 0
	for _, l := range strings.Split(d, "\n") {
		if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") {
			continue
		}
		var st lipgloss.Style
		switch {
		case strings.HasPrefix(l, "+"):
			st = okStyle
		case strings.HasPrefix(l, "-"):
			st = errStyle
		default:
			continue
		}
		if len(out) >= miniDiffLines {
			more++
			continue
		}
		out = append(out, st.Render(truncateW(strings.ReplaceAll(l, "\t", "    "), w)))
	}
	if more > 0 {
		out = append(out, subtleStyle.Render(fmt.Sprintf("… mais %d linhas no Visualizador", more)))
	}
	return out
}

func changeMarkdown(c changeCard) string {
	return fmt.Sprintf("> %s `%s` — %s\n\n```diff\n%s\n```\n", opLabel[c.c.Op], c.c.Path, stateLabel[c.state], strings.TrimRight(c.diff, "\n"))
}

// agentChip: "◉ ler" ou "✎ editar · perguntar".
func (m model) agentChip() string {
	if m.agent == agentLer {
		return lipgloss.NewStyle().Foreground(faint).Render("◉ ler")
	}
	return lipgloss.NewStyle().Foreground(warnCol).Bold(true).Render("✎ editar · " + m.cfg.Permission("editar"))
}

// agentBlock é a linha de permissão dentro do bloco <modo>.
func (m model) agentBlock() string {
	if m.agent == agentLer {
		return "Permissão: ler. Você não altera arquivos (edit_file, write_file e delete_file recusam). Ao sugerir " +
			"correções, mostre o trecho proposto no texto.\n"
	}
	return fmt.Sprintf("Permissão: editar (editar arquivos: %s; criar: %s; apagar: %s). Quando o pedido for mudar o "+
		"código, use edit_file, write_file e delete_file, com mudanças pequenas e focadas no que foi pedido. Antes de "+
		"editar, leia o trecho atual no disco (read_file sem ref). Cada mudança pode precisar da aprovação do "+
		"desenvolvedor; uma recusa vale para aquela mudança. No fim, resuma o que mudou, com arquivo:linha.\n",
		m.cfg.Permission("editar"), m.cfg.Permission("criar"), m.cfg.Permission("apagar"))
}

// ---- painel de permissões ----

var permCycle = []string{config.PermAsk, config.PermAllow, config.PermDeny}

func (m model) updatePermKeys(key string) (tea.Model, tea.Cmd) {
	ops := config.PermissionOps
	switch key {
	case "esc", "q", "enter":
		m.mode = modeNormal
	case "up", "k":
		m.permIdx = max(m.permIdx-1, 0)
	case "down", "j":
		m.permIdx = min(m.permIdx+1, len(ops)-1)
	case "left", "h", "right", "l", " ":
		dir := 1
		if key == "left" || key == "h" {
			dir = -1
		}
		op := ops[m.permIdx]
		cur := 0
		for i, v := range permCycle {
			if v == m.cfg.Permission(op) {
				cur = i
			}
		}
		m.cfg.SetPermission(op, permCycle[(cur+dir+len(permCycle))%len(permCycle)])
		_ = m.cfg.Save()
		m.writes().setPerms(m.cfg)
	case "e":
		m.setAgent(agentEditar)
	case "r":
		m.setAgent(agentLer)
	}
	return m, nil
}

func (m model) permsPopup() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("permissões do agente") + "\n\n")
	modeLine := "modo: " + selectedStyle.Render(agentNames[m.agent])
	b.WriteString(modeLine + subtleStyle.Render("   (r ler · e editar)") + "\n\n")
	for i, op := range config.PermissionOps {
		v := m.cfg.Permission(op)
		var vs lipgloss.Style
		switch v {
		case config.PermAllow:
			vs = okStyle
		case config.PermDeny:
			vs = errStyle
		default:
			vs = warnStyle
		}
		line := padTo(op+" arquivos", 16) + " ‹ " + vs.Render(padTo(v, 9)) + " ›"
		if i == m.permIdx {
			b.WriteString(selectedStyle.Render("▸ ") + line + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n" + subtleStyle.Render("Valem no modo editar. No modo ler, nada é alterado.") + "\n")
	b.WriteString(subtleStyle.Render("Nunca: arquivos sensíveis, a pasta .git, fora do repositório.") + "\n\n")
	b.WriteString(subtleStyle.Render("↑↓ escolhe · ←→ muda · esc fecha"))
	return boxStyle.Padding(1, 2).Render(b.String())
}
