package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aishiteirai/dante/internal/checks"
	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

type entryKind int

const (
	eUser entryKind = iota
	eAssistant
	eTool
	eNotice
	eError
	eUsage   // gasto de tokens de uma resposta
	eFinding // achado de revisão, desenhado como cartão
	eChange  // mudança de arquivo proposta ou feita pelo agente
)

type entry struct {
	kind   entryKind
	text   string
	f      *finding    // eFinding
	ref    int         // número da referência do achado
	chg    *changeCard // eChange
	ok     bool        // ferramenta: terminou sem erro
	done   bool        // ferramenta: terminou
	cache  string
	cacheW int
	cacheN int
}

var guidelineFiles = []string{".dante/diretrizes.md", ".revisor/diretrizes.md", "DANTE.md", "REVIEW.md"}

const systemPrompt = `Você é o dante, um assistente de código sênior que trabalha no terminal do desenvolvedor, no repositório
%q. Você revisa mudanças e também explica o código para quem quer entendê-lo. O desenvolvedor navega pelos commits e pelos
arquivos alterados e conversa com você. Cada mensagem dele começa com um bloco <contexto> que diz o alvo (um commit ou as
mudanças locais ainda não commitadas) e o arquivo em foco. Na primeira pergunta sobre um alvo, vem também um bloco <diff>
com as mudanças do código (3 linhas de contexto; o cabeçalho de cada trecho diz a função) e, em <fora_do_diff>, os
arquivos alterados que não vieram (testes, documentação, gerados, só espaços ou os que não couberam), e em <quem_chama>
onde as funções alteradas são usadas. Por último vem um bloco <modo>
com a tarefa (revisar ou explicar) e o formato da resposta: siga-o, ele vale para aquela mensagem.

Como trabalhar. Cada rodada de ferramentas reenvia a conversa inteira: leia com propósito, em lote, e pare quando
tiver o que precisa.
- Comece pelo <diff>. Não peça git_diff de novo para o que já veio; peça só os de <fora_do_diff> que importarem
  (ex.: o teste do código alterado).
- Para entender o porquê e o impacto de uma mudança, costuma valer ler o código ao redor: quem chama a função
  alterada (veja <quem_chama>), o tipo usado, o teste. Peça todos os trechos de uma vez, numa chamada de read_files
  com start_line/end_line. Arquivo grande lido sem trecho volta como esqueleto, com as linhas de cada declaração.
  Não releia o que já está na conversa.
- Não opine sobre código que não viu, no diff ou num arquivo. Use git_log e git_blame quando a intenção original ajudar.
- Num commit, read_file com ref=<sha> mostra o arquivo como estava naquele commit; sem ref, mostra o que está no disco hoje.
- Ancore cada observação em arquivo:linha (ex.: internal/llm/openai.go:111). Numa revisão, para bugs e falhas de segurança,
  descreva uma entrada ou situação concreta que leva ao erro; se não conseguir, diga que é uma suspeita.
- Prioridade numa revisão: bugs, depois segurança, depois qualidade e simplificação, depois os padrões do time. Não
  comente estilo que um formatador resolveria.
- O bloco <modo> diz também a sua permissão: ler (você não altera arquivos; ao sugerir correções, mostre o trecho
  proposto) ou editar (você pode alterar arquivos com edit_file, write_file e delete_file, quando o pedido for esse).
  Só o desenvolvedor troca a permissão; nunca peça para ele trocar só para você aplicar uma sugestão de revisão.

Como responder (em português, em markdown; o chat é um painel de terminal, então seja enxuto):
- Comece pela resposta, numa frase: o efeito da mudança na prática, não a lista de arquivos.
- Explique o porquê e o impacto: como era antes, como fica agora, quem é afetado, o que pode quebrar. Nunca descreva
  o diff arquivo por arquivo ("adiciona X em a.go, edita b.go"): agrupe por ideia.
- A mensagem do commit é uma pista da intenção; confira no código.
- Parágrafos de até 3 linhas; prefira tópicos curtos. Use rótulos em negrito (**Por quê:**) em vez de títulos; títulos
  (##) só em respostas longas.
- Cite código como caminho/arquivo.ext:LINHA e funções entre crases. Commits pelo SHA curto (7 caracteres).
- Sem introdução ("Claro!", "Vou analisar…"), sem repetir a pergunta, sem conclusão genérica nem elogios.
- Em perguntas simples, responda só o que foi perguntado.`

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
	if m.mapData != nil {
		s += fmt.Sprintf("\n\n<mapa_do_projeto commit=%q>\nResumo do projeto feito antes, para você se orientar. Pode estar "+
			"desatualizado: confira no código antes de afirmar algo.\n\n%s\n</mapa_do_projeto>",
			shortSHA(m.mapData.Commit), m.mapData.Compact(compactMapBytes))
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
	m.refs, m.refIdx = nil, 0
	m.approval = nil
	m.packed.reset()
	m.tipShown = false
	m.checkBlock, m.checkSent, m.checksFor, m.checksTip = "", false, "", false
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
		// As ferramentas de escrita vão em toda conversa (a lista não pode mudar no meio dela);
		// no modo ler, elas recusam.
		Tools: append(append(repo.Tools(), findingToolDef()), repo.WriteTools()...),
		Exec: m.writes().wrap(m.repo, withFindings(withPacked(m.packed, sendTo,
			m.repo.Executor(func() string { return *sendTo })))),
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
	if base, tip, ok := repo.SplitRange(m.target); ok {
		subs := m.repo.RangeCommits(m.target, 30)
		fmt.Fprintf(&b, "Alvo: a branch %s inteira, comparada com %s desde que saiu dela, como um PR (%s):\n",
			tip, base, plural(len(subs), "commit", "commits"))
		for _, l := range subs {
			b.WriteString("  " + l + "\n")
		}
		fmt.Fprintf(&b, "Para ler os arquivos como estão na branch, use read_file com ref=%s.\n", tip)
	} else if m.target == repo.WorkTree {
		b.WriteString("Alvo: mudanças locais não commitadas (working tree comparado com HEAD; arquivos com status ? ainda não estão no git)\n")
	} else {
		msg := m.repo.CommitMessage(m.target)
		fmt.Fprintf(&b, "Alvo: commit %s\n%s\n", shortSHA(m.target), indent(msg, "  "))
		// Num merge, a intenção está nos commits que ele trouxe.
		if subs := m.repo.MergedSubjects(m.target, 15); len(subs) > 0 {
			b.WriteString("Commits trazidos por este merge:\n")
			for _, l := range subs {
				b.WriteString("  " + l + "\n")
			}
		}
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
	m.task = taskRevisar // a tecla r é sempre uma revisão
	fm, cmd := m.focusPane(pChat)
	mm := fm.(model)
	// Com as verificações permitidas, as ferramentas do projeto rodam antes e os problemas
	// vão à revisão como fatos. Sem a permissão, uma dica, uma vez por conversa.
	if mm.checksFor != mm.target && mm.modelName != "" && !mm.streaming {
		if files, _ := mm.checkFiles(); files != nil {
			if mm.cfg.ChecksAllowed(mm.repo.Root) {
				mm.checksFor = mm.target
				nm, run := mm.startChecks(true, text, path)
				return nm, tea.Batch(cmd, run)
			}
			if !mm.checksTip && len(checks.Detect(mm.repo.Root, files)) > 0 {
				mm.checksTip = true
				mm.entries = append(mm.entries, entry{kind: eNotice,
					text: "dica: /verificar roda as ferramentas do projeto (linters, go vet, tsc…) nos arquivos alterados, e a revisão passa a usá-las"})
			}
		}
	}
	nm, send := mm.sendScoped(text, path)
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

// diffLimit é o tamanho máximo do diff que vai na mensagem: até ~1/4 do contexto do
// modelo (3 bytes por token), no máximo maxInlineDiff.
func (m model) diffLimit() int {
	limit := maxInlineDiff
	if c := m.effectiveContext(); c > 0 {
		limit = min(limit, c*3/4)
	}
	return limit
}

// diffBlock devolve o diff que vai na mensagem, uma vez por conversa para cada alvo ou
// arquivo. Para o alvo inteiro, é o pacote de revisão: o código alterado e, à parte, a
// lista do que ficou de fora (testes, documentação, gerados, o que não coube).
func (m *model) diffBlock(scope string) string {
	key := m.target + "\x00" + scope
	if key == m.diffSent || (scope != "" && m.diffSent == m.target+"\x00") {
		return "" // já enviado nesta conversa (o do alvo inteiro cobre os arquivos)
	}
	limit := m.diffLimit()
	// A resposta rápida não precisa do diff inteiro: os arquivos que ficam de fora vão como
	// lista, com as funções alteradas, e o modelo pede o diff de um deles se precisar.
	if m.level == levelRapido {
		limit = min(limit, 6000)
	}
	if scope != "" {
		d, err := m.repo.DiffForModel(m.target, scope, repo.DiffContextLines)
		if err != nil || strings.TrimSpace(d) == "" {
			return ""
		}
		if len(d) > limit {
			return fmt.Sprintf("<diff>O diff de %s tem %d KB, grande demais para vir aqui: peça git_diff com path e "+
				"leia os trechos que importam.</diff>\n\n", scope, len(d)/1024)
		}
		m.diffPending = key // vira diffSent quando a resposta terminar sem erro
		m.markPacked(scope)
		return "<diff de " + scope + ">\n" + strings.TrimRight(d, "\n") + "\n</diff>\n\n"
	}
	p, err := m.repo.ReviewDiff(m.target, limit)
	if err != nil || (p.Included == 0 && len(p.Left) == 0) {
		return ""
	}
	var b strings.Builder
	if p.Included > 0 {
		b.WriteString("<diff do alvo>\n" + strings.TrimRight(p.Diff, "\n") + "\n</diff>\n")
		for _, f := range p.Files {
			m.markPacked(f)
		}
	}
	if p.Included > 0 && m.task == taskRevisar {
		b.WriteString(reviewChecklist(p.Diff))
	}
	if len(p.Left) > 0 {
		b.WriteString("<fora_do_diff>\nAlterados, mas sem o diff acima (peça git_diff com path se precisar):\n")
		for i, l := range p.Left {
			if i == 60 {
				fmt.Fprintf(&b, "… e mais %d\n", len(p.Left)-60)
				break
			}
			fmt.Fprintf(&b, "- %s (+%d −%d, %s)", l.Path, l.Add, l.Del, l.Why)
			if len(l.Funcs) > 0 {
				fs := l.Funcs[:min(len(l.Funcs), 6)]
				b.WriteString(": " + strings.Join(fs, ", "))
				if len(l.Funcs) > 6 {
					b.WriteString(", …")
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("</fora_do_diff>\n")
	}
	if len(p.Callers) > 0 {
		b.WriteString("<quem_chama>\nOnde as funções alteradas são usadas (só a posição; leia o trecho se a revisão depender dele):\n")
		for _, c := range p.Callers {
			fmt.Fprintf(&b, "- %s (%s): %s", c.Name, c.File, strings.Join(c.Sites, ", "))
			if c.More > 0 {
				fmt.Fprintf(&b, " … e mais %d", c.More)
			}
			b.WriteString("\n")
		}
		b.WriteString("</quem_chama>\n")
	}
	m.diffPending = key
	return b.String() + "\n"
}

// turnLimits são as rodadas de leitura de cada pergunta, conforme a tarefa e o formato.
// No modo editar não há limite: editar pede ler antes.
func (m model) turnLimits() llm.Limits {
	free := []string{findingTool}
	for name := range writeTools {
		free = append(free, name)
	}
	// Uma rodada só com achados encerra a resposta: o resumo veio antes, e o resultado da
	// ferramenta é só "registrado". Poupa uma rodada inteira, que reenviaria tudo.
	l := llm.Limits{Free: free, Final: []string{findingTool}}
	switch {
	case m.agent == agentEditar:
	case m.level == levelRapido:
		l.ReadRounds = 1
	case m.task == taskExplicar:
		l.ReadRounds = 3
	default:
		l.ReadRounds = 2
	}
	return l
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
	// /funcao Nome, ou /funcao seguido do código colado: explica a função numa pergunta
	// avulsa e barata. Uma mensagem com uma função colada e "o que faz" vai pelo mesmo caminho.
	if f := fold(text); strings.HasPrefix(f, "/funcao") {
		arg := strings.TrimSpace(text[len(strings.Fields(text)[0]):])
		m.input.Reset()
		return m.explainFuncText(arg)
	}
	if f := fold(text); f == "/regras" || strings.HasPrefix(f, "/regras ") {
		m.input.Reset()
		return m.startRegras(strings.TrimSpace(text[len("/regras"):]))
	}
	if f := fold(text); strings.HasPrefix(f, "/onde ") || f == "/onde" || strings.HasPrefix(f, "/como ") || f == "/como" {
		kind := strings.TrimPrefix(strings.Fields(f)[0], "/")
		m.input.Reset()
		return m.startLocate(kind, strings.TrimSpace(text[len(strings.Fields(text)[0]):]))
	}
	switch fold(text) {
	case "/verificar", "/checar", "/lint":
		m.input.Reset()
		m.checksFor = m.target
		return m.startChecks(false, "", "")
	case "/commits", "/commit":
		m.input.Reset()
		return m.startCommitPlan()
	case "/pr":
		m.input.Reset()
		return m.startPRDescription()
	}
	if f := fold(text); f == "/corrigir" || f == "/pedido" {
		m.input.Reset()
		m.copyFixRequest(m.allFindings())
		return m, nil
	}
	if !strings.HasPrefix(text, "/") && asksAboutPastedFunc(text) {
		return m.explainFuncText(text)
	}
	// Comandos de modo no começo da mensagem; sem eles, a detecção pelo pedido.
	cmds, rest, unknown := parseCommands(text)
	if unknown != "" {
		m.setErr("comando desconhecido: " + unknown + " · digite / para ver os comandos")
		return m, nil
	}
	t, l := cmds.task, cmds.level
	if cmds.agent != nil && *cmds.agent != m.agent {
		m.setAgent(*cmds.agent)
		note := "permissão: ler · o modelo não altera arquivos"
		if m.agent == agentEditar {
			note = fmt.Sprintf("permissão: editar · editar arquivos: %s, criar: %s, apagar: %s · /permissoes muda",
				m.cfg.Permission("editar"), m.cfg.Permission("criar"), m.cfg.Permission("apagar"))
		}
		m.entries = append(m.entries, entry{kind: eNotice, text: note})
	}
	if cmds.perms {
		m.mode, m.permIdx = modePerms, 0
	}
	if cmds.mapa || cmds.tour { // o resto da mensagem é ignorado
		if cmds.mapa {
			return m.startMapScan()
		}
		return m.startTour()
	}
	explicit := t != nil || l != nil
	if !explicit {
		t, l = detectMode(rest)
	}
	before := modeLabel(m.task, m.level)
	if t != nil {
		m.task = *t
	}
	if l != nil {
		m.level = *l
	}
	if after := modeLabel(m.task, m.level); after != before {
		note := "modo: " + after
		if !explicit {
			note = "↻ modo detectado no seu pedido: " + after + " · /revisar, /explicar ou /normal para trocar"
		}
		m.entries = append(m.entries, entry{kind: eNotice, text: note})
	}
	if m.agent == agentLer && rest != "" && asksForEdit(rest) {
		m.entries = append(m.entries, entry{kind: eNotice,
			text: "você está no modo ler: o modelo só pode sugerir mudanças · /editar deixa ele aplicar, com a sua aprovação"})
	}
	if rest == "" { // só trocou o modo
		m.refreshChat()
		m.chatView.GotoBottom()
		return m, nil
	}
	text = rest
	if err := m.ensureChat(); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	*m.sendTo = m.target
	m.diffPending = ""
	checkBlock := ""
	if m.checkBlock != "" && !m.checkSent && m.task == taskRevisar {
		checkBlock, m.checkSent = m.checkBlock, true
	}
	prompt := m.contextBlock() + m.diffBlock(scope) + checkBlock + modeBlock(m.task, m.level, m.agentBlock()) + text
	m.entries = append(m.entries, entry{kind: eUser, text: text})
	m.streaming, m.thinking = true, false
	m.status = ""

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ctx = llm.WithLimits(ctx, m.turnLimits())
	ch := make(chan tea.Msg, 256)
	m.events = ch
	m.writes().startTurn(ch)
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
		return m.setFocus(pLog)
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
		if _, w := writeTools[ev.Call.Name]; w {
			break // o cartão da mudança vem do executor
		}
		if ev.Call.Name == findingTool {
			if f, err := parseFinding(ev.Call.Args); err == nil {
				n := 0
				if p, ok := m.resolveRefPath(f.Arquivo); ok {
					f.Arquivo = p
					n = m.addRef(p, f.Linha)
				}
				m.entries = append(m.entries, entry{kind: eFinding, f: &f, ref: n})
			}
			break
		}
		m.entries = append(m.entries, entry{kind: eTool, text: describeCall(ev.Call)})
	case llm.EvToolResult:
		if _, w := writeTools[ev.Call.Name]; w || ev.Call.Name == findingTool {
			break // o cartão já foi desenhado; um erro volta ao modelo, que corrige
		}
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
	isolated, purpose := m.isolated, m.purpose
	m.isolated, m.purpose = false, ""
	if m.approval != nil { // cancelada com uma proposta esperando
		m.approval = nil
		for i := range m.entries {
			if m.entries[i].kind == eChange && m.entries[i].chg.state == chPending {
				c := *m.entries[i].chg
				c.state, c.note = chDeclined, "resposta encerrada antes da aprovação"
				m.entries[i].chg = &c
			}
		}
	}
	if g := m.writes().endTurn(); len(g.changes) > 0 {
		m.undo = append(m.undo, g)
		m.entries = append(m.entries, entry{kind: eNotice,
			text: plural(len(g.changes), "mudança aplicada", "mudanças aplicadas") + " nesta resposta · ctrl+z desfaz"})
	}
	m.usage.Add(msg.usage)
	// Achados que o modelo escreveu como texto em vez de usar a ferramenta viram cartões.
	m.recoverTextFindings()
	// Ferramentas que ficaram sem resultado (cancelamento).
	for i := range m.entries {
		if m.entries[i].kind == eTool && !m.entries[i].done {
			m.entries[i].done = true
		}
	}
	if t := msg.usage.InputTokens + msg.usage.OutputTokens; t > 0 {
		m.entries = append(m.entries, entry{kind: eUsage, text: usageLine(msg.usage)})
	}
	switch {
	case msg.err == nil:
		m.status = ""
		if m.diffPending != "" {
			m.diffSent = m.diffPending
		}
		m.packed.commit()
		// Cada pergunta reenvia a conversa inteira: quando ela pesa, sugere começar outra.
		if avg := msg.usage.InputTokens / int64(max(msg.usage.Rounds, 1)); avg > 20000 && !m.tipShown {
			m.tipShown = true
			m.entries = append(m.entries, entry{kind: eNotice, text: fmt.Sprintf("dica: cada pergunta reenvia a conversa "+
				"inteira (~%s tokens por requisição agora); para um assunto novo, ctrl+n começa uma conversa limpa",
				humanTokens(avg))})
		}
		// Commit conversado conta como revisado no painel de commits (uma pergunta avulsa
		// sobre uma função, não).
		if !isolated && m.target != repo.WorkTree && m.reviewed != nil && !m.reviewed[m.target] {
			m.reviewed[m.target] = true
			saveReviewed(m.repo.Root, m.reviewed)
		}
	case msg.err == context.Canceled:
		m.packed.rollback()
		m.entries = append(m.entries, entry{kind: eNotice, text: "resposta cancelada"})
		m.status = ""
	default:
		m.packed.rollback()
		m.entries = append(m.entries, entry{kind: eError, text: msg.err.Error()})
		if strings.Contains(msg.err.Error(), "chave") {
			m.setErr("problema com a chave: pressione m e depois c para cadastrar outra")
		}
	}
	m.refreshChat()
	m.chatView.GotoBottom()
	m.saveTranscript()
	if purpose != "" && msg.err == nil {
		return m.onIsolatedDone(purpose)
	}
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
		case eFinding:
			b.WriteString(findingMarkdown(*e.f, e.ref) + "\n")
		case eChange:
			b.WriteString(changeMarkdown(*e.chg) + "\n")
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

// activityLine resume uma sequência de ações do modelo numa linha.
func (m model) activityLine(run []entry, w int) string {
	pending, failed := 0, 0
	var texts []string
	for _, e := range run {
		texts = append(texts, e.text)
		if !e.done {
			pending++
		} else if !e.ok {
			failed++
		}
	}
	if pending > 0 {
		return m.spin.View() + " " + subtleStyle.Render(truncateW(run[len(run)-1].text+"…", w-2))
	}
	mark := okStyle.Render("✓")
	if failed > 0 {
		mark = errStyle.Render("✗")
	}
	label := plural(len(run), "leitura", "leituras")
	if failed > 0 {
		label += fmt.Sprintf(" (%d com erro)", failed)
	}
	return mark + " " + subtleStyle.Render(truncateW(label+": "+strings.Join(texts, " · "), w-2))
}

// followedByFinding: depois desta resposta (pulando as leituras) vem um cartão de achado.
func (m model) followedByFinding(i int) bool {
	for j := i + 1; j < len(m.entries); j++ {
		switch m.entries[j].kind {
		case eTool:
			continue
		case eFinding:
			return true
		}
		return false
	}
	return false
}

func markdown(text string, width int) string {
	r, ok := mdRenderers[width]
	if !ok {
		style := "dark"
		if !darkBg {
			style = "light"
		}
		var err error
		r, err = glamour.NewTermRenderer(glamour.WithStyles(chatStyle(style == "dark")), glamour.WithWordWrap(width), glamour.WithEmoji())
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
	skip := -1
	for i := range m.entries {
		if i <= skip {
			continue
		}
		e := &m.entries[i]
		// Leituras seguidas do modelo viram uma linha só (ctrl+o mostra uma por linha).
		if e.kind == eTool && !m.showActivity {
			end := i
			for end+1 < len(m.entries) && m.entries[end+1].kind == eTool {
				end++
			}
			parts = append(parts, "  "+m.activityLine(m.entries[i:end+1], w-2))
			skip = end
			continue
		}
		switch e.kind {
		case eUser:
			parts = append(parts, "", selectedStyle.Render("você")+"\n"+lipgloss.NewStyle().Width(w).Render(e.text))
		case eAssistant:
			body := ""
			if m.streaming && i == last {
				body = lipgloss.NewStyle().Width(w).Render(e.text) // texto cru enquanto chega
			} else {
				if e.cacheW != w || e.cacheN != len(e.text) {
					e.cache, e.cacheW, e.cacheN = markdown(m.annotateRefs(e.text), w), w, len(e.text)
				}
				body = e.cache
			}
			head := ""
			if i == 0 || m.entries[i-1].kind == eUser {
				head = "\n" + titleStyle.Render(m.modelName) + "\n"
			}
			// O texto antes dos cartões de achados é o resumo da revisão.
			if m.followedByFinding(i) {
				head += lipgloss.NewStyle().Foreground(accent).Bold(true).Render("▍RESUMO") + "\n"
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
		case eFinding:
			parts = append(parts, "", renderFinding(*e.f, e.ref, w))
		case eChange:
			parts = append(parts, "", renderChange(*e.chg, w))
		}
	}
	if m.streaming && m.approval == nil && (last < 0 || m.entries[last].kind != eAssistant || m.thinking) {
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
		subtleStyle.Render("Pergunte sobre o commit ou o arquivo selecionado no Navegador."),
		subtleStyle.Render("r revisa · / comandos (/explicar, /editar, /mapa…) · o modelo abre os arquivos sozinho"),
	}
	return lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
}

// usageLine: "12,3 mil tokens · 2 rodadas (entrada 11 mil, 8 mil do cache · saída 1,3 mil)".
func usageLine(u llm.Usage) string {
	s := humanTokens(u.InputTokens+u.OutputTokens) + " tokens nesta resposta"
	if u.Rounds > 1 {
		s += fmt.Sprintf(" · %d rodadas", u.Rounds)
	}
	in := "entrada " + humanTokens(u.InputTokens)
	if u.CachedTokens > 0 {
		in += ", " + humanTokens(u.CachedTokens) + " do cache"
	}
	return s + " (" + in + " · saída " + humanTokens(u.OutputTokens) + ")"
}

// ---- diffs já enviados na conversa ----

// packedDiffs guarda os diffs por arquivo que já foram na mensagem, para o modelo não
// pagar por eles de novo com git_diff. É compartilhado com o executor (outra goroutine).
type packedDiffs struct {
	mu      sync.Mutex
	sent    map[string]bool
	pending []string // da mensagem em andamento: valem quando a resposta termina bem
}

func newPackedDiffs() *packedDiffs { return &packedDiffs{sent: map[string]bool{}} }

func (p *packedDiffs) has(k string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sent[k]
}

func (p *packedDiffs) commit() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.pending = nil
	p.mu.Unlock()
}

// rollback desfaz os da mensagem que falhou (ela sai do histórico).
func (p *packedDiffs) rollback() {
	if p == nil {
		return
	}
	p.mu.Lock()
	for _, k := range p.pending {
		delete(p.sent, k)
	}
	p.pending = nil
	p.mu.Unlock()
}

func (p *packedDiffs) reset() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.sent, p.pending = map[string]bool{}, nil
	p.mu.Unlock()
}

func (m *model) markPacked(path string) {
	if m.packed == nil {
		m.packed = newPackedDiffs()
	}
	k := m.target + "\x00" + path
	m.packed.mu.Lock()
	if !m.packed.sent[k] {
		m.packed.sent[k] = true
		m.packed.pending = append(m.packed.pending, k)
	}
	m.packed.mu.Unlock()
}

// withPacked responde a um git_diff de um arquivo cujo diff já foi na mensagem, sem
// repeti-lo. Com mais linhas de contexto do que o enviado, o diff é gerado normalmente.
func withPacked(p *packedDiffs, target *string, next llm.Executor) llm.Executor {
	return func(ctx context.Context, call llm.ToolCall) (string, bool) {
		if call.Name == "git_diff" {
			var a struct {
				Path    string `json:"path"`
				Ref     string `json:"ref"`
				Context int    `json:"context_lines"`
			}
			_ = json.Unmarshal([]byte(call.Args), &a)
			t := a.Ref
			if t == "" {
				t = *target
			}
			if a.Path != "" && a.Context <= repo.DiffContextLines && p.has(t+"\x00"+a.Path) {
				return fmt.Sprintf("O diff de %s já veio no <diff> de uma mensagem anterior desta conversa (com %d linhas "+
					"de contexto): use-o de lá. Para mais contexto, peça context_lines maior ou read_file com o trecho.",
					a.Path, repo.DiffContextLines), false
			}
		}
		return next(ctx, call)
	}
}

// chatStyle é o estilo do markdown no chat: o padrão do glamour, mas sem a margem (o
// painel já tem a dele), títulos sem "##" e na cor do dante, e código em linha sem fundo,
// só colorido (caminhos e funções aparecem muito e o fundo pesava).
func chatStyle(dark bool) ansi.StyleConfig {
	cfg := styles.LightStyleConfig
	head, code, sub := "57", "30", "240"
	if dark {
		cfg = styles.DarkStyleConfig
		head, code, sub = "141", "80", "248"
	}
	zero := uint(0)
	cfg.Document.Margin = &zero
	cfg.Heading.Color = &head
	cfg.H1 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &head, Bold: boolP(true)}}
	cfg.H2 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &head, Bold: boolP(true)}}
	cfg.H3 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &sub, Bold: boolP(true)}}
	cfg.H4 = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &sub, Bold: boolP(true)}}
	cfg.Code = ansi.StyleBlock{StylePrimitive: ansi.StylePrimitive{Color: &code}}
	return cfg
}

func boolP(b bool) *bool { return &b }
