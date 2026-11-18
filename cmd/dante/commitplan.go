package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/aishiteirai/dante/internal/repo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Fase 2: organizar as mudanças locais em commits e escrever a descrição do PR. O dante
// monta o contexto sem o modelo (arquivos, diff, estilo dos commits recentes) e faz uma
// pergunta avulsa, sem ferramentas. O modelo só sugere: quem roda o git é o dante, quando a
// pessoa aperta a tecla. Nesta versão, a divisão é por arquivo.

const commitsSystemPrompt = `Você organiza mudanças locais de um repositório em commits. Responda só com um bloco
` + "```json" + ` neste formato, sem texto fora dele:
{"commits":[{"mensagem":"tipo(escopo): resumo","corpo":"por quê, em 1 a 3 linhas","arquivos":["caminho"]}]}
Regras:
- Conventional Commits: feat, fix, refactor, perf, docs, test, build, ci, chore, style. O escopo é opcional.
- Siga o idioma e o estilo dos commits recentes do repositório. Resumo no imperativo, até 72 caracteres.
- Um commit por assunto, e cada um deve fazer sentido sozinho; testes vão junto do código que testam.
- Cada arquivo alterado entra em exatamente um commit. Ordem: o que os outros usam vem antes.
- O corpo diz o porquê da mudança (não repita a lista de arquivos). Pode ficar vazio em mudanças óbvias.`

const prSystemPrompt = `Você escreve a descrição de um pull request, em português, em markdown, a partir dos commits
e do resumo dos arquivos. Formato:
# título curto (no estilo dos commits)
## O que muda
(3 a 6 tópicos, por assunto, não por arquivo)
## Por quê
## Como testar
(passos concretos)
## Riscos
(só se houver: o que pode quebrar, migrações, mudanças de comportamento)
Não invente o que não está nos commits; sem introdução nem conclusão.`

type plannedCommit struct {
	Mensagem string   `json:"mensagem"`
	Corpo    string   `json:"corpo"`
	Arquivos []string `json:"arquivos"`
	done     bool
	err      string
}

type commitPlan struct {
	commits  []plannedCommit
	missing  []string // arquivos alterados que o plano não pôs em nenhum commit
	editing  string   // arquivo temporário aberto no editor
	editIdx  int
	notes    []string // avisos da validação
	forFiles string   // a lista de arquivos para a qual o plano foi feito
}

// startCommitPlan pede o plano de commits das mudanças locais.
func (m model) startCommitPlan() (tea.Model, tea.Cmd) {
	if !m.canAsk() {
		return m, nil
	}
	if m.target != repo.WorkTree || len(m.files) == 0 {
		m.setInfo("/commits organiza as mudanças locais: selecione \"Mudanças locais\" no Navegador")
		return m, nil
	}
	if m.plan != nil && m.plan.forFiles == filesKey(m.files) { // nada mudou: reabre o mesmo plano
		m.mode = modeCommits
		return m, nil
	}
	var b strings.Builder
	if m.branch != "" {
		fmt.Fprintf(&b, "Branch: %s\n\n", m.branch)
	}
	if subs := m.repo.RecentSubjects(10); len(subs) > 0 {
		b.WriteString("<commits_recentes>\n" + strings.Join(subs, "\n") + "\n</commits_recentes>\n\n")
	}
	b.WriteString("<arquivos_alterados>\n")
	for _, f := range m.files {
		fmt.Fprintf(&b, "%s %s (+%d -%d)\n", f.Status, f.Path, f.Add, f.Del)
	}
	b.WriteString("</arquivos_alterados>\n\n")
	if p, err := m.repo.ReviewDiff(repo.WorkTree, min(m.diffLimit(), 30000)); err == nil {
		if p.Diff != "" {
			b.WriteString("<diff>\n" + strings.TrimRight(p.Diff, "\n") + "\n</diff>\n")
		}
		for _, l := range p.Left {
			if len(l.Funcs) > 0 {
				fmt.Fprintf(&b, "- %s (sem diff aqui): %s\n", l.Path, strings.Join(l.Funcs[:min(len(l.Funcs), 6)], ", "))
			}
		}
	}
	b.WriteString("\nComo dividir estas mudanças em commits?")
	return m.askIsolated(commitsSystemPrompt, fmt.Sprintf("como dividir as mudanças locais (%s) em commits?",
		plural(len(m.files), "arquivo", "arquivos")), b.String(), "commits")
}

// startPRDescription pede a descrição do PR da branch atual.
func (m model) startPRDescription() (tea.Model, tea.Cmd) {
	if !m.canAsk() {
		return m, nil
	}
	t := m.target
	if _, _, ok := repo.SplitRange(t); !ok {
		for _, r := range m.rows {
			if r.rng != "" {
				t = r.rng
			}
		}
	}
	base, tip, ok := repo.SplitRange(t)
	if !ok {
		m.setInfo("/pr descreve a branch atual comparada com a principal: crie uma branch com commits primeiro")
		return m, nil
	}
	prompt := fmt.Sprintf("Branch %s, comparada com %s.\n\n<commits>\n%s\n</commits>\n\n<arquivos>\n%s\n</arquivos>",
		tip, base, m.repo.RangeLog(t, 60), m.repo.DiffStat(t))
	prompt, _ = repo.Redact(prompt)
	return m.askIsolated(prSystemPrompt, fmt.Sprintf("descrição do PR de %s", tip), prompt, "pr")
}

func filesKey(fs []repo.FileChange) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "%s %s %d %d\n", f.Status, f.Path, f.Add, f.Del)
	}
	return b.String()
}

// onIsolatedDone age sobre a resposta avulsa que terminou.
func (m model) onIsolatedDone(purpose string) (tea.Model, tea.Cmd) {
	text := ""
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].kind == eAssistant {
			text = m.entries[i].text
			break
		}
		if m.entries[i].kind == eUser {
			break
		}
	}
	switch purpose {
	case "commits":
		p, err := parsePlan(text, m.files)
		if err != nil {
			m.setErr("não consegui ler o plano de commits: " + err.Error())
			return m, nil
		}
		p.forFiles = filesKey(m.files)
		m.plan, m.planIdx, m.mode = p, 0, modeCommits
	case "pr":
		desc := strings.TrimSpace(text)
		termenv.Copy(desc)
		path := filepath.Join(dataHome(), "pedidos", "pr.md")
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil && os.WriteFile(path, []byte(desc+"\n"), 0o644) == nil {
			m.setInfo("descrição do PR copiada · também em " + prettyPath(path))
		}
	}
	return m, nil
}

var jsonBlock = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// parsePlan lê o plano do modelo e confere com os arquivos alterados.
func parsePlan(text string, files []repo.FileChange) (*commitPlan, error) {
	raw := text
	if m := jsonBlock.FindStringSubmatch(text); m != nil {
		raw = m[1]
	} else if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		raw = text[i : j+1]
	}
	var out struct {
		Commits []plannedCommit `json:"commits"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("a resposta não é o JSON pedido")
	}
	changed := map[string]bool{}
	for _, f := range files {
		changed[f.Path] = true
	}
	p := &commitPlan{}
	used := map[string]bool{}
	for _, c := range out.Commits {
		c.Mensagem = strings.TrimSpace(c.Mensagem)
		var keep []string
		for _, f := range c.Arquivos {
			f = strings.TrimPrefix(strings.TrimSpace(f), "./")
			switch {
			case !changed[f]:
				p.notes = append(p.notes, "ignorado (não está nas mudanças): "+f)
			case used[f]:
				p.notes = append(p.notes, "repetido, ficou só no primeiro commit: "+f)
			default:
				used[f] = true
				keep = append(keep, f)
			}
		}
		if c.Mensagem != "" && len(keep) > 0 {
			c.Arquivos = keep
			p.commits = append(p.commits, c)
		}
	}
	for _, f := range files {
		if !used[f.Path] {
			p.missing = append(p.missing, f.Path)
		}
	}
	if len(p.commits) == 0 {
		return nil, fmt.Errorf("o plano veio sem commits")
	}
	return p, nil
}

// ---- janela do plano ----

func (m model) updateCommitKeys(key string) (tea.Model, tea.Cmd) {
	p := m.plan
	if p == nil {
		m.mode = modeNormal
		return m, nil
	}
	if p.editing != "" {
		return m, nil
	}
	switch key {
	case "esc", "q":
		m.mode = modeNormal
	case "up", "k":
		m.planIdx = max(m.planIdx-1, 0)
	case "down", "j":
		m.planIdx = min(m.planIdx+1, len(p.commits)-1)
	case "enter":
		return m.doCommits(m.planIdx, m.planIdx)
	case "a":
		return m.doCommits(0, len(p.commits)-1)
	case "d": // tira do plano: os arquivos voltam para "sem commit"
		c := p.commits[m.planIdx]
		if !c.done {
			p.missing = append(p.missing, c.Arquivos...)
			p.commits = append(p.commits[:m.planIdx], p.commits[m.planIdx+1:]...)
			if len(p.commits) == 0 {
				m.plan, m.mode = nil, modeNormal
				return m, nil
			}
			m.planIdx = min(m.planIdx, len(p.commits)-1)
		}
	case "e":
		c := p.commits[m.planIdx]
		dir, err := os.MkdirTemp("", "dante-commit-")
		if err != nil {
			m.setErr(err.Error())
			return m, nil
		}
		tmp := filepath.Join(dir, "COMMIT_EDITMSG")
		_ = os.WriteFile(tmp, []byte(c.Mensagem+"\n\n"+c.Corpo+"\n"), 0o600)
		p.editing, p.editIdx = tmp, m.planIdx
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		parts := editorCommand(editor, runtime.GOOS, tmp, 1)
		return m, tea.ExecProcess(exec.Command(parts[0], parts[1:]...), func(err error) tea.Msg { return commitEditedMsg{tmp, err} })
	}
	return m, nil
}

type commitEditedMsg struct {
	tmp string
	err error
}

func (m model) onCommitEdited(msg commitEditedMsg) (tea.Model, tea.Cmd) {
	defer os.RemoveAll(filepath.Dir(msg.tmp))
	p := m.plan
	if p == nil || p.editing != msg.tmp {
		return m, nil
	}
	p.editing = ""
	b, err := os.ReadFile(msg.tmp)
	if msg.err != nil || err != nil {
		m.setErr("não consegui ler a mensagem editada")
		return m, nil
	}
	subj, body, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	if strings.TrimSpace(subj) != "" {
		p.commits[p.editIdx].Mensagem, p.commits[p.editIdx].Corpo = strings.TrimSpace(subj), strings.TrimSpace(body)
	}
	return m, nil
}

// doCommits roda o git para os commits de from a to, em ordem, parando no primeiro erro.
func (m model) doCommits(from, to int) (tea.Model, tea.Cmd) {
	p := m.plan
	n := 0
	for i := from; i <= to; i++ {
		c := &p.commits[i]
		if c.done {
			continue
		}
		if err := m.repo.CommitFiles(c.Arquivos, c.Mensagem, c.Corpo); err != nil {
			c.err = firstLine(err.Error())
			m.setErr("o commit \"" + c.Mensagem + "\" falhou: " + c.err)
			break
		}
		c.done, c.err = true, ""
		n++
	}
	if n > 0 {
		m.setInfo(plural(n, "commit feito", "commits feitos"))
	}
	all := true
	for _, c := range p.commits {
		all = all && c.done
	}
	if all {
		m.mode, m.plan = modeNormal, nil
		m.entries = append(m.entries, entry{kind: eNotice, text: plural(len(p.commits), "commit feito", "commits feitos") +
			" pelo plano · confira no Navegador"})
		m.refreshChat()
	}
	return m, tea.Batch(loadLog(m.repo), m.reloadFilesIfWorktree())
}

func (m model) commitsPopup() string {
	p := m.plan
	w := min(m.width-10, 90)
	var b strings.Builder
	b.WriteString(titleStyle.Render("plano de commits") + subtleStyle.Render("  · o git só roda quando você confirma") + "\n\n")
	for i, c := range p.commits {
		mark := subtleStyle.Render("○")
		switch {
		case c.done:
			mark = okStyle.Render("✓")
		case c.err != "":
			mark = errStyle.Render("✗")
		}
		line := fmt.Sprintf("%d. %s", i+1, c.Mensagem)
		if i == m.planIdx {
			line = selectedStyle.Render("▸ " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(mark + " " + truncateW(line, w-4) + subtleStyle.Render(fmt.Sprintf("  %d arq.", len(c.Arquivos))) + "\n")
		if i == m.planIdx {
			if c.Corpo != "" {
				b.WriteString(lipgloss.NewStyle().Width(w-6).PaddingLeft(6).Render(subtleStyle.Render(c.Corpo)) + "\n")
			}
			for _, f := range c.Arquivos {
				b.WriteString("      " + truncateW(f, w-8) + "\n")
			}
			if c.err != "" {
				b.WriteString("      " + errStyle.Render(truncateW(c.err, w-8)) + "\n")
			}
		}
	}
	if len(p.missing) > 0 {
		b.WriteString("\n" + warnStyle.Render(truncateW("sem commit: "+strings.Join(p.missing, ", "), w)) + "\n")
	}
	for _, n := range p.notes {
		b.WriteString(subtleStyle.Render(truncateW(n, w)) + "\n")
	}
	b.WriteString("\n" + subtleStyle.Render("enter commita este · a todos em ordem · e edita a mensagem · d tira do plano · esc fecha"))
	return boxStyle.Padding(1, 2).Render(b.String())
}
