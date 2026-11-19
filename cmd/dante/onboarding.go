package main

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

// Fase 3: chegar num projeto novo. O explorador mostra o repositório inteiro (p no
// Navegador), x explica um arquivo, /onde diz onde mexer para uma tarefa (com um briefing
// pronto para o Claude) e /como explica uma funcionalidade de ponta a ponta. O contexto é
// montado pelo dante (git grep, esqueleto, mapa); o modelo responde numa pergunta avulsa.

// ---- explorador do projeto ----

type projTree struct {
	files []string
	open  map[string]bool // pastas abertas
	idx   int
	off   int
}

type projNode struct {
	path  string
	name  string
	dir   bool
	depth int
}

type projMsg struct {
	files []string
	err   error
}

func (m model) toggleProject() (tea.Model, tea.Cmd) {
	m.navProject = !m.navProject
	if !m.navProject {
		return m, nil
	}
	if m.proj != nil {
		return m, nil
	}
	r := m.repo
	return m, func() tea.Msg {
		fs, err := r.ListFiles()
		return projMsg{files: fs, err: err}
	}
}

func (m model) onProject(msg projMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setErr(msg.err.Error())
		return m, nil
	}
	sort.Strings(msg.files)
	m.proj = &projTree{files: msg.files, open: map[string]bool{}}
	return m, nil
}

// nodes é a árvore visível: pastas primeiro, depois arquivos, só dentro das pastas abertas.
func (t *projTree) nodes() []projNode {
	children := map[string][]string{} // pasta → filhos diretos (caminhos completos)
	isDir := map[string]bool{}
	for _, f := range t.files {
		parts := strings.Split(f, "/")
		parent := ""
		for i := range parts {
			p := strings.Join(parts[:i+1], "/")
			if i < len(parts)-1 {
				if !isDir[p] {
					isDir[p] = true
					children[parent] = append(children[parent], p)
				}
			} else {
				children[parent] = append(children[parent], p)
			}
			parent = p
		}
	}
	var out []projNode
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		kids := children[dir]
		sort.SliceStable(kids, func(i, j int) bool {
			if isDir[kids[i]] != isDir[kids[j]] {
				return isDir[kids[i]]
			}
			return kids[i] < kids[j]
		})
		for _, k := range kids {
			out = append(out, projNode{path: k, name: path.Base(k), dir: isDir[k], depth: depth})
			if isDir[k] && t.open[k] {
				walk(k, depth+1)
			}
		}
	}
	walk("", 0)
	return out
}

func (m model) viewProject(w, h int) string {
	t := m.proj
	if t == nil {
		return subtleStyle.Render("carregando…")
	}
	ns := t.nodes()
	t.idx = min(t.idx, max(len(ns)-1, 0))
	t.off = scrollTo(t.idx, t.off, h)
	var b strings.Builder
	for i := t.off; i < len(ns) && i < t.off+h; i++ {
		n := ns[i]
		line := strings.Repeat("  ", n.depth)
		switch {
		case n.dir && t.open[n.path]:
			line += subtleStyle.Render("▾ ") + boldStyle.Render(n.name+"/")
		case n.dir:
			line += subtleStyle.Render("▸ ") + n.name + "/"
		default:
			line += "  " + n.name
		}
		line = truncateW(line, w)
		if i == t.idx {
			line = cursorBg.Render(padTo(line, w))
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m model) updateProjectKeys(key string) (tea.Model, tea.Cmd) {
	t := m.proj
	if t == nil {
		if key == "p" || key == "esc" {
			m.navProject = false
		}
		return m, nil
	}
	ns := t.nodes()
	if len(ns) == 0 {
		return m, nil
	}
	cur := ns[min(t.idx, len(ns)-1)]
	moved := false
	switch key {
	case "p", "esc":
		m.navProject = false
		return m, nil
	case "up", "k":
		t.idx, moved = max(t.idx-1, 0), true
	case "down", "j":
		t.idx, moved = min(t.idx+1, len(ns)-1), true
	case "pgup":
		t.idx, moved = max(t.idx-m.logHeight()/2, 0), true
	case "pgdown":
		t.idx, moved = min(t.idx+m.logHeight()/2, len(ns)-1), true
	case "right", "l", "enter":
		if cur.dir {
			t.open[cur.path] = !t.open[cur.path] || key != "enter"
			return m, nil
		}
		return m.focusPane(pDiff)
	case "left", "h":
		if cur.dir && t.open[cur.path] {
			t.open[cur.path] = false
			return m, nil
		}
		parent := path.Dir(cur.path)
		for i, n := range ns {
			if n.path == parent {
				t.idx = i
			}
		}
		return m, nil
	case "x":
		if !cur.dir {
			return m.explainFile(cur.path, repo.WorkTree)
		}
	}
	if moved {
		// Como no diff, o Visualizador acompanha o cursor: o arquivo abre na aba Arquivo.
		if n := ns[t.idx]; !n.dir {
			return m.openFileTab(n.path, repo.WorkTree, 0)
		}
	}
	return m, nil
}

// ---- x: o que é este arquivo? ----

const fileSystemPrompt = `Você apresenta um arquivo de um repositório a alguém que acabou de chegar no projeto.
Recebe o esqueleto do arquivo (declarações com o número da linha), o começo dele e onde outros arquivos o usam.
Use só isso. Responda em português, em markdown enxuto:
- primeira linha, em negrito: para que serve o arquivo, numa frase;
- **O que tem aqui:** as peças principais (tipos e funções), cada uma com caminho:LINHA e o papel dela;
- **Onde se encaixa:** quem usa este arquivo e para quê;
- **Por onde ler:** qual função ou trecho ler primeiro.
Sem introdução nem conclusão.`

type filePackMsg struct {
	label, prompt string
	err           error
}

func (m model) explainFile(p, ref string) (tea.Model, tea.Cmd) {
	if !m.canAsk() {
		return m, nil
	}
	r := m.repo
	m.setInfo("lendo " + p + "…")
	return m, func() tea.Msg {
		lines, err := r.FileContent(ref, p)
		if err != nil {
			return filePackMsg{err: err}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "<arquivo caminho=%q linhas=\"%d\">\n", p, len(lines))
		if o := repo.FileOutline(p, lines); o != "" {
			b.WriteString("<esqueleto>\n" + o + "\n</esqueleto>\n")
		}
		b.WriteString("<comeco>\n")
		for i, l := range lines[:min(len(lines), 40)] {
			fmt.Fprintf(&b, "%5d  %s\n", i+1, l)
		}
		b.WriteString("</comeco>\n</arquivo>\n\n")
		if us := r.FileUsers(ref, p, 8); len(us) > 0 {
			b.WriteString("<quem_usa>\n")
			for _, u := range us {
				fmt.Fprintf(&b, "- %s:%d  %s\n", u.Path, u.Line, u.Text)
			}
			b.WriteString("</quem_usa>\n\n")
		}
		b.WriteString("Para que serve este arquivo e como ele se encaixa no projeto?")
		return filePackMsg{label: "o que é " + p + "?", prompt: b.String()}
	}
}

func (m model) onFilePack(msg filePackMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setErr(msg.err.Error())
		return m, nil
	}
	return m.askIsolated(fileSystemPrompt, msg.label, msg.prompt, "")
}

// ---- /onde e /como ----

const termsSystemPrompt = `Você gera termos de busca para achar código num repositório. Responda só com os termos,
separados por vírgula, sem explicação.`

const whereSystemPrompt = `Você orienta alguém que acabou de chegar num projeto e vai pedir uma tarefa a um agente de
código (como o Claude Code). Recebe a tarefa, trechos do repositório encontrados por busca e, às vezes, um resumo
do projeto. Use só isso. Responda em português, enxuto:
- **Onde mexer:** os arquivos e funções, na ordem de leitura, cada um com caminho:LINHA e o porquê;
- **O que já existe parecido:** código que serve de modelo para a tarefa, se houver;
- **Cuidados:** o que pode quebrar e quais testes ajustar.
Por fim, um bloco ` + "```briefing" + ` com um pedido pronto para colar no agente: o objetivo, onde mexer (caminhos), o
padrão a seguir, o que não mexer e como testar. Se os trechos não bastarem para saber onde mexer, diga e sugira o que
procurar.`

const howSystemPrompt = `Você explica como uma funcionalidade funciona de ponta a ponta para alguém que acabou de chegar
no projeto. Recebe trechos do repositório encontrados por busca e, às vezes, um resumo do projeto. Use só isso.
Responda em português, enxuto:
- primeira linha, em negrito: o que a funcionalidade faz, numa frase;
- **Caminho:** passo a passo numerado, do começo ao fim, cada passo com caminho:LINHA e o que acontece;
- **Peças principais:** os tipos e funções que importam;
- **Para ir além:** o que ler depois.
Se os trechos não bastarem para seguir o caminho, diga o que falta.`

type locatePackMsg struct {
	kind   string // "onde" ou "como"
	task   string
	terms  []string
	prompt string
	usage  llm.Usage
	err    error
}

// startLocate roda /onde ou /como: termos de busca (pergunta pequena), busca no código
// (sem o modelo) e a pergunta com os trechos encontrados.
func (m model) startLocate(kind, task string) (tea.Model, tea.Cmd) {
	task = strings.TrimSpace(task)
	if task == "" {
		if kind == "onde" {
			m.setInfo("use /onde e descreva a tarefa, ex.: /onde adicionar filtro de data na listagem de pedidos")
		} else {
			m.setInfo("use /como e diga a funcionalidade, ex.: /como funciona o login")
		}
		return m, nil
	}
	if !m.canAsk() {
		return m, nil
	}
	chat, err := llm.New(m.prov.LLM(), llm.Options{Model: m.modelName, System: termsSystemPrompt, MaxSteps: 1,
		MaxTokens: 400, ContextTokens: m.effectiveContext()})
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	m.setInfo("procurando no código…")
	r := m.repo
	compact := ""
	if m.mapData != nil {
		compact = m.mapData.Compact(3000)
	}
	return m, func() tea.Msg {
		files, _ := r.ListFiles()
		var q strings.Builder
		fmt.Fprintf(&q, "Tarefa: %s\n\nPastas do projeto:\n%s\n\n", task, folderList(files, 60))
		q.WriteString("Liste de 6 a 12 termos de busca para achar no código onde isso acontece: nomes prováveis de " +
			"funções, tipos, arquivos, rotas ou textos da interface, em inglês e em português.")
		var b strings.Builder
		u, err := chat.Send(context.Background(), q.String(), func(ev llm.Event) {
			if ev.Kind == llm.EvText {
				b.WriteString(ev.Text)
			}
		})
		if err != nil {
			return locatePackMsg{err: err}
		}
		terms := parseTerms(b.String())
		hits := r.Locate(terms, 8)
		var p strings.Builder
		fmt.Fprintf(&p, "Tarefa: %s\n\n", task)
		if compact != "" {
			p.WriteString("<resumo_do_projeto>\n" + compact + "\n</resumo_do_projeto>\n\n")
		}
		if len(hits) == 0 {
			p.WriteString("<trechos>nenhum trecho encontrado para os termos: " + strings.Join(terms, ", ") + "</trechos>\n\n")
		} else {
			p.WriteString("<trechos>\n")
			for i, h := range hits {
				fmt.Fprintf(&p, "## %s\n", h.Path)
				for _, l := range h.Lines {
					fmt.Fprintf(&p, "%d: %s\n", l.Line, l.Text)
				}
				if i < 3 { // os mais relevantes também com o esqueleto
					if ls, err := r.FileContent(repo.WorkTree, h.Path); err == nil {
						if o := repo.FileOutline(h.Path, ls); o != "" {
							p.WriteString("esqueleto:\n" + firstLines(o, 25) + "\n")
						}
					}
				}
				p.WriteString("\n")
			}
			p.WriteString("</trechos>\n\n")
		}
		if kind == "onde" {
			p.WriteString("Onde mexer para fazer esta tarefa?")
		} else {
			p.WriteString("Como isto funciona de ponta a ponta?")
		}
		prompt, _ := repo.Redact(p.String())
		return locatePackMsg{kind: kind, task: task, terms: terms, prompt: prompt, usage: u}
	}
}

func (m model) onLocatePack(msg locatePackMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setErr("não consegui buscar: " + msg.err.Error())
		return m, nil
	}
	m.usage.Add(msg.usage)
	m.entries = append(m.entries, entry{kind: eNotice, text: fmt.Sprintf("termos de busca (%s tokens): %s",
		humanTokens(msg.usage.InputTokens+msg.usage.OutputTokens), strings.Join(msg.terms, ", "))})
	if msg.kind == "onde" {
		return m.askIsolated(whereSystemPrompt, "onde mexo para: "+msg.task, msg.prompt, "onde")
	}
	return m.askIsolated(howSystemPrompt, "como funciona: "+msg.task, msg.prompt, "")
}

var briefingBlock = regexp.MustCompile("(?s)```briefing\\s*\\n(.*?)```")

// copyBriefing copia o briefing da resposta do /onde.
func (m *model) copyBriefing(text string) {
	sm := briefingBlock.FindStringSubmatch(text)
	if sm == nil {
		return
	}
	b := strings.TrimSpace(sm[1])
	termenv.Copy(b)
	p := filepath.Join(dataHome(), "pedidos", "briefing.md")
	if os.MkdirAll(filepath.Dir(p), 0o755) == nil && os.WriteFile(p, []byte(b+"\n"), 0o644) == nil {
		m.setInfo("briefing copiado: cole no Claude · também em " + prettyPath(p))
	}
}

func parseTerms(s string) []string {
	s = strings.NewReplacer("\n", ",", ";", ",", "`", "", "*", "", "\"", "").Replace(s)
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(t), "-•0123456789. "))
		if len(t) >= 3 && len(t) <= 40 && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			out = append(out, t)
		}
	}
	return out[:min(len(out), 12)]
}

// folderList resume as pastas do projeto (até dois níveis), com quantos arquivos têm.
func folderList(files []string, n int) string {
	count := map[string]int{}
	for _, f := range files {
		d := path.Dir(f)
		if parts := strings.Split(d, "/"); len(parts) > 2 {
			d = strings.Join(parts[:2], "/")
		}
		count[d]++
	}
	var ds []string
	for d := range count {
		ds = append(ds, d)
	}
	sort.Strings(ds)
	var b strings.Builder
	for i, d := range ds {
		if i == n {
			fmt.Fprintf(&b, "… e mais %d pastas\n", len(ds)-n)
			break
		}
		fmt.Fprintf(&b, "%s (%d)\n", d, count[d])
	}
	return strings.TrimSpace(b.String())
}

func firstLines(s string, n int) string {
	ls := strings.Split(s, "\n")
	return strings.Join(ls[:min(len(ls), n)], "\n")
}
