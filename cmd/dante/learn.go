package main

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/aishiteirai/dante/internal/repo"
	"github.com/aishiteirai/dante/internal/tech"
	tea "github.com/charmbracelet/bubbletea"
)

// Aprendizado (fase 6): o dante ajuda a aprender as tecnologias pelo próprio projeto.
//   /tecnologias   o que o projeto usa, lido dos manifestos e do código (sem tokens)
//   /trilha NOME   roteiro para aprender uma tecnologia lendo os arquivos do projeto
//   /oque TERMO    o que é um termo, com exemplos achados no projeto
//   i (Visualizador) o que é a construção da linha marcada
// As três últimas vão numa pergunta à parte, com o contexto montado pelo dante.

const trailSystemPrompt = `Você é o dante e monta uma trilha de estudo: um roteiro para aprender uma tecnologia lendo o
próprio projeto da pessoa. Você recebe a tecnologia, os arquivos do projeto que a usam (com o esqueleto de cada um e as
linhas onde ela aparece) e as outras tecnologias do projeto. Use só isso. Responda em português, em markdown, para
quem nunca usou a tecnologia:
- primeira linha, em negrito: o que é a tecnologia e para que o projeto a usa, numa frase;
- **Conceitos:** de 3 a 5 ideias centrais, cada uma em uma linha, sem jargão sem explicação;
- **Roteiro:** de 4 a 7 passos numerados, do mais simples ao mais completo. Cada passo diz o que abrir
  (caminho/arquivo.ext:LINHA), o que observar ali e qual conceito ele mostra;
- **Para praticar:** um exercício pequeno neste projeto (uma mudança que a pessoa possa fazer e testar).
Sem introdução nem conclusão.`

const whatSystemPrompt = `Você é o dante e explica para quem está aprendendo o que é uma construção de código, um termo ou
uma biblioteca. Você recebe o termo ou o trecho, as tecnologias do projeto e, quando houver, onde aquilo aparece no
projeto. Responda em português, em markdown enxuto:
- primeira linha, em negrito: o que é, numa frase simples;
- **Como funciona:** de 2 a 4 tópicos curtos, com uma analogia se ajudar;
- **No projeto:** como ele aparece aqui, citando caminho/arquivo.ext:LINHA (só se houver exemplos);
- **Para saber mais:** o nome do conceito ou da documentação oficial a procurar.
Sem introdução nem conclusão. Se o trecho tiver várias coisas, explique a principal da linha marcada.`

type techReportMsg struct {
	rep  tech.Report
	show bool   // mostrar o relatório no chat
	then string // depois: "trilha:NOME" ou "oque:TERMO"
}

type learnPackMsg struct {
	label, system, prompt string
	err                   error
}

// withTechs garante o relatório de tecnologias (lido uma vez por repositório) e segue.
func (m model) withTechs(show bool, then string) (tea.Model, tea.Cmd) {
	if m.techs != nil && m.techsRoot == m.repo.Root {
		return m.onTechReport(techReportMsg{rep: *m.techs, show: show, then: then})
	}
	m.setInfo("lendo as tecnologias do projeto…")
	root := m.repo.Root
	return m, func() tea.Msg { return techReportMsg{rep: tech.Detect(root), show: show, then: then} }
}

func (m model) onTechReport(msg techReportMsg) (tea.Model, tea.Cmd) {
	rep := msg.rep
	m.techs, m.techsRoot = &rep, m.repo.Root
	if msg.show {
		m.entries = append(m.entries, entry{kind: eUser, text: "/tecnologias"}, entry{kind: eAssistant, text: rep.Markdown()})
		m.refreshChat()
		m.chatView.GotoBottom()
	}
	kind, arg, _ := strings.Cut(msg.then, ":")
	switch kind {
	case "trilha":
		return m.startTrail(rep, arg)
	case "oque":
		return m.startWhat(rep, arg)
	}
	return m, nil
}

// startTrail monta o contexto da trilha sem tokens e pergunta ao modelo.
func (m model) startTrail(rep tech.Report, name string) (tea.Model, tea.Cmd) {
	t, ok := rep.Find(name)
	if !ok || t.Uses == 0 && t.Kind != "linguagem" {
		var names []string
		for _, x := range rep.Techs {
			if x.Uses > 0 || x.Kind == "linguagem" {
				names = append(names, x.Name)
			}
		}
		what := "não achei " + name + " no projeto"
		if name == "" {
			what = "diga a tecnologia"
		} else if ok {
			what = t.Name + " está nos manifestos, mas não achei onde o código a usa"
		}
		m.setErr(fmt.Sprintf("%s · use /trilha NOME, com uma destas: %s", what, strings.Join(names, ", ")))
		return m, nil
	}
	if !m.canAsk() {
		return m, nil
	}
	m.setInfo("montando a trilha de " + t.Name + "…")
	r := m.repo
	return m, func() tea.Msg {
		return learnPackMsg{label: "/trilha " + t.Name, system: trailSystemPrompt, prompt: trailPrompt(r, rep, t)}
	}
}

func trailPrompt(r repo.Repo, rep tech.Report, t tech.Tech) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<tecnologia nome=%q pacote=%q versao=%q>%s</tecnologia>\n\n", t.Name, t.Package, t.Version, t.What)
	files := t.Files
	if t.Kind == "linguagem" { // linguagem: os arquivos principais do projeto, pelo mapa de uso das bibliotecas
		for _, x := range rep.Techs {
			for _, f := range x.Files {
				if !slices.Contains(files, f) && len(files) < 6 {
					files = append(files, f)
				}
			}
		}
	}
	// Os arquivos que mais usam a tecnologia (contando também os usos pelo nome curto, não só
	// o import) mostram melhor como ela funciona.
	names := tech.ImportNames(t)
	type fileUses struct {
		path  string
		lines []string
		uses  []string
	}
	var fus []fileUses
	for _, f := range files {
		lines, err := r.FileContent(repo.WorkTree, f)
		if err != nil {
			continue
		}
		fus = append(fus, fileUses{f, lines, usesIn(lines, t, names)})
	}
	sort.SliceStable(fus, func(i, j int) bool { return len(fus[i].uses) > len(fus[j].uses) })
	budget := 9000
	for i, fu := range fus {
		if i == 6 || budget <= 0 {
			break
		}
		out := repo.FileOutline(fu.path, fu.lines)
		if len(out) > 1500 {
			out = out[:1500] + "\n…"
		}
		block := fmt.Sprintf("<arquivo caminho=%q linhas=\"%d\" usos=\"%d\">\n<esqueleto>\n%s\n</esqueleto>\n", fu.path, len(fu.lines),
			len(fu.uses), strings.TrimSpace(out))
		if len(fu.uses) > 0 {
			shown := fu.uses
			if len(shown) > 8 {
				shown = shown[:8]
			}
			block += "<onde_aparece>\n" + strings.Join(shown, "\n") + "\n</onde_aparece>\n"
		}
		block += "</arquivo>\n\n"
		budget -= len(block)
		b.WriteString(block)
	}
	if c := rep.Compact(15); c != "" {
		b.WriteString("<outras_tecnologias>\n" + c + "</outras_tecnologias>\n\n")
	}
	fmt.Fprintf(&b, "Monte a trilha para aprender %s por este projeto.", t.Name)
	return b.String()
}

// usesIn acha as linhas onde a tecnologia aparece: o import e os usos pelo nome curto
// (em Go, também pelo apelido do import: tea "github.com/charmbracelet/bubbletea").
func usesIn(lines []string, t tech.Tech, names []string) []string {
	var out []string
	short := map[string]bool{}
	if s := path.Base(majorless(t.Package)); len(s) > 1 {
		short[strings.ReplaceAll(s, "-", "_")] = true
	}
	for n, l := range lines {
		hit := false
		for _, imp := range names {
			if strings.Contains(l, imp) {
				hit = true
				if f := strings.Fields(strings.TrimSpace(l)); len(f) == 2 && strings.HasPrefix(f[1], `"`) {
					short[f[0]] = true // apelido do import em Go
				}
				if _, alias, ok := strings.Cut(l, " as "); ok { // import numpy as np
					short[strings.TrimSpace(strings.Trim(alias, ",;"))] = true
				}
				break
			}
		}
		if !hit {
			for s := range short {
				if strings.Contains(l, s+".") {
					hit = true
					break
				}
			}
		}
		if hit {
			out = append(out, fmt.Sprintf("%d: %s", n+1, strings.TrimSpace(l)))
		}
	}
	return out
}

func majorless(p string) string {
	if i := strings.LastIndex(p, "/v"); i > 0 && strings.Trim(p[i+2:], "0123456789") == "" {
		return p[:i]
	}
	return p
}

// startWhat explica um termo com exemplos achados no projeto.
func (m model) startWhat(rep tech.Report, term string) (tea.Model, tea.Cmd) {
	term = strings.TrimSpace(term)
	if term == "" {
		m.setInfo("use /oque e o termo, ex.: /oque goroutine, ou marque uma linha no Visualizador e aperte i")
		return m, nil
	}
	if !m.canAsk() {
		return m, nil
	}
	r := m.repo
	return m, func() tea.Msg {
		var b strings.Builder
		if strings.Contains(term, "\n") {
			fmt.Fprintf(&b, "<trecho>\n%s\n</trecho>\n\n", term)
		} else {
			fmt.Fprintf(&b, "<termo>%s</termo>\n\n", term)
			if t, ok := rep.Find(term); ok && t.What != "" {
				fmt.Fprintf(&b, "<no_catalogo>%s: %s</no_catalogo>\n\n", t.Name, t.What)
			}
			if hits := r.Locate(strings.Fields(term), 3); len(hits) > 0 {
				b.WriteString("<exemplos_no_projeto>\n")
				for _, h := range hits {
					for i, l := range h.Lines {
						if i == 3 {
							break
						}
						fmt.Fprintf(&b, "%s:%d  %s\n", l.Path, l.Line, strings.TrimSpace(l.Text))
					}
				}
				b.WriteString("</exemplos_no_projeto>\n\n")
			}
		}
		b.WriteString("<tecnologias_do_projeto>\n" + rep.Compact(15) + "</tecnologias_do_projeto>\n\n")
		b.WriteString("O que é isso?")
		label := "o que é " + term + "?"
		if strings.Contains(term, "\n") {
			label = "o que é este trecho?"
		}
		return learnPackMsg{label: label, system: whatSystemPrompt, prompt: b.String()}
	}
}

// whatAt explica a construção da linha marcada no Visualizador, com algumas linhas em volta.
func (m model) whatAt(path, ref string, line int) (tea.Model, tea.Cmd) {
	if !m.canAsk() {
		return m, nil
	}
	if m.techs == nil || m.techsRoot != m.repo.Root {
		rep := tech.Detect(m.repo.Root)
		m.techs, m.techsRoot = &rep, m.repo.Root
	}
	rep, r := *m.techs, m.repo
	return m, func() tea.Msg {
		lines, err := r.FileContent(ref, path)
		if err != nil {
			return learnPackMsg{err: err}
		}
		if line > len(lines) {
			line = len(lines)
		}
		from, to := max(line-8, 1), min(line+8, len(lines))
		var b strings.Builder
		fmt.Fprintf(&b, "<trecho arquivo=%q linha_marcada=\"%d\">\n", path, line)
		for i := from; i <= to; i++ {
			mark := "  "
			if i == line {
				mark = "▶ "
			}
			fmt.Fprintf(&b, "%s%5d  %s\n", mark, i, lines[i-1])
		}
		b.WriteString("</trecho>\n\n<tecnologias_do_projeto>\n" + rep.Compact(15) + "</tecnologias_do_projeto>\n\n")
		fmt.Fprintf(&b, "O que é a construção da linha %d (marcada com ▶) e por que ela está ali?", line)
		return learnPackMsg{label: fmt.Sprintf("o que é isso? (%s:%d)", path, line), system: whatSystemPrompt, prompt: b.String()}
	}
}

func (m model) onLearnPack(msg learnPackMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setErr(msg.err.Error())
		return m, nil
	}
	m.input.Reset()
	return m.askIsolated(msg.system, msg.label, msg.prompt, "")
}
