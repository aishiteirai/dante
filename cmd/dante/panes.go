package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Modos da área de trabalho (como no noct: prefixo abre os comandos; mover e
// redimensionar usam as setas até esc/enter).
const (
	modeNormal = iota
	modePrefix
	modeMove
	modeResize
	modeRefs    // lista de referências (ctrl+g)
	modePerms   // permissões do agente (/permissoes ou ctrl+space p)
	modeMap     // confirmação do /mapa
	modeCommits // plano de commits (/commits)
	modeChecks  // permissão para rodar as verificações do projeto
)

const resizeStep = 0.03

func isPrefixKey(k string) bool { return k == "ctrl+@" || k == "ctrl+ " || k == "ctrl+space" }

// Direção de uma tecla de seta ou hjkl.
func arrow(k string) (dx, dy int, ok bool) {
	switch k {
	case "left", "h":
		return -1, 0, true
	case "right", "l":
		return 1, 0, true
	case "up", "k":
		return 0, -1, true
	case "down", "j":
		return 0, 1, true
	}
	return 0, 0, false
}

// focusPane dá foco ao painel: restaura se estava minimizado e abre se estava dobrado.
func (m model) focusPane(p paneID) (tea.Model, tea.Cmd) {
	if m.lay.isMinimized(p) {
		m.lay.restore(p)
		m.lay.save()
	}
	if ci, _ := m.lay.find(p); ci >= 0 {
		m.lay.Cols[ci].Active = p
	}
	m.focus = p
	m.relayout()
	if p == pChat {
		return m, m.input.Focus()
	}
	m.input.Blur()
	return m, nil
}

// setFocus mantém o nome usado no resto do código.
func (m model) setFocus(p paneID) (tea.Model, tea.Cmd) { return m.focusPane(p) }

func (m model) focusDir(dx, dy int) (tea.Model, tea.Cmd) {
	// Navega pelo layout real, mesmo em tela cheia.
	nav := m.lay.compute(0, 1, m.width, m.areaHeight(), m.focus, false)
	if p, ok := neighbor(nav, m.focus, dx, dy); ok {
		return m.focusPane(p)
	}
	return m, nil
}

func (m model) cycleFocus(dir int) (tea.Model, tea.Cmd) {
	vs := m.lay.visiblePanes()
	for i, p := range vs {
		if p == m.focus {
			return m.focusPane(vs[(i+dir+len(vs))%len(vs)])
		}
	}
	if len(vs) > 0 {
		return m.focusPane(vs[0])
	}
	return m, nil
}

func (m *model) layoutChanged() {
	m.lay.save()
	m.relayout()
}

// command roda um comando de layout/app; ok=false se a tecla não é um comando.
func (m model) command(k string) (tea.Model, tea.Cmd, bool) {
	if dx, dy, ok := arrow(k); ok {
		nm, cmd := m.focusDir(dx, dy)
		return nm, cmd, true
	}
	switch k {
	case "a":
		m.mode = modeMove
	case "n":
		m.mode = modeResize
	case "w":
		if !m.lay.toggleStack(m.focus) {
			m.setInfo("só dá para empilhar uma coluna com mais de um painel")
		}
		m.layoutChanged()
	case "f":
		m.zoom = !m.zoom
		m.relayout()
	case "z":
		if !m.lay.minimize(m.focus) {
			m.setInfo("o último painel não pode ser minimizado")
			return m, nil, true
		}
		m.zoom = false
		m.layoutChanged()
		nm, cmd := m.focusPane(m.lay.visiblePanes()[0])
		return nm, cmd, true
	case "1", "2", "3":
		nm, cmd := m.focusPane(paneID(k[0] - '1'))
		return nm, cmd, true
	case "0":
		m.lay, m.zoom = defaultLayout(), false
		m.layoutChanged()
		m.setInfo("layout padrão")
	case "m":
		m.mode = modeNormal
		m.screen = scrModel
		return m, tea.Batch(m.enterModelScreen()...), true
	case "o":
		m.mode = modeNormal
		nm, cmd := m.toRepoScreen()
		return nm, cmd, true
	case "u":
		return m, tea.Batch(loadLog(m.repo), m.reloadFilesIfWorktree()), true
	case "p":
		m.mode, m.permIdx = modePerms, 0
	case "q":
		return m, tea.Quit, true
	default:
		return m, nil, false
	}
	return m, nil, true
}

func (m model) moveOrResize(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "enter", "q":
		m.mode = modeNormal
		return m, nil
	}
	dx, dy, ok := arrow(k)
	if !ok {
		return m, nil
	}
	if m.mode == modeMove {
		if dx != 0 {
			m.lay.moveH(m.focus, dx)
		} else {
			m.lay.moveV(m.focus, dy)
		}
		m.zoom = false
	} else {
		if dx != 0 {
			m.lay.resizeH(m.focus, float64(dx)*resizeStep)
		} else {
			m.lay.resizeV(m.focus, float64(dy)*resizeStep)
		}
	}
	m.layoutChanged()
	return m, nil
}

func (m model) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		return m.onMouse(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	if m.focus == pChat {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()

	// A busca e o "ir para a linha" da aba Arquivo capturam todas as teclas.
	if m.fileInput != "" && m.focus == pDiff {
		return m.updateFileInput(k)
	}
	// Uma mudança esperando aprovação captura s/n/t/e/esc, de qualquer painel.
	if m.approval != nil && m.mode == modeNormal {
		if nm, cmd, ok := m.updateApprovalKeys(key); ok {
			return nm, cmd
		}
	}

	switch m.mode {
	case modePrefix:
		if key == "esc" || isPrefixKey(key) {
			m.mode = modeNormal
			return m, nil
		}
		nm, cmd, ok := m.command(key)
		mm := nm.(model)
		// Navegação mantém o modo ligado; os outros comandos o encerram.
		if _, _, isArrow := arrow(key); !isArrow && mm.mode == modePrefix {
			mm.mode = modeNormal
		}
		if !ok {
			mm.mode = modeNormal
		}
		return mm, cmd
	case modeMove, modeResize:
		return m.moveOrResize(key)
	case modeRefs:
		return m.updateRefKeys(key)
	case modePerms:
		return m.updatePermKeys(key)
	case modeMap:
		return m.updateMapKeys(key)
	case modeCommits:
		return m.updateCommitKeys(key)
	case modeChecks:
		return m.updateChecksConfirm(key)
	}

	if key == "ctrl+z" { // desfaz as mudanças da última resposta
		return m.undoLast()
	}

	if key == "ctrl+o" { // detalhes das leituras do modelo no chat
		m.showActivity = !m.showActivity
		m.refreshChat()
		return m, nil
	}
	if key == "ctrl+g" { // referências do chat, de qualquer painel
		m.mode = modeRefs
		m.refIdx = max(len(m.refs)-1, 0)
		m.refDigits = ""
		return m, nil
	}

	if isPrefixKey(key) {
		m.mode = modePrefix
		m.status = ""
		return m, nil
	}

	// Atalhos diretos com Alt (funcionam até digitando no chat).
	if strings.HasPrefix(key, "alt+shift+") {
		if dx, dy, ok := arrow(strings.TrimPrefix(key, "alt+shift+")); ok {
			if dx != 0 {
				m.lay.moveH(m.focus, dx)
			} else {
				m.lay.moveV(m.focus, dy)
			}
			m.zoom = false
			m.layoutChanged()
			return m, nil
		}
	}
	if strings.HasPrefix(key, "alt+") && key != "alt+enter" {
		if nm, cmd, ok := m.command(strings.TrimPrefix(key, "alt+")); ok {
			return nm, cmd
		}
	}

	if key == "tab" && m.focus == pChat {
		if sugg := commandSuggestions(m.input.Value()); len(sugg) > 0 {
			m.input.SetValue(sugg[0].name + " ")
			m.input.CursorEnd()
			return m, nil
		}
	}
	switch key {
	case "tab":
		return m.cycleFocus(1)
	case "shift+tab":
		return m.cycleFocus(-1)
	}

	if m.focus == pChat {
		return m.updateChatKeys(k)
	}
	m.status = ""

	switch key {
	case "q":
		return m, tea.Quit
	case "1", "2", "3", "m", "u":
		nm, cmd, _ := m.command(key)
		return nm, cmd
	}
	switch m.focus {
	case pLog:
		return m.updateLogKeys(key)
	case pDiff:
		return m.updateDiffKeys(key)
	}
	return m, nil
}

// ---- mouse: clique dá foco, roda rola, arrastar a borda redimensiona ----

type dragState struct {
	vertical bool   // borda entre colunas (true) ou entre painéis de uma coluna
	a, b     paneID // painel antes e depois da borda
}

func (m model) paneAt(x, y int) (paneID, rect, bool) {
	for p, r := range m.rects {
		if r.contains(x, y) {
			return p, r, true
		}
	}
	return 0, rect{}, false
}

// borderAt acha a borda entre dois painéis sob o mouse.
func (m model) borderAt(x, y int) (dragState, bool) {
	if m.zoomed() {
		return dragState{}, false
	}
	for pa, a := range m.rects {
		for pb, b := range m.rects {
			if pa == pb {
				continue
			}
			// a à esquerda de b, lado a lado.
			if a.x+a.w == b.x && (x == a.x+a.w-1 || x == b.x) && y >= max(a.y, b.y) && y < min(a.y+a.h, b.y+b.h) {
				return dragState{vertical: true, a: pa, b: pb}, true
			}
			// a em cima de b, na mesma coluna (pilhas não se redimensionam).
			if a.y+a.h == b.y && a.x == b.x && a.h > 2 && b.h > 2 && (y == a.y+a.h-1 || y == b.y) && x > a.x && x < a.x+a.w-1 {
				return dragState{vertical: false, a: pa, b: pb}, true
			}
		}
	}
	return dragState{}, false
}

func (m model) onMouse(ev tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode == modePrefix {
		m.mode = modeNormal
	}
	switch {
	case ev.Action == tea.MouseActionRelease:
		if m.drag != nil {
			m.drag = nil
			m.lay.save()
		}
		return m, nil

	case ev.Action == tea.MouseActionMotion && m.drag != nil:
		m.dragTo(ev.X, ev.Y)
		m.relayout()
		return m, nil

	case ev.Action == tea.MouseActionPress && ev.Button == tea.MouseButtonLeft:
		if d, ok := m.borderAt(ev.X, ev.Y); ok {
			m.drag = &d
			return m, nil
		}
		if p, _, ok := m.paneAt(ev.X, ev.Y); ok {
			if p == pDiff {
				m.clickLine(ev.Y) // marca a linha (f explica a função dela)
			}
			if p != m.focus {
				return m.focusPane(p)
			}
		}

	case ev.Button == tea.MouseButtonWheelUp || ev.Button == tea.MouseButtonWheelDown:
		p, _, ok := m.paneAt(ev.X, ev.Y)
		if !ok {
			return m, nil
		}
		dir := 1
		if ev.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		return m.wheel(p, dir)
	}
	return m, nil
}

func (m model) wheel(p paneID, dir int) (tea.Model, tea.Cmd) {
	switch p {
	case pLog:
		if i := m.nextSelectable(m.logIdx, dir); i >= 0 {
			m.logIdx = i
			m.fixLogScroll()
			return m.selectTarget()
		}
	case pDiff:
		if dir < 0 {
			m.preview.ScrollUp(3)
		} else {
			m.preview.ScrollDown(3)
		}
	case pChat:
		if dir < 0 {
			m.chatView.ScrollUp(3)
		} else {
			m.chatView.ScrollDown(3)
		}
	}
	return m, nil
}

// dragTo move a borda arrastada até (x, y), dividindo de novo o espaço dos dois lados.
func (m *model) dragTo(x, y int) {
	d := m.drag
	ra, rb := m.rects[d.a], m.rects[d.b]
	if d.vertical {
		ca, _ := m.lay.find(d.a)
		cb, _ := m.lay.find(d.b)
		if ca < 0 || cb < 0 {
			return
		}
		pair := ra.w + rb.w
		na := min(max(x-ra.x+1, minColWidth), pair-minColWidth)
		total := m.lay.Cols[ca].Width + m.lay.Cols[cb].Width
		m.lay.Cols[ca].Width = total * float64(na) / float64(pair)
		m.lay.Cols[cb].Width = total - m.lay.Cols[ca].Width
		return
	}
	ci, ia := m.lay.find(d.a)
	_, ib := m.lay.find(d.b)
	if ci < 0 {
		return
	}
	c := &m.lay.Cols[ci]
	pair := ra.h + rb.h
	na := min(max(y-ra.y+1, minPaneRows), pair-minPaneRows)
	total := c.Sizes[ia] + c.Sizes[ib]
	c.Sizes[ia] = total * float64(na) / float64(pair)
	c.Sizes[ib] = total - c.Sizes[ia]
}
