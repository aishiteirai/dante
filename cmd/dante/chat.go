package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type entryKind int

const (
	eUser entryKind = iota
	eAssistant
	eTool
	eNotice
	eError
	eUsage // gasto de tokens de uma resposta
)

type entry struct {
	kind   entryKind
	text   string
	ok     bool // ferramenta: terminou sem erro
	done   bool // ferramenta: terminou
	cache  string
	cacheW int
	cacheN int
}

var guidelineFiles = []string{".dante/diretrizes.md", ".revisor/diretrizes.md", "DANTE.md", "REVIEW.md"}

const systemPrompt = `Você é o dante, um revisor de código sênior que trabalha no terminal do desenvolvedor, no repositório %q.
O desenvolvedor navega pelos commits e pelos arquivos alterados e conversa com você sobre as mudanças. Cada mensagem dele
começa com um bloco <contexto> que diz o alvo em revisão (um commit ou as mudanças locais ainda não commitadas) e o
arquivo em foco. Quando cabe, vem também um bloco <diff> com as mudanças e 10 linhas de contexto ao redor de cada uma.

Como trabalhar. Cada rodada de ferramentas reenvia a conversa inteira, então leia com economia:
- Comece pelo <diff>. Ele já mostra o que mudou; não peça git_diff de novo para o mesmo alvo. Sem <diff> na mensagem,
  peça git_diff por arquivo, começando pelos que têm mais lógica.
- Abra arquivos só para o que o diff não mostra: quem chama o código alterado, tipos, testes relacionados. Peça só os
  trechos necessários (start_line/end_line) e junte tudo numa chamada de read_files, ou faça várias chamadas de
  ferramenta na mesma resposta. Não abra de novo um arquivo que já leu nesta conversa.
- Não opine sobre código que não viu, no diff ou num arquivo. Use git_log e git_blame quando a intenção original ajudar.
- Num commit, read_file com ref=<sha> mostra o arquivo como estava naquele commit; sem ref, mostra o que está no disco hoje.
- Ancore cada observação em arquivo:linha. Para bugs e falhas de segurança, descreva uma entrada ou situação concreta que
  leva ao erro; se não conseguir, diga que é uma suspeita.
- Prioridade: bugs, depois segurança, depois qualidade e simplificação, depois os padrões do time. Não comente estilo que
  um formatador resolveria.
- Se as mudanças estiverem boas, diga isso sem inventar problemas.
- Por enquanto você só lê: não consegue alterar arquivos. Ao sugerir correções, mostre o trecho de código proposto.

Responda em português, em markdown, de forma direta. Numa revisão, comece com um resumo de uma ou duas frases e depois
liste os pontos do mais grave para o menos grave, cada um com arquivo:linha, o problema, por que importa e a sugestão.
Em perguntas simples, responda só o que foi perguntado.`

func (m *model) buildSystemPrompt() string {
	s := fmt.Sprintf(systemPrompt, repoDisplayName(m.repo.Root))
	for _, name := range guidelineFiles {
		if b, err := os.ReadFile(filepath.Join(m.repo.Root, name)); err == nil {
			g := string(b)
			if len(g) > 20000 {
				g = g[:20000]
			}
			s += "\n\n<diretrizes_do_time arquivo=\"" + name + "\">\n" + strings.TrimSpace(g) + "\n</diretrizes_do_time>"
			break
		}
	}
	return s
}

func (m *model) selectModel(p config.Provider, name string) {
	m.prov, m.modelName = p, name
	m.cfg.UltimoModelo = &config.Choice{Provedor: p.ID, Modelo: name}
	_ = m.cfg.Save()
	m.resetChat()
	if !p.HasKey() {
		m.setErr(fmt.Sprintf("%s sem chave: pressione m e depois c para cadastrar", p.Nome))
	}
}

func (m *model) resetChat() {
	if m.cancel != nil {
		m.cancel()
	}
	m.chat = nil
	m.entries = nil
	m.diffSent, m.diffPending = "", ""
	m.streaming = false
	m.usage = llm.Usage{}
	m.convStart = time.Time{}
	m.refreshChat()
}

func (m *model) ensureChat() error {
	if m.chat != nil {
		return nil
	}
	if m.modelName == "" {
		return fmt.Errorf("escolha um modelo (tecla m)")
	}
	effort := ""
	if m.prov.Tipo == llm.KindAnthropic {
		effort = m.cfg.Esforco
	}
	sendTo := m.sendTo
	c, err := llm.New(m.prov.LLM(), llm.Options{
		Model:  m.modelName,
		System: m.buildSystemPrompt(),
		Tools:  repo.Tools(),
		Exec:   m.repo.Executor(func() string { return *sendTo }),
		Effort: effort,
		// Contexto do provedor; para o Claude vale o padrão do servidor (limpeza a 150 mil).
		ContextTokens: m.effectiveContext(),
		TurnBudget:    m.cfg.TurnBudget(),
	})
	if err != nil {
		return err
	}
	m.chat = c
	m.convStart = time.Now()
	return nil
}

// Bloco de contexto anexado a cada mensagem: alvo, arquivos e arquivo em foco.
func (m model) contextBlock() string {
	var b strings.Builder
	b.WriteString("<contexto>\n")
	if m.branch != "" {
		fmt.Fprintf(&b, "Branch atual: %s\n", m.branch)
	}
	if m.target == repo.WorkTree {
		b.WriteString("Alvo: mudanças locais não commitadas (working tree comparado com HEAD; arquivos com status ? ainda não estão no git)\n")
	} else {
		msg := m.repo.CommitMessage(m.target)
		fmt.Fprintf(&b, "Alvo: commit %s\n%s\n", m.target, indent(msg, "  "))
		if m.target != m.repo.Head() {
			fmt.Fprintf(&b, "Este commit não é o HEAD: para ver os arquivos como estavam nele, use read_file com ref=%s.\n", shortSHA(m.target))
		}
	}
	if len(m.files) > 0 {
		fmt.Fprintf(&b, "Arquivos alterados no alvo (%d):\n", len(m.files))
		for i, f := range m.files {
			if i == 80 {
				fmt.Fprintf(&b, "  … e mais %d\n", len(m.files)-80)
				break
			}
			fmt.Fprintf(&b, "  %s %s (+%d -%d)\n", f.Status, f.Path, f.Add, f.Del)
		}
		fmt.Fprintf(&b, "Arquivo em foco: %s\n", m.files[m.fileIdx].Path)
	}
	b.WriteString("</contexto>\n\n")
	out, _ := repo.Redact(b.String())
	return out
}

func indent(s, pre string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pre + l
	}
	return strings.Join(lines, "\n")
}

// Atalho `r`: pede uma revisão do alvo inteiro ou de um arquivo.
func (m model) quickReview(path string) (tea.Model, tea.Cmd) {
	text := "Revise as mudanças deste alvo: diga o que está bom e o que precisa mudar."
	if path != "" {
		text = fmt.Sprintf("Revise as mudanças em `%s`.", path)
	}
	fm, cmd := m.focusPane(pChat)
	nm, send := fm.(model).sendScoped(text, path)
	return nm, tea.Batch(cmd, send)
}

// Diffs maiores que isto não vão na mensagem; o modelo pede por arquivo.
const maxInlineDiff = 60_000

// Contexto assumido para provedores sem "contexto" no config.json (ex.: um provedor
// compatível com OpenAI que o usuário acrescentou). Conservador: a maioria dos modelos
// atuais tem pelo menos isso.
const defaultContextTokens = 64000

// effectiveContext é o contexto do modelo em tokens. No Claude, 0: o servidor cuida dos
// limites (limpeza de contexto) e a janela é de 1 milhão.
func (m model) effectiveContext() int {
	switch {
	case m.prov.Contexto > 0:
		return m.prov.Contexto
	case m.prov.Tipo == llm.KindAnthropic:
		return 0
	}
	return defaultContextTokens
}

// diffBlock devolve o diff do alvo (ou de um arquivo dele) para ir na mensagem, uma vez por
// conversa para cada alvo/arquivo. Diffs grandes viram só um aviso.
func (m *model) diffBlock(scope string) string {
	key := m.target + "\x00" + scope
	if key == m.diffSent || (scope != "" && m.diffSent == m.target+"\x00") {
		return "" // já enviado nesta conversa (o do alvo inteiro cobre os arquivos)
	}
	limit := maxInlineDiff
	if c := m.effectiveContext(); c > 0 {
		limit = min(limit, c*3/4) // até ~1/4 do contexto (3 bytes por token)
	}
	d, err := m.repo.DiffForModel(m.target, scope, repo.DiffContextLines)
	if err != nil || strings.TrimSpace(d) == "" {
		return ""
	}
	what := "do alvo"
	if scope != "" {
		what = "de " + scope
	}
	if len(d) > limit {
		return fmt.Sprintf("<diff>O diff %s tem %d KB, grande demais para vir aqui: peça git_diff por arquivo.</diff>\n\n",
			what, len(d)/1024)
	}
	m.diffPending = key // vira diffSent quando a resposta terminar sem erro
	return "<diff " + what + ">\n" + strings.TrimRight(d, "\n") + "\n</diff>\n\n"
}

func (m model) send(text string) (tea.Model, tea.Cmd) { return m.sendScoped(text, "") }

// sendScoped envia a mensagem; scope é o arquivo cujo diff acompanha a mensagem ("" = alvo inteiro).
func (m model) sendScoped(text, scope string) (tea.Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" {
		return m, nil
	}
	if m.streaming {
		m.setInfo("aguarde a resposta terminar (esc cancela)")
		return m, nil
	}
	if err := m.ensureChat(); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	*m.sendTo = m.target
	m.diffPending = ""
	prompt := m.contextBlock() + m.diffBlock(scope) + text
	m.entries = append(m.entries, entry{kind: eUser, text: text})
	m.streaming, m.thinking = true, false
	m.status = ""

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := make(chan tea.Msg, 256)
	m.events = ch
	chat := m.chat
	go func() {
		u, err := chat.Send(ctx, prompt, func(ev llm.Event) { ch <- chatEventMsg{ev} })
		ch <- chatDoneMsg{usage: u, err: err}
		close(ch)
	}()
	m.refreshChat()
	m.chatView.GotoBottom()
	return m, tea.Batch(waitFor(ch), m.spin.Tick)
}

func (m model) updateChatKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		if m.streaming {
			m.setInfo("aguarde a resposta terminar (esc cancela)")
			return m, nil
		}
		text := m.input.Value()
		m.input.Reset()
		return m.send(text)
	case "esc":
		if m.streaming && m.cancel != nil {
			m.cancel()
			m.setInfo("cancelando…")
			return m, nil
		}
		return m.setFocus(pFiles)
	case "pgup":
		m.chatView.HalfPageUp()
		return m, nil
	case "pgdown":
		m.chatView.HalfPageDown()
		return m, nil
	case "ctrl+n":
		if m.streaming {
			return m, nil
		}
		m.resetChat()
		m.setInfo("nova conversa")
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m *model) onChatEvent(ev llm.Event) {
	follow := m.chatView.AtBottom()
	switch ev.Kind {
	case llm.EvText:
		m.thinking = false
		if n := len(m.entries); n > 0 && m.entries[n-1].kind == eAssistant {
			m.entries[n-1].text += ev.Text
		} else {
			m.entries = append(m.entries, entry{kind: eAssistant, text: ev.Text})
		}
	case llm.EvReasoning:
		m.thinking = true
	case llm.EvToolCall:
		m.thinking = false
		m.entries = append(m.entries, entry{kind: eTool, text: describeCall(ev.Call)})
	case llm.EvToolResult:
		for i := len(m.entries) - 1; i >= 0; i-- {
			if m.entries[i].kind == eTool && !m.entries[i].done {
				m.entries[i].done, m.entries[i].ok = true, !ev.IsError
				if ev.IsError {
					m.entries[i].text += " — " + firstLine(ev.Text)
				}
				break
			}
		}
		if !ev.IsError {
			m.showOpenedByModel(ev.Call, ev.Text)
		}
	case llm.EvNotice:
		kind := eNotice
		if ev.IsError {
			kind = eError
		}
		m.entries = append(m.entries, entry{kind: kind, text: ev.Text})
	}
	m.refreshChat()
	if follow {
		m.chatView.GotoBottom()
	}
}

// O modelo abriu um arquivo: mostra o documento (ou o diff) no painel do meio.
func (m *model) showOpenedByModel(call llm.ToolCall, result string) {
	var a struct {
		Path string `json:"path"`
		Ref  string `json:"ref"`
	}
	_ = json.Unmarshal([]byte(call.Args), &a)
	if call.Name == "read_files" { // o lote inteiro aparece no painel, separado por arquivo
		var b struct {
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		}
		_ = json.Unmarshal([]byte(call.Args), &b)
		if len(b.Files) == 0 {
			return
		}
		a.Path = b.Files[0].Path
		m.selectFile(a.Path)
		m.setDoc(fmt.Sprintf("%s abertos pelo modelo", plural(len(b.Files), "arquivo", "arquivos")), result, false)
		return
	}
	if a.Path == "" || (call.Name != "read_file" && call.Name != "git_diff") {
		return
	}
	m.selectFile(a.Path)
	title := a.Path
	if a.Ref != "" {
		title += " @ " + shortSHA(a.Ref)
	}
	if call.Name == "git_diff" {
		m.setDoc("diff de "+a.Path+" visto pelo modelo", result, true)
		return
	}
	m.setDoc(title, result, false)
}

func (m *model) selectFile(path string) {
	for i, f := range m.files {
		if f.Path == path {
			m.fileIdx = i
			m.fixFilesScroll()
			return
		}
	}
}

// humanTokens: 950 → "950", 58200 → "58,2 mil", 1250000 → "1,25 mi".
func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return strings.Replace(fmt.Sprintf("%.2f mi", float64(n)/1e6), ".", ",", 1)
	case n >= 1000:
		return strings.Replace(fmt.Sprintf("%.1f mil", float64(n)/1e3), ".", ",", 1)
	}
	return fmt.Sprint(n)
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func describeCall(c llm.ToolCall) string {
	var a struct {
		Path      string `json:"path"`
		Ref       string `json:"ref"`
		Pattern   string `json:"pattern"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	_ = json.Unmarshal([]byte(c.Args), &a)
	loc := a.Path
	if a.StartLine > 0 {
		loc += fmt.Sprintf(":%d-%d", a.StartLine, a.EndLine)
	}
	if a.Ref != "" {
		loc += " @ " + shortSHA(a.Ref)
	}
	switch c.Name {
	case "read_file":
		return "abriu " + loc
	case "read_files":
		var b struct {
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		}
		_ = json.Unmarshal([]byte(c.Args), &b)
		var names []string
		for _, f := range b.Files {
			names = append(names, f.Path)
		}
		return fmt.Sprintf("abriu %s: %s", plural(len(names), "arquivo", "arquivos"), strings.Join(names, ", "))
	case "git_diff":
		if loc == "" {
			return "viu o diff do alvo"
		}
		return "viu o diff de " + loc
	case "grep":
		s := fmt.Sprintf("buscou %q", a.Pattern)
		if a.Path != "" {
			s += " em " + a.Path
		}
		return s
	case "list_files":
		if a.Path == "" {
			return "listou os arquivos"
		}
		return "listou " + a.Path
	case "git_log":
		return "consultou o git log " + a.Path
	case "git_blame":
		return "git blame " + loc
	}
	return c.Name + " " + c.Args
}

func (m model) onChatDone(msg chatDoneMsg) (tea.Model, tea.Cmd) {
	m.streaming, m.thinking = false, false
	m.cancel = nil
	m.usage.InputTokens += msg.usage.InputTokens
	m.usage.OutputTokens += msg.usage.OutputTokens
	// Ferramentas que ficaram sem resultado (cancelamento).
	for i := range m.entries {
		if m.entries[i].kind == eTool && !m.entries[i].done {
			m.entries[i].done = true
		}
	}
	if t := msg.usage.InputTokens + msg.usage.OutputTokens; t > 0 {
		m.entries = append(m.entries, entry{kind: eUsage, text: fmt.Sprintf("%s tokens nesta resposta (entrada %s · saída %s)",
			humanTokens(t), humanTokens(msg.usage.InputTokens), humanTokens(msg.usage.OutputTokens))})
	}
	switch {
	case msg.err == nil:
		m.status = ""
		if m.diffPending != "" {
			m.diffSent = m.diffPending
		}
		// Commit conversado conta como revisado no painel de commits.
		if m.target != repo.WorkTree && m.reviewed != nil && !m.reviewed[m.target] {
			m.reviewed[m.target] = true
			saveReviewed(m.repo.Root, m.reviewed)
		}
	case msg.err == context.Canceled:
		m.entries = append(m.entries, entry{kind: eNotice, text: "resposta cancelada"})
		m.status = ""
	default:
		m.entries = append(m.entries, entry{kind: eError, text: msg.err.Error()})
		if strings.Contains(msg.err.Error(), "chave") {
			m.setErr("problema com a chave: pressione m e depois c para cadastrar outra")
		}
	}
	m.refreshChat()
	m.chatView.GotoBottom()
	m.saveTranscript()
	return m, nil
}

// Conversa salva em markdown: ~/.local/share/dante/conversas/<repo>/<início>.md
func (m model) saveTranscript() {
	if m.convStart.IsZero() || len(m.entries) == 0 {
		return
	}
	dir := filepath.Join(dataHome(), "conversas", repoDisplayName(m.repo.Root))
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s/%s\n\n%s\n\n", repoDisplayName(m.repo.Root), m.prov.ID, m.modelName, m.convStart.Format(time.RFC3339))
	for _, e := range m.entries {
		switch e.kind {
		case eUser:
			fmt.Fprintf(&b, "## Você\n\n%s\n\n", e.text)
		case eAssistant:
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", m.modelName, e.text)
		case eTool:
			fmt.Fprintf(&b, "> %s\n\n", e.text)
		default:
			fmt.Fprintf(&b, "> **%s**\n\n", e.text)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, m.convStart.Format("20060102T150405")+".md"), []byte(b.String()), 0o644)
}

// ---- renderização do chat ----

var (
	mdRenderers = map[int]*glamour.TermRenderer{}
	darkBg      = true
)

func markdown(text string, width int) string {
	r, ok := mdRenderers[width]
	if !ok {
		style := "dark"
		if !darkBg {
			style = "light"
		}
		var err error
		r, err = glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(width), glamour.WithEmoji())
		if err != nil {
			return lipgloss.NewStyle().Width(width).Render(text)
		}
		mdRenderers[width] = r
	}
	out, err := r.Render(text)
	if err != nil {
		return lipgloss.NewStyle().Width(width).Render(text)
	}
	return strings.Trim(out, "\n")
}

func (m *model) refreshChat() {
	w := max(m.chatView.Width, 20)
	var parts []string
	if len(m.entries) == 0 {
		parts = append(parts, m.chatWelcome(w))
	}
	last := len(m.entries) - 1
	for i := range m.entries {
		e := &m.entries[i]
		switch e.kind {
		case eUser:
			parts = append(parts, "", selectedStyle.Render("você")+"\n"+lipgloss.NewStyle().Width(w).Render(e.text))
		case eAssistant:
			body := ""
			if m.streaming && i == last {
				body = lipgloss.NewStyle().Width(w).Render(e.text) // texto cru enquanto chega
			} else {
				if e.cacheW != w || e.cacheN != len(e.text) {
					e.cache, e.cacheW, e.cacheN = markdown(e.text, w), w, len(e.text)
				}
				body = e.cache
			}
			head := ""
			if i == 0 || m.entries[i-1].kind == eUser {
				head = "\n" + titleStyle.Render(m.modelName) + "\n"
			}
			parts = append(parts, head+body)
		case eTool:
			mark := m.spin.View()
			if e.done {
				mark = okStyle.Render("✓")
				if !e.ok {
					mark = errStyle.Render("✗")
				}
			}
			parts = append(parts, "  "+mark+" "+subtleStyle.Render(truncateW(e.text, w-4)))
		case eNotice:
			parts = append(parts, "  "+warnStyle.Render("• "+e.text))
		case eError:
			parts = append(parts, "  "+errStyle.Render(lipgloss.NewStyle().Width(w-2).Render("⚠ "+e.text)))
		case eUsage:
			parts = append(parts, "  "+subtleStyle.Render(truncateW("↳ "+e.text, w-2)))
		}
	}
	if m.streaming && (last < 0 || m.entries[last].kind != eAssistant || m.thinking) {
		label := " pensando…"
		if last >= 0 && m.entries[last].kind == eTool && !m.entries[last].done {
			label = ""
		}
		if label != "" {
			parts = append(parts, "  "+m.spin.View()+subtleStyle.Render(label))
		}
	}
	m.chatView.SetContent(strings.Join(parts, "\n"))
}

func (m model) chatWelcome(w int) string {
	lines := []string{
		subtleStyle.Render("Escolha um commit e um arquivo e pergunte o que quiser."),
		"",
		subtleStyle.Render("Atalhos:"),
		subtleStyle.Render("  r no painel de commits   revisa o alvo inteiro"),
		subtleStyle.Render("  r no painel de arquivos  revisa o arquivo"),
		"",
		subtleStyle.Render("O modelo abre os arquivos sozinho, e o"),
		subtleStyle.Render("painel do meio mostra o que ele está lendo."),
	}
	return lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
}
