package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

// "O que faz esta função?" numa requisição só. O dante acha a função (pela linha marcada
// no Visualizador, pelo nome ou pelo código colado), junta o corpo, onde ela é usada e o
// que ela chama, sem gastar tokens, e pergunta ao modelo numa conversa à parte: sem
// ferramentas e sem o histórico do chat. Fica em poucos milhares de tokens.

const funcSystemPrompt = `Você é o dante e explica uma função de um repositório para quem vai trabalhar com ela.
Você recebe o código da função (com números de linha), onde ela é usada no projeto e as funções do projeto que ela
chama. Use só isso: se algo depender de código que não está aqui, diga. Responda em português, em markdown enxuto:
- primeira linha, em negrito: o que a função faz, numa frase;
- **Como funciona:** de 3 a 6 tópicos curtos, citando caminho/arquivo.ext:LINHA;
- **Onde é usada:** como ela afeta o projeto, a partir dos usos (quem chama e para quê);
- **Cuidados:** efeitos colaterais, erros e casos de borda, só se houver.
Sem introdução nem conclusão.`

type funcPackMsg struct {
	info    repo.FuncInfo
	version string // "o disco" ou "o commit abc1234"
	pasted  bool   // código colado que não foi achado no repositório
	err     error
}

// explainFuncAt explica a função que contém a linha do arquivo, na versão do alvo.
func (m model) explainFuncAt(path, ref string, line int) (tea.Model, tea.Cmd) {
	if !m.canAsk() {
		return m, nil
	}
	m.setInfo(fmt.Sprintf("lendo a função em %s:%d…", path, line))
	r := m.repo
	return m, func() tea.Msg {
		fi, err := r.FuncAt(ref, path, line)
		return funcPackMsg{info: fi, version: versionLabel(ref), err: err}
	}
}

// explainFuncText explica uma função pelo nome ou pelo código colado.
func (m model) explainFuncText(arg string) (tea.Model, tea.Cmd) {
	if !m.canAsk() {
		return m, nil
	}
	arg = strings.Trim(strings.TrimSpace(arg), "`")
	name := repo.DeclaredName(arg)
	if name == "" && identRe.MatchString(arg) {
		name = arg
	}
	if name == "" {
		m.setErr("não achei uma função: use /funcao NomeDaFuncao, ou cole o código dela depois do comando")
		return m, nil
	}
	m.setInfo("procurando " + name + "…")
	r, ref := m.repo, repo.Tip(m.target)
	pasted := strings.Contains(arg, "\n") || strings.Contains(arg, "(")
	return m, func() tea.Msg {
		for _, sha := range []string{ref, repo.WorkTree} {
			if p, l, ok := r.FindFunc(sha, name); ok {
				fi, err := r.FuncAt(sha, p, l)
				return funcPackMsg{info: fi, version: versionLabel(sha), err: err}
			}
		}
		if !pasted {
			return funcPackMsg{err: fmt.Errorf("não achei a função %s no repositório; cole o código dela depois de /funcao", name)}
		}
		// Código colado que não está no repositório: vai como veio, com os usos pelo nome.
		var b strings.Builder
		for i, l := range strings.Split(strings.TrimRight(arg, "\n"), "\n") {
			fmt.Fprintf(&b, "%5d  %s\n", i+1, l)
		}
		code, _ := repo.Redact(b.String())
		fi := repo.FuncInfo{Name: name, Path: "(colado)", Start: 1, Code: code}
		fi.Usages, fi.MoreUsages = r.UsagesOf(repo.WorkTree, name)
		return funcPackMsg{info: fi, version: "o código colado", pasted: true}
	}
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

func versionLabel(ref string) string {
	if ref == repo.WorkTree {
		return "o disco"
	}
	return "o commit " + shortSHA(ref)
}

func (m *model) canAsk() bool {
	switch {
	case m.streaming:
		m.setInfo("aguarde a resposta terminar (esc cancela)")
		return false
	case m.modelName == "":
		m.setErr("escolha um modelo (tecla m)")
		return false
	}
	return true
}

// funcPrompt monta a pergunta com o pacote da função.
func (m model) funcPrompt(msg funcPackMsg) string {
	fi := msg.info
	var b strings.Builder
	what := "a função " + fi.Name
	if fi.Whole {
		what = "o trecho"
	}
	fmt.Fprintf(&b, "<funcao nome=%q arquivo=%q linhas=\"%d-%d\" versao=%q>\n%s</funcao>\n\n",
		fi.Name, fi.Path, fi.Start, fi.End, msg.version, fi.Code)
	if len(fi.Usages) > 0 {
		b.WriteString("<onde_e_usada>\n")
		for _, u := range fi.Usages {
			fmt.Fprintf(&b, "- %s:%d  %s\n", u.Path, u.Line, u.Text)
		}
		if fi.MoreUsages > 0 {
			fmt.Fprintf(&b, "… e mais %d usos\n", fi.MoreUsages)
		}
		b.WriteString("</onde_e_usada>\n\n")
	} else if fi.Name != "" {
		b.WriteString("<onde_e_usada>nenhum uso encontrado no repositório (pode ser chamada por fora, por reflexão ou estar sem uso)</onde_e_usada>\n\n")
	}
	if len(fi.Calls) > 0 {
		b.WriteString("<o_que_ela_chama>\n")
		for _, c := range fi.Calls {
			fmt.Fprintf(&b, "- %s:%d  %s\n", c.Path, c.Line, c.Text)
		}
		b.WriteString("</o_que_ela_chama>\n\n")
	}
	switch m.level {
	case levelAprendiz:
		b.WriteString("Público: alguém que está aprendendo. Evite jargão e explique os termos técnicos em poucas palavras.\n")
	case levelRapido:
		b.WriteString("Formato: rápido. Até 5 linhas, sem rótulos.\n")
	}
	fmt.Fprintf(&b, "O que faz %s e como ela afeta o projeto?", what)
	return b.String()
}

func (m model) onFuncPack(msg funcPackMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setErr(msg.err.Error())
		return m, nil
	}
	fi := msg.info
	label := fmt.Sprintf("o que faz `%s`? (%s:%d)", fi.Name, fi.Path, fi.Start)
	if fi.Whole {
		label = fmt.Sprintf("o que faz o trecho %s:%d-%d?", fi.Path, fi.Start, fi.End)
	}
	if msg.pasted {
		label = fmt.Sprintf("o que faz `%s`? (código colado)", fi.Name)
	}
	return m.askIsolated(funcSystemPrompt, label, m.funcPrompt(msg), "")
}

// askIsolated faz uma pergunta numa conversa à parte (sem ferramentas e sem o histórico):
// a resposta aparece no chat, mas não entra na conversa principal.
//
// purpose diz o que fazer com a resposta quando ela terminar ("commits": ler o plano; "pr":
// copiar a descrição; "": nada).
func (m model) askIsolated(system, label, prompt, purpose string) (tea.Model, tea.Cmd) {
	effort := ""
	if m.prov.Tipo == llm.KindAnthropic {
		effort = "low"
	}
	chat, err := llm.New(m.prov.LLM(), llm.Options{Model: m.modelName, System: system, Effort: effort,
		ContextTokens: m.effectiveContext(), MaxSteps: 1})
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.isolated, m.purpose = true, purpose
	m.entries = append(m.entries, entry{kind: eUser, text: label})
	m.streaming, m.thinking = true, false
	m.status = ""
	if m.convStart.IsZero() {
		m.convStart = time.Now()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := make(chan tea.Msg, 256)
	m.events = ch
	go func() {
		u, err := chat.Send(ctx, prompt, func(ev llm.Event) { ch <- chatEventMsg{ev} })
		ch <- chatDoneMsg{usage: u, err: err}
		close(ch)
	}()
	fm, cmd := m.focusPane(pChat)
	mm := fm.(model)
	mm.refreshChat()
	mm.chatView.GotoBottom()
	return mm, tea.Batch(cmd, waitFor(ch), mm.spin.Tick)
}

// ---- no Visualizador: f explica a função da linha marcada ----

// funcLocation diz o arquivo, a versão e a linha para o f no Visualizador.
func (m model) funcLocation() (path, ref string, line int, ok bool) {
	switch m.vtab {
	case vtDiff:
		if m.diffPath == "" {
			return "", "", 0, false
		}
		path, ref, line = m.diffPath, repo.Tip(m.target), m.markLine
		if line <= 0 {
			if h := m.currentHunk(); h < len(m.hunkLines) {
				line = m.hunkLines[h] + 3 // o trecho começa com 3 linhas de contexto
			}
		}
		return path, ref, max(line, 1), true
	case vtDoc:
		if m.docIsDiff {
			return "", "", 0, false
		}
		path, ref, ok = docLocation(m.docTitle, repo.Tip(m.target))
		line = m.markLine
		if line <= 0 && m.preview.YOffset < len(m.rowLines) {
			line = m.rowLines[m.preview.YOffset] // a primeira linha visível
		}
		return path, ref, max(line, 1), ok
	}
	return "", "", 0, false
}

// docLocation lê o arquivo e a versão do título da aba Documento: "a.go", "a.go @ abc1234",
// "a.go:12 · referência [3]" ou "… · versão atual (não existe neste commit)".
func docLocation(title, target string) (string, string, bool) {
	if title == "" || strings.Contains(title, "abertos pelo modelo") {
		return "", "", false
	}
	first, _, _ := strings.Cut(title, " ")
	if i := strings.LastIndex(first, ":"); i > 0 {
		first = first[:i]
	}
	ref := repo.WorkTree
	switch {
	case strings.Contains(title, " @ "):
		ref = strings.Fields(title[strings.Index(title, " @ ")+3:])[0]
	case strings.Contains(title, "referência") && !strings.Contains(title, "versão atual"):
		ref = target
	}
	return first, ref, first != ""
}

// clickLine marca a linha clicada no Visualizador (para o f).
func (m *model) clickLine(y int) {
	r, ok := m.rects[pDiff]
	if !ok {
		return
	}
	idx := y - r.y - 1 - vHeaderLines + m.preview.YOffset
	if idx >= 0 && idx < len(m.rowLines) && m.rowLines[idx] > 0 {
		m.markLine = m.rowLines[idx]
		m.renderPreview()
		m.setInfo(fmt.Sprintf("linha %d marcada · f explica a função dela", m.markLine))
	}
}

// asksAboutPastedFunc: a mensagem traz o código de uma função e pergunta o que ela faz.
func asksAboutPastedFunc(text string) bool {
	if strings.Count(text, "\n") < 2 || repo.DeclaredName(text) == "" {
		return false
	}
	t, _ := detectMode(text)
	return (t != nil && *t == taskExplicar) || strings.Contains(fold(text), "o que") || strings.Contains(text, "?")
}
