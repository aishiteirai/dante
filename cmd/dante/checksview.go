package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aishiteirai/dante/internal/checks"
	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
)

// Verificações locais (/verificar e antes de cada revisão): o dante roda as ferramentas
// que o projeto já usa (Qlty, Biome, Oxlint, ESLint, tsc, ruff, go vet, golangci-lint,
// clippy) só nos arquivos alterados e mostra só os problemas que a mudança introduziu, como
// cartões, sem gastar tokens. Na revisão, eles vão ao modelo como fatos. Rodar essas
// ferramentas executa código do projeto, então o dante pede permissão por repositório.

type checksState struct {
	tools      []checks.Tool
	thenReview bool
	text       string // revisão a enviar depois
	scope      string
}

type checksDoneMsg struct {
	root, target string
	results      []checks.Result
	issues       []checks.Issue
	thenReview   bool
	text, scope  string
}

// checkFiles são os arquivos alterados que dá para verificar (sem os apagados), ou nil
// quando o alvo não é o código no disco.
func (m model) checkFiles() ([]string, string) {
	switch base, tip, ok := repo.SplitRange(m.target); {
	case m.target == repo.WorkTree:
	case ok && tip == m.branch:
		_ = base
	default:
		return nil, "as verificações rodam no código do disco: selecione as mudanças locais ou a branch atual × main"
	}
	var fs []string
	for _, f := range m.files {
		if f.Status != "D" && !f.Binary {
			fs = append(fs, f.Path)
		}
	}
	if len(fs) == 0 {
		return nil, "não há arquivos alterados para verificar"
	}
	return fs, ""
}

// startChecks roda as verificações (pedindo a permissão, na primeira vez neste repositório).
// thenReview: ao terminar, envia a revisão text/scope.
func (m model) startChecks(thenReview bool, text, scope string) (tea.Model, tea.Cmd) {
	files, why := m.checkFiles()
	if files == nil {
		if thenReview {
			return m.sendScoped(text, scope)
		}
		m.setInfo(why)
		return m, nil
	}
	tools := checks.Detect(m.repo.Root, files)
	if len(tools) == 0 {
		if thenReview {
			return m.sendScoped(text, scope)
		}
		m.setInfo("nenhuma ferramenta de verificação encontrada no projeto (Qlty, Biome, Oxlint, ESLint, tsc, ruff, go vet, golangci-lint, clippy)")
		return m, nil
	}
	m.checks = &checksState{tools: tools, thenReview: thenReview, text: text, scope: scope}
	if !m.cfg.ChecksAllowed(m.repo.Root) {
		m.mode = modeChecks
		return m, nil
	}
	return m.runChecks()
}

func (m model) runChecks() (tea.Model, tea.Cmd) {
	st := m.checks
	m.mode = modeNormal
	names := make([]string, len(st.tools))
	for i, t := range st.tools {
		names[i] = t.Name
	}
	m.setInfo("verificando com " + strings.Join(names, ", ") + "…")
	r, target, files := m.repo, m.target, m.files
	return m, tea.Batch(m.spin.Tick, func() tea.Msg {
		var results []checks.Result
		var all []checks.Issue
		for _, t := range st.tools {
			res := checks.Run(context.Background(), t)
			results = append(results, res)
			all = append(all, res.Issues...)
		}
		d, _ := r.DiffContext(target, "", 0)
		changed := checks.ChangedLines(d)
		for _, f := range files {
			if f.Status == "?" { // arquivo novo, fora do git: inteiro
				changed[f.Path] = map[int]bool{0: true}
			}
		}
		return checksDoneMsg{root: r.Root, target: target, results: results, issues: checks.OnlyNew(all, changed),
			thenReview: st.thenReview, text: st.text, scope: st.scope}
	})
}

func (m model) updateChecksConfirm(key string) (tea.Model, tea.Cmd) {
	st := m.checks
	switch key {
	case "s": // sempre neste repositório
		m.cfg.AllowChecks(m.repo.Root)
		_ = m.cfg.Save()
		return m.runChecks()
	case "u": // só desta vez
		return m.runChecks()
	case "n", "esc", "q":
		m.mode, m.checks = modeNormal, nil
		if st != nil && st.thenReview {
			return m.sendScoped(st.text, st.scope)
		}
	}
	return m, nil
}

func (m model) checksConfirmPopup() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("rodar as verificações do projeto?") + "\n\n")
	b.WriteString("O dante vai rodar, só nos arquivos alterados:\n\n")
	for _, t := range m.checks.tools {
		cmd := t.Command()
		if len(cmd) > 90 {
			cmd = cmd[:90] + "…"
		}
		b.WriteString("  " + boldStyle.Render(t.Name) + subtleStyle.Render("  "+cmd) + "\n")
	}
	b.WriteString("\n" + subtleStyle.Render("Essas ferramentas executam código do projeto (configurações, plugins).") + "\n")
	b.WriteString(subtleStyle.Render("Só permita em repositórios em que você confia. Nada é baixado.") + "\n\n")
	b.WriteString(okStyle.Bold(true).Render("s") + " sempre neste repositório   " + boldStyle.Render("u") + " só desta vez   " +
		subtleStyle.Render("n não"))
	return boxStyle.Padding(1, 2).Render(b.String())
}

const maxCheckCards = 30

func (m model) onChecksDone(msg checksDoneMsg) (tea.Model, tea.Cmd) {
	m.checks = nil
	if msg.root != m.repo.Root {
		return m, nil
	}
	var ran, broken []string
	for _, r := range msg.results {
		if r.Err != "" {
			broken = append(broken, r.Tool.Name+": "+r.Err)
			continue
		}
		ran = append(ran, fmt.Sprintf("%s (%s)", r.Tool.Name, r.Took.Round(100*time.Millisecond)))
	}
	summary := "verificações"
	if len(ran) > 0 {
		summary += ": " + strings.Join(ran, ", ")
	}
	if len(msg.issues) == 0 {
		summary += " · nenhum problema novo · sem gastar tokens"
	} else {
		summary += fmt.Sprintf(" · %s · sem gastar tokens", plural(len(msg.issues), "problema novo", "problemas novos"))
	}
	m.entries = append(m.entries, entry{kind: eNotice, text: summary})
	for _, b := range broken {
		m.entries = append(m.entries, entry{kind: eError, text: "não rodou: " + b})
	}
	for i, is := range msg.issues {
		if i == maxCheckCards {
			m.entries = append(m.entries, entry{kind: eNotice, text: fmt.Sprintf("… e mais %d", len(msg.issues)-i)})
			break
		}
		f := finding{Arquivo: is.Path, Linha: is.Line, Severidade: "baixa", Categoria: "qualidade", Titulo: shortText(is.Message, 100),
			Fonte: is.Tool}
		if is.Error {
			f.Severidade = "media"
		}
		if is.Rule != "" {
			f.Explicacao = "regra " + is.Rule
		}
		n := 0
		if p, ok := m.resolveRefPath(f.Arquivo); ok {
			f.Arquivo = p
			n = m.addRef(p, f.Linha)
		}
		m.entries = append(m.entries, entry{kind: eFinding, f: &f, ref: n})
	}
	m.checkBlock, m.checkSent = checksBlock(msg.issues), false
	m.refreshChat()
	m.chatView.GotoBottom()
	m.status = ""
	if msg.thenReview {
		return m.sendScoped(msg.text, msg.scope)
	}
	return m, nil
}

// checksBlock é o que vai ao modelo na revisão: os problemas como fatos.
func checksBlock(is []checks.Issue) string {
	if len(is) == 0 {
		return "<verificacoes>As ferramentas do projeto rodaram nos arquivos alterados e não apontaram nada novo.</verificacoes>\n\n"
	}
	var b strings.Builder
	b.WriteString("<verificacoes>\nO que as ferramentas do projeto já apontaram nas linhas alteradas (fatos: o desenvolvedor já vê " +
		"esses cartões; não os registre de novo, use-os para avaliar o impacto e priorizar):\n")
	for i, x := range is {
		if i == 20 {
			fmt.Fprintf(&b, "… e mais %d\n", len(is)-20)
			break
		}
		rule := ""
		if x.Rule != "" {
			rule = " [" + x.Rule + "]"
		}
		fmt.Fprintf(&b, "- %s %s:%d%s %s\n", x.Tool, x.Path, x.Line, rule, shortText(x.Message, 160))
	}
	b.WriteString("</verificacoes>\n\n")
	return b.String()
}

func shortText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
