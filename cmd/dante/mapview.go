package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/mapa"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Mapa do projeto (/mapa) e tour (/tour). O mapa só é gerado quando pedido, depois de
// mostrar a estimativa de tokens e deixar escolher um modelo barato. Fica salvo por
// repositório, aparece na aba Mapa do Visualizador e, em versão compacta, vai como
// contexto no começo de cada conversa.

func mapPath(root string) string {
	h := sha1.Sum([]byte(root))
	return filepath.Join(dataHome(), "mapas", repoDisplayName(root)+"-"+hex.EncodeToString(h[:4])+".json")
}

const compactMapBytes = 12000

type mapChoice struct {
	prov  config.Provider
	model string
	note  string
}

// mapChoices: o último modelo usado no mapa, os modelos baratos dos provedores com chave,
// o modelo da conversa e os modelos locais (grátis, mais lentos).
func (m model) mapChoices() []mapChoice {
	var out []mapChoice
	seen := map[string]bool{}
	add := func(p config.Provider, model, note string) {
		k := p.ID + "/" + model
		if model == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, mapChoice{p, model, note})
	}
	noteFor := func(p config.Provider, model string) string {
		switch {
		case p.Local:
			return p.Nome + " · grátis, mais lento"
		case model == p.ModeloMapa:
			return p.Nome + " · barato"
		case p.ID == m.prov.ID && model == m.modelName:
			return p.Nome + " · modelo da conversa"
		}
		return p.Nome
	}
	if c := m.cfg.MapaModelo; c != nil {
		if p, ok := m.cfg.Provider(c.Provedor); ok && p.HasKey() {
			add(p, c.Modelo, noteFor(p, c.Modelo)+" · último usado")
		}
	}
	for _, p := range m.cfg.Provedores {
		if !p.Local && p.ModeloMapa != "" && p.KeySource() != "" {
			add(p, p.ModeloMapa, noteFor(p, p.ModeloMapa))
		}
	}
	if m.modelName != "" {
		add(m.prov, m.modelName, m.prov.Nome+" · modelo da conversa")
	}
	for _, p := range m.cfg.Provedores {
		if p.Local {
			for _, name := range m.listed[p.ID] {
				add(p, name, noteFor(p, name))
			}
		}
	}
	return out
}

// ---- /mapa: varredura e confirmação ----

type mapScannedMsg struct {
	root string
	scan mapa.Scan
	err  error
}

type mapState struct {
	scan    mapa.Scan
	idx     int // modelo escolhido no popup
	steps   []mapStepView
	usage   llm.Usage
	cancel  context.CancelFunc
	ch      chan tea.Msg
	running bool
}

type mapStepView struct {
	label string
	calls int
	state int // 0 esperando, 1 rodando, 2 pronto, 3 falhou
	err   string
}

func (m model) startMapScan() (tea.Model, tea.Cmd) {
	if m.mapGen != nil && m.mapGen.running {
		m.setInfo("o mapa já está sendo gerado")
		return m.showMapTab()
	}
	m.setInfo("lendo o projeto para estimar o mapa…")
	r := m.repo
	cmds := []tea.Cmd{func() tea.Msg {
		s, err := mapa.ScanRepo(r)
		return mapScannedMsg{root: r.Root, scan: s, err: err}
	}}
	for _, p := range m.cfg.Provedores { // modelos locais para a lista
		if p.Local && len(m.listed[p.ID]) == 0 {
			cmds = append(cmds, listModels(p))
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) onMapScanned(msg mapScannedMsg) (tea.Model, tea.Cmd) {
	if msg.root != m.repo.Root {
		return m, nil
	}
	if msg.err != nil {
		m.setErr("não consegui ler o projeto: " + msg.err.Error())
		return m, nil
	}
	m.mapGen = &mapState{scan: msg.scan}
	if m.mapData != nil && mapa.MakePlan(msg.scan, m.mapData, 0).UpToDate() {
		m.setInfo("o mapa está em dia com o projeto")
		return m.showMapTab()
	}
	m.status = ""
	m.mode = modeMap
	return m, nil
}

func (m model) currentMapChoice() (mapChoice, bool) {
	cs := m.mapChoices()
	if len(cs) == 0 || m.mapGen == nil {
		return mapChoice{}, false
	}
	return cs[min(m.mapGen.idx, len(cs)-1)], true
}

func (m model) mapPlan() (mapa.Plan, bool) {
	c, ok := m.currentMapChoice()
	if !ok {
		return mapa.Plan{}, false
	}
	return mapa.MakePlan(m.mapGen.scan, m.mapData, c.prov.Contexto), true
}

func (m model) updateMapKeys(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.mode = modeNormal
		m.mapGen = nil
	case "up", "k":
		m.mapGen.idx = max(m.mapGen.idx-1, 0)
	case "down", "j":
		m.mapGen.idx = min(m.mapGen.idx+1, max(len(m.mapChoices())-1, 0))
	case "enter":
		c, ok := m.currentMapChoice()
		if !ok {
			return m, nil
		}
		m.mode = modeNormal
		return m.runMap(c)
	}
	return m, nil
}

func (m model) runMap(c mapChoice) (tea.Model, tea.Cmd) {
	p, ok := m.mapPlan()
	if !ok {
		return m, nil
	}
	m.cfg.MapaModelo = &config.Choice{Provedor: c.prov.ID, Modelo: c.model}
	_ = m.cfg.Save()
	st := m.mapGen
	st.steps = nil
	for _, s := range p.Steps {
		st.steps = append(st.steps, mapStepView{label: s.Label, calls: s.Calls})
	}
	ctx, cancel := context.WithCancel(context.Background())
	st.cancel, st.running = cancel, true
	st.ch = make(chan tea.Msg, 64)
	prov, model := c.prov, c.model
	conc := 4
	if prov.Local {
		conc = 1
	}
	g := mapa.Generator{Model: model, Commit: m.repo.Head(), Concurrency: conc,
		NewChat: func(system string, maxTok int64) (llm.Chat, error) {
			effort := ""
			if prov.Tipo == llm.KindAnthropic {
				effort, maxTok = "low", maxTok*2 // o pensamento também conta na saída
			}
			return llm.New(prov.LLM(), llm.Options{Model: model, System: system, MaxTokens: maxTok,
				Effort: effort, ContextTokens: prov.Contexto, MaxSteps: 1})
		}}
	ch := st.ch
	go func() {
		mp, u, err := g.Run(ctx, p, func(ev mapa.Event) { ch <- mapEventMsg{ev} })
		ch <- mapDoneMsg{root: "", m: mp, usage: u, err: err}
		close(ch)
	}()
	m.mapChapter = 0
	nm, cmd := m.showMapTab()
	return nm, tea.Batch(cmd, waitFor(ch), m.spin.Tick)
}

type mapEventMsg struct{ ev mapa.Event }

type mapDoneMsg struct {
	root  string
	m     *mapa.Map
	usage llm.Usage
	err   error
}

func (m model) onMapEvent(msg mapEventMsg) (tea.Model, tea.Cmd) {
	st := m.mapGen
	if st == nil || !st.running {
		return m, nil
	}
	ev := msg.ev
	if ev.Step < len(st.steps) {
		switch ev.State {
		case mapa.StepStarted:
			st.steps[ev.Step].state = 1
		case mapa.StepDone:
			st.steps[ev.Step].state = 2
		case mapa.StepFailed:
			st.steps[ev.Step].state, st.steps[ev.Step].err = 3, firstLine(ev.Err.Error())
		}
	}
	st.usage = ev.Usage
	m.renderPreview()
	return m, waitFor(st.ch)
}

func (m model) onMapDone(msg mapDoneMsg) (tea.Model, tea.Cmd) {
	st := m.mapGen
	if st == nil {
		return m, nil
	}
	st.running, st.cancel = false, nil
	tokens := humanTokens(msg.usage.InputTokens + msg.usage.OutputTokens)
	switch {
	case msg.err == context.Canceled:
		m.mapGen = nil
		m.setInfo("geração do mapa cancelada · " + tokens + " tokens gastos")
	case msg.m == nil:
		m.mapGen = nil
		m.setErr("o mapa falhou: " + msg.err.Error())
	default:
		if err := msg.m.Save(mapPath(m.repo.Root)); err != nil {
			m.setErr("não consegui salvar o mapa: " + err.Error())
		}
		m.mapData, m.mapGen, m.mapCacheKey = msg.m, nil, ""
		note := fmt.Sprintf("mapa do projeto gerado com %s · %s tokens · /tour percorre", msg.m.Model, tokens)
		if m.chat != nil {
			note += " · vale como contexto a partir da próxima conversa (ctrl+n)"
		}
		m.entries = append(m.entries, entry{kind: eNotice, text: note})
		m.refreshChat()
		if msg.err != nil {
			m.setErr(msg.err.Error() + " · /mapa tenta de novo só o que faltou")
		} else {
			m.setInfo("mapa pronto · " + tokens + " tokens")
		}
	}
	m.renderPreview()
	return m, nil
}

// ---- /tour e a aba Mapa ----

func (m model) showMapTab() (tea.Model, tea.Cmd) {
	if m.lay.isMinimized(pDiff) {
		m.lay.restore(pDiff)
		m.layoutChanged()
	}
	m.vtab = vtMap
	m.renderPreview()
	m.preview.GotoTop()
	return m, nil
}

func (m model) startTour() (tea.Model, tea.Cmd) {
	if m.mapData == nil && (m.mapGen == nil || !m.mapGen.running) {
		m.setInfo("ainda não há mapa deste projeto: /mapa gera um (você vê a estimativa antes)")
		return m, nil
	}
	m.mapChapter = 0
	return m.showMapTab()
}

func (m model) mapChapters() []mapa.Chapter {
	if m.mapData == nil {
		return nil
	}
	return m.mapData.Chapters()
}

// renderMap desenha a aba Mapa: o progresso da geração ou o capítulo atual.
func (m *model) renderMap(w int) []string {
	if st := m.mapGen; st != nil && st.running {
		lines := []string{titleStyle.Render("gerando o mapa"), ""}
		done := 0
		for _, s := range st.steps {
			mark, label := subtleStyle.Render("○"), subtleStyle.Render(s.label)
			extra := ""
			if s.calls > 1 {
				extra = subtleStyle.Render(fmt.Sprintf(" · %d partes", s.calls))
			}
			switch s.state {
			case 1:
				mark, label = m.spin.View(), s.label
				extra += subtleStyle.Render(" · resumindo…")
			case 2:
				mark, label = okStyle.Render("✓"), s.label
				done++
			case 3:
				mark, label = errStyle.Render("✗"), s.label
				extra += " " + errStyle.Render(s.err)
				done++
			}
			lines = append(lines, truncateW(mark+" "+label+extra, w))
		}
		bar := max(w-2, 10)
		fill := bar * done / max(len(st.steps), 1)
		lines = append(lines, "", lipgloss.NewStyle().Foreground(accent).Render(strings.Repeat("━", fill))+
			lipgloss.NewStyle().Foreground(faint).Render(strings.Repeat("━", bar-fill)),
			subtleStyle.Render(fmt.Sprintf("%d de %d · %s tokens até agora · esc cancela", done, len(st.steps),
				humanTokens(st.usage.InputTokens+st.usage.OutputTokens))))
		return lines
	}
	chs := m.mapChapters()
	if len(chs) == 0 {
		return []string{subtleStyle.Render("Ainda não há mapa deste projeto."), "",
			subtleStyle.Render("/mapa no chat gera um: um modelo barato resume cada pasta, e o"),
			subtleStyle.Render("mapa fica salvo. Antes, o dante mostra quantos tokens vai gastar.")}
	}
	m.mapChapter = min(max(m.mapChapter, 0), len(chs)-1)
	key := fmt.Sprintf("%d\x00%d\x00%s", m.mapChapter, w, m.mapData.Generated)
	if key != m.mapCacheKey {
		c := chs[m.mapChapter]
		body := markdown(m.annotateRefs(c.Body), w)
		head := titleStyle.Render(fmt.Sprintf("%d · %s", m.mapChapter+1, c.Title))
		m.mapCache, m.mapCacheKey = head+"\n\n"+body, key
	}
	return strings.Split(m.mapCache, "\n")
}

func (m model) mapHeader(w int) (string, string) {
	if st := m.mapGen; st != nil && st.running {
		return boldStyle.Render("Mapa do projeto"), subtleStyle.Render("gerando · o resultado fica salvo para as próximas vezes")
	}
	chs := m.mapChapters()
	if len(chs) == 0 {
		return boldStyle.Render("Mapa do projeto"), subtleStyle.Render("/mapa gera · [ ] troca de aba")
	}
	var titles []string
	for i, c := range chs {
		t := fmt.Sprintf("%d %s", i+1, c.Title)
		if i == m.mapChapter {
			titles = append(titles, selectedStyle.Render(t))
		} else {
			titles = append(titles, subtleStyle.Render(t))
		}
	}
	l1 := boldStyle.Render("Tour ") + strings.Join(titles, subtleStyle.Render(" · "))
	// Mantém o capítulo atual visível quando a lista não cabe.
	if start := max(m.mapChapter-1, 0); lipgloss.Width(l1) > w && start > 0 {
		l1 = boldStyle.Render("Tour ") + subtleStyle.Render("… ") + strings.Join(titles[start:], subtleStyle.Render(" · "))
	}
	l2 := fmt.Sprintf("gerado com %s no commit %s · ←→ capítulo · ctrl+g referências · /mapa atualiza", m.mapData.Model, shortSHA(m.mapData.Commit))
	return l1, subtleStyle.Render(l2)
}

func (m model) updateMapTabKeys(key string) (tea.Model, tea.Cmd, bool) {
	switch key {
	case "left", "h", "N", "p":
		if m.mapChapter > 0 {
			m.mapChapter--
			m.renderPreview()
			m.preview.GotoTop()
		}
	case "right", "l", "n":
		if m.mapChapter < len(m.mapChapters())-1 {
			m.mapChapter++
			m.renderPreview()
			m.preview.GotoTop()
		}
	case "esc":
		if st := m.mapGen; st != nil && st.running && st.cancel != nil {
			st.cancel()
			m.setInfo("cancelando o mapa…")
			return m, nil, true
		}
		return m, nil, false
	default:
		return m, nil, false
	}
	return m, nil, true
}

// ---- popup de confirmação ----

func (m model) mapPopup() string {
	var b strings.Builder
	p, ok := m.mapPlan()
	title := "/mapa · gerar o mapa do projeto?"
	if m.mapData != nil {
		title = "/mapa · atualizar o mapa do projeto?"
	}
	b.WriteString(titleStyle.Render(title) + "\n\n")
	if !ok {
		b.WriteString("Nenhum modelo disponível: cadastre uma chave (m, depois c)\nou instale um modelo no Ollama.\n\n")
		b.WriteString(subtleStyle.Render("esc fecha"))
		return boxStyle.Padding(1, 2).Render(b.String())
	}
	row := func(k, v string) { b.WriteString(subtleStyle.Render(padTo(k, 16)) + v + "\n") }
	proj := fmt.Sprintf("%d arquivos · %d KB · %s", p.Files(), p.Bytes()/1024, plural(p.Folders(), "pasta", "pastas"))
	row("projeto", proj)
	if p.Skipped() > 0 {
		row("de fora", fmt.Sprintf("%d (sensíveis, binários, grandes, travas)", p.Skipped()))
	}
	if m.mapData != nil {
		row("a resumir", fmt.Sprintf("%d de %d pastas mudaram", max(len(p.Steps)-1, 0), p.Folders()))
	}
	row("leitura única", "~"+humanTokens(int64(p.Tokens))+" tokens")
	row("depois", fmt.Sprintf("~%s tokens por conversa (o mapa compacto)", humanTokens(int64(min(p.Folders()*60+700, compactMapBytes/3)))))
	b.WriteString("\n" + subtleStyle.Render("Modelo do mapa (um barato basta):") + "\n")
	cs := m.mapChoices()
	for i, c := range cs {
		line := c.model + subtleStyle.Render(" · "+c.note)
		if i == min(m.mapGen.idx, len(cs)-1) {
			b.WriteString(selectedStyle.Render("▸ "+c.model) + subtleStyle.Render(" · "+c.note) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n" + okStyle.Bold(true).Render("enter") + " gerar   " + subtleStyle.Render("esc") + " cancelar   " +
		subtleStyle.Render("↑↓") + " trocar o modelo")
	return boxStyle.Padding(1, 2).Render(b.String())
}
