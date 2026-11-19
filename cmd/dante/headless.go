package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aishiteirai/dante/internal/checks"
	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
)

// Comandos sem interface: dante review, check, explain, ask e hook. Usam a mesma montagem
// de contexto da interface (o model, sem desenhar nada), para que terminal, hook do Claude
// Code e CI deem o mesmo resultado.

const headlessUsage = `Comandos sem interface:
  dante review [alvo]          revisa (verificações do projeto + IA) e imprime o relatório
      --json                   saída em JSON
      --saida ARQUIVO          grava o relatório em markdown (para comentar no PR)
      --sem-ia                 só as verificações, sem gastar tokens
      --ci --falhar-em SEV     sai com erro se houver achado com gravidade >= SEV (baixa, media, alta, critica)
      --hook                   modo hook do Claude Code (lê o evento no stdin; ver "dante hook")
  dante check [alvo]           só as verificações do projeto (sem tokens)
  dante explain ARQ:LINHA|Nome explica uma função
  dante ask "pergunta"         pergunta sobre as mudanças
  dante hook instalar [--ia]   configura o hook no .claude/settings.json deste repositório

  alvo: "local" (mudanças locais), um commit, ou base...branch (ex.: main...feat/x).
  Sem alvo: as mudanças locais; sem elas, a branch atual × main; senão, o último commit.
  --modelo provedor/modelo escolhe o modelo (padrão: o último usado na interface).
`

var headlessCmds = map[string]bool{"review": true, "revisar": true, "check": true, "verificar": true, "explain": true,
	"explicar": true, "ask": true, "perguntar": true, "hook": true}

// runHeadless roda um comando sem interface e devolve o código de saída.
func runHeadless(cfg config.Config, args []string) int {
	cmd := args[0]
	fs := flag.NewFlagSet("dante "+cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, headlessUsage) }
	modelFlag := fs.String("modelo", "", "provedor/modelo")
	asJSON := fs.Bool("json", false, "saída em JSON")
	out := fs.String("saida", "", "grava o relatório em markdown neste arquivo")
	noAI := fs.Bool("sem-ia", false, "só as verificações")
	ci := fs.Bool("ci", false, "modo CI: código de saída pela gravidade")
	failAt := fs.String("falhar-em", "alta", "gravidade mínima que faz o CI falhar")
	hook := fs.Bool("hook", false, "modo hook do Claude Code")
	withAI := fs.Bool("ia", false, "no hook: também a revisão com IA (gasta tokens)")
	if err := fs.Parse(reorder(args[1:])); err != nil {
		return 2
	}
	rest := fs.Args()

	if cmd == "hook" {
		if len(rest) == 0 || (rest[0] != "instalar" && rest[0] != "install") {
			fmt.Fprint(os.Stderr, headlessUsage)
			return 2
		}
		return installHook(cfg, *withAI)
	}

	var event struct {
		StopHookActive bool   `json:"stop_hook_active"`
		Cwd            string `json:"cwd"`
	}
	if *hook {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		_ = json.Unmarshal(b, &event)
		if event.StopHookActive { // o Claude já está corrigindo o que o dante apontou: deixa terminar
			return 0
		}
		if event.Cwd != "" {
			_ = os.Chdir(event.Cwd)
		}
	}

	m, err := headlessModel(cfg, *modelFlag, firstOr(rest, ""), cmd)
	if err != nil {
		if *hook {
			return 0 // o hook nunca atrapalha o Claude por um problema do próprio dante
		}
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}

	switch cmd {
	case "check", "verificar":
		issues := headlessChecks(m, true)
		printIssues(issues)
		if *ci && len(issues) > 0 {
			return 1
		}
		return 0
	case "explain", "explicar":
		return headlessExplain(m, strings.Join(rest, " "))
	case "ask", "perguntar":
		if m.target != repo.WorkTree && len(rest) > 0 && looksLikeTarget(rest[0]) {
			rest = rest[1:]
		}
		return headlessAsk(m, strings.Join(rest, " "))
	}

	// review
	if *hook && len(m.files) == 0 {
		return 0
	}
	issues := headlessChecks(m, *hook || m.cfg.ChecksAllowed(m.repo.Root))
	var rev reviewResult
	if !*noAI && (!*hook || *withAI) {
		if rev, err = headlessReview(m, issues, !*asJSON && !*hook); err != nil {
			fmt.Fprintln(os.Stderr, "dante:", err)
			if *hook {
				rev = reviewResult{}
			} else {
				return 2
			}
		}
	}
	if *hook {
		return hookDecision(issues, rev.findings)
	}
	report := reviewMarkdown(m, rev, issues)
	if *out != "" {
		if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "dante:", err)
		}
	}
	if *asJSON {
		printReviewJSON(m, rev, issues)
	} else if *noAI {
		printIssues(issues)
	} else {
		fmt.Println()
		fmt.Print(findingsMarkdown(rev.findings))
	}
	if *ci && failsGate(rev.findings, issues, *failAt) {
		return 1
	}
	return 0
}

// reorder põe as opções antes dos argumentos (o pacote flag para no primeiro argumento).
func reorder(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) && needsValue(a) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}

func needsValue(f string) bool {
	switch strings.TrimLeft(f, "-") {
	case "modelo", "saida", "falhar-em":
		return true
	}
	return false
}

func firstOr(xs []string, def string) string {
	if len(xs) > 0 {
		return xs[0]
	}
	return def
}

func looksLikeTarget(s string) bool {
	return s == "local" || strings.Contains(s, "...") || (len(s) >= 7 && strings.Trim(s, "0123456789abcdef") == "")
}

// headlessModel monta o model sem interface: repositório, alvo, arquivos e modelo.
func headlessModel(cfg config.Config, modelFlag, arg, cmd string) (model, error) {
	wd, _ := os.Getwd()
	root, err := repo.Root(wd)
	if err != nil {
		return model{}, err
	}
	m := newModel(cfg, "", "")
	m.repo = repo.Repo{Root: root, Bloquear: cfg.ArquivosBloqueados, Liberar: cfg.ArquivosLiberados}
	m.branch = m.repo.Branch()
	m.reviewed = map[string]bool{}

	// Alvo: o pedido, ou as mudanças locais, ou a branch atual × main, ou o último commit.
	target := ""
	switch {
	case cmd != "ask" && cmd != "perguntar" && cmd != "explain" && cmd != "explicar" && arg != "":
		target = arg
	case cmd == "ask" || cmd == "perguntar":
		if looksLikeTarget(arg) {
			target = arg
		}
	}
	if target == "local" {
		target = repo.WorkTree
	} else if target == "" {
		if wt, _ := m.repo.Files(repo.WorkTree); len(wt) > 0 {
			target = repo.WorkTree
		} else if base := m.repo.BaseBranch(); base != "" && m.branch != "" && base != m.branch &&
			len(m.repo.RangeCommits(repo.Range(base, m.branch), 1)) > 0 {
			target = repo.Range(base, m.branch)
		} else {
			target = m.repo.Head()
		}
	}
	m.target = target
	*m.sendTo = target
	files, err := m.repo.Files(target)
	if err != nil {
		return model{}, fmt.Errorf("alvo %q: %w", target, err)
	}
	sortFiles(files)
	m.files, m.filesFor = files, target

	choice := cfg.UltimoModelo
	if modelFlag != "" {
		prov, name, _ := strings.Cut(modelFlag, "/")
		choice = &config.Choice{Provedor: prov, Modelo: name}
	}
	if choice != nil {
		if p, ok := cfg.Provider(choice.Provedor); ok {
			m.prov, m.modelName = p, choice.Modelo
		}
	}
	return m, nil
}

func (m model) needModel() error {
	switch {
	case m.modelName == "":
		return fmt.Errorf("sem modelo: use --modelo provedor/modelo, ou escolha um modelo na interface (dante)")
	case !m.prov.HasKey():
		return fmt.Errorf("%s sem chave: cadastre na interface (dante, tecla m) ou defina %s", m.prov.Nome, m.prov.ChaveEnv)
	}
	return nil
}

// headlessChecks roda as verificações do projeto (se permitido) e devolve os problemas novos.
func headlessChecks(m model, allowed bool) []checks.Issue {
	files, _ := m.checkFiles()
	if files == nil {
		return nil
	}
	tools := checks.Detect(m.repo.Root, files)
	if len(tools) == 0 {
		return nil
	}
	if !allowed {
		fmt.Fprintln(os.Stderr, "dante: verificações do projeto não permitidas neste repositório (use /verificar na interface e escolha \"sempre\")")
		return nil
	}
	var all []checks.Issue
	for _, t := range tools {
		fmt.Fprintf(os.Stderr, "dante: %s…\n", t.Name)
		r := checks.Run(context.Background(), t)
		if r.Err != "" {
			fmt.Fprintf(os.Stderr, "dante: %s não rodou: %s\n", t.Name, r.Err)
		}
		all = append(all, r.Issues...)
	}
	d, _ := m.repo.DiffContext(m.target, "", 0)
	changed := checks.ChangedLines(d)
	for _, f := range m.files {
		if f.Status == "?" {
			changed[f.Path] = map[int]bool{0: true}
		}
	}
	return checks.OnlyNew(all, changed)
}

type reviewResult struct {
	text     string
	findings []finding
	usage    llm.Usage
}

// headlessReview faz a revisão com o modelo, como a tecla r da interface.
func headlessReview(m model, issues []checks.Issue, stream bool) (reviewResult, error) {
	if err := m.needModel(); err != nil {
		return reviewResult{}, err
	}
	m.task = taskRevisar
	if err := m.ensureChat(); err != nil {
		return reviewResult{}, err
	}
	block := ""
	if len(issues) > 0 || m.cfg.ChecksAllowed(m.repo.Root) {
		block = checksBlock(issues)
	}
	prompt := m.contextBlock() + m.diffBlock("") + block + modeBlock(m.task, m.level, m.agentBlock()) +
		"Revise as mudanças deste alvo: diga o que está bom e o que precisa mudar."
	fmt.Fprintf(os.Stderr, "dante: revisando %s com %s…\n", targetName(m.target), m.modelName)
	return sendHeadless(m, m.chat, prompt, stream)
}

// sendHeadless envia a pergunta e junta o texto e os achados.
func sendHeadless(m model, chat llm.Chat, prompt string, stream bool) (reviewResult, error) {
	var r reviewResult
	var b strings.Builder
	ctx := llm.WithLimits(context.Background(), m.turnLimits())
	u, err := chat.Send(ctx, prompt, func(ev llm.Event) {
		switch ev.Kind {
		case llm.EvText:
			b.WriteString(ev.Text)
			if stream {
				fmt.Print(ev.Text)
			}
		case llm.EvToolCall:
			if ev.Call.Name == findingTool {
				if f, err := parseFinding(ev.Call.Args); err == nil {
					r.findings = append(r.findings, f)
				}
			} else if stream {
				fmt.Fprintln(os.Stderr, "dante: "+describeCall(ev.Call))
			}
		case llm.EvNotice:
			fmt.Fprintln(os.Stderr, "dante: "+ev.Text)
		}
	})
	rest, more := extractTextFindings(b.String())
	r.text, r.findings, r.usage = rest, append(r.findings, more...), u
	if stream {
		fmt.Println()
		fmt.Fprintln(os.Stderr, "dante: "+usageLine(u))
	}
	return r, err
}

func targetName(t string) string {
	if base, tip, ok := repo.SplitRange(t); ok {
		return "a branch " + tip + " × " + base
	}
	if t == repo.WorkTree {
		return "as mudanças locais"
	}
	return "o commit " + shortSHA(t)
}

var sevRank = map[string]int{"baixa": 0, "media": 1, "alta": 2, "critica": 3}

// failsGate: há achado com gravidade >= a mínima (problemas das ferramentas contam como
// média quando são erros, e baixa quando são avisos).
func failsGate(fs []finding, is []checks.Issue, min string) bool {
	limit, ok := sevRank[fold(min)]
	if !ok {
		limit = sevRank["alta"]
	}
	for _, f := range fs {
		if sevRank[f.Severidade] >= limit {
			return true
		}
	}
	for _, i := range is {
		if (i.Error && limit <= 1) || limit == 0 {
			return true
		}
	}
	return false
}

func findingsMarkdown(fs []finding) string {
	if len(fs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Achados\n\n")
	for i, f := range fs {
		b.WriteString(findingMarkdown(f, i+1) + "\n")
	}
	return b.String()
}

func issuesMarkdown(is []checks.Issue) string {
	var b strings.Builder
	for _, i := range is {
		kind := "aviso"
		if i.Error {
			kind = "erro"
		}
		rule := ""
		if i.Rule != "" {
			rule = " (" + i.Rule + ")"
		}
		fmt.Fprintf(&b, "- `%s:%d` %s, %s%s: %s\n", i.Path, i.Line, i.Tool, kind, rule, shortText(i.Message, 200))
	}
	return b.String()
}

func printIssues(is []checks.Issue) {
	if len(is) == 0 {
		fmt.Println("Nenhum problema novo nas verificações do projeto.")
		return
	}
	fmt.Print(issuesMarkdown(is))
}

func reviewMarkdown(m model, r reviewResult, is []checks.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Revisão do dante: %s\n\n", targetName(m.target))
	if t := strings.TrimSpace(r.text); t != "" {
		b.WriteString(t + "\n\n")
	}
	b.WriteString(findingsMarkdown(r.findings))
	if len(is) > 0 {
		b.WriteString("## Verificações do projeto (só o que é novo)\n\n" + issuesMarkdown(is) + "\n")
	}
	if r.usage.Rounds > 0 {
		fmt.Fprintf(&b, "_%s · %s_\n", m.modelName, usageLine(r.usage))
	}
	return b.String()
}

func printReviewJSON(m model, r reviewResult, is []checks.Issue) {
	type jsonIssue struct {
		Ferramenta string `json:"ferramenta"`
		Arquivo    string `json:"arquivo"`
		Linha      int    `json:"linha"`
		Regra      string `json:"regra,omitempty"`
		Mensagem   string `json:"mensagem"`
		Erro       bool   `json:"erro"`
	}
	out := struct {
		Alvo         string      `json:"alvo"`
		Resumo       string      `json:"resumo"`
		Achados      []finding   `json:"achados"`
		Verificacoes []jsonIssue `json:"verificacoes"`
		Tokens       int64       `json:"tokens"`
	}{Alvo: m.target, Resumo: strings.TrimSpace(r.text), Achados: r.findings, Tokens: r.usage.InputTokens + r.usage.OutputTokens}
	if out.Achados == nil {
		out.Achados = []finding{}
	}
	out.Verificacoes = []jsonIssue{}
	for _, i := range is {
		out.Verificacoes = append(out.Verificacoes, jsonIssue{i.Tool, i.Path, i.Line, i.Rule, i.Message, i.Error})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// ---- hook do Claude Code ----

// hookDecision devolve ao Claude os problemas novos (bloqueando o fim da tarefa até ele
// corrigir), ou deixa terminar em silêncio.
func hookDecision(is []checks.Issue, fs []finding) int {
	var items []string
	for _, i := range is {
		items = append(items, fmt.Sprintf("%s:%d (%s) %s", i.Path, i.Line, i.Tool, shortText(i.Message, 200)))
	}
	for _, f := range fs {
		if sevRank[f.Severidade] >= sevRank["media"] {
			items = append(items, fmt.Sprintf("%s:%d (%s) %s: %s", f.Arquivo, f.Linha, sevLabel[f.Severidade], f.Titulo, shortText(f.Explicacao, 300)))
		}
	}
	if len(items) == 0 {
		return 0
	}
	var b strings.Builder
	fmt.Fprintf(&b, "O dante revisou as suas mudanças e encontrou %s. Confira cada um e corrija os que procedem "+
		"(se algum não proceder, diga por quê) antes de terminar:\n", plural(len(items), "problema novo", "problemas novos"))
	for i, it := range items {
		if i == 15 {
			fmt.Fprintf(&b, "… e mais %d\n", len(items)-15)
			break
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, it)
	}
	resp, _ := json.Marshal(map[string]string{"decision": "block", "reason": b.String()})
	fmt.Println(string(resp))
	return 0
}

// installHook configura o hook Stop no .claude/settings.json do repositório.
func installHook(cfg config.Config, withAI bool) int {
	wd, _ := os.Getwd()
	root, err := repo.Root(wd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	bin := "dante"
	if _, err := exec.LookPath("dante"); err != nil {
		if exe, err := os.Executable(); err == nil {
			bin = strconv.Quote(exe)
		}
	}
	command := bin + " review --hook"
	if withAI {
		command += " --ia"
	}
	path := filepath.Join(root, ".claude", "settings.json")
	settings := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &settings); err != nil {
			fmt.Fprintf(os.Stderr, "dante: %s não é um JSON válido; não mexi nele\n", path)
			return 2
		}
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	stop, _ := hooks["Stop"].([]any)
	for _, g := range stop {
		if b, _ := json.Marshal(g); strings.Contains(string(b), "review --hook") {
			fmt.Println("O hook do dante já está configurado em", path)
			return 0
		}
	}
	stop = append(stop, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 300}}})
	hooks["Stop"] = stop
	settings["hooks"] = hooks
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	b, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	// Instalar o hook é a permissão para rodar as verificações do projeto neste repositório.
	cfg.AllowChecks(root)
	_ = cfg.Save()
	fmt.Printf("Hook configurado em %s:\n  %s\n\n", path, command)
	fmt.Println("Quando o Claude Code terminar uma tarefa neste repositório, o dante roda as verificações do projeto")
	fmt.Println("nas mudanças locais e, se achar problemas novos, devolve a lista ao Claude para ele corrigir.")
	if withAI {
		fmt.Println("Com --ia, também faz a revisão com o modelo (gasta tokens a cada tarefa).")
	}
	return 0
}

// ---- explain e ask ----

func headlessExplain(m model, arg string) int {
	if err := m.needModel(); err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	ref := repo.Tip(m.target)
	var fi repo.FuncInfo
	var err error
	if p, l, ok := strings.Cut(arg, ":"); ok && l != "" {
		n, _ := strconv.Atoi(l)
		fi, err = m.repo.FuncAt(repo.WorkTree, p, n)
	} else {
		pth, line, found := m.repo.FindFunc(ref, arg)
		if !found {
			pth, line, found = m.repo.FindFunc(repo.WorkTree, arg)
		}
		if !found {
			fmt.Fprintf(os.Stderr, "dante: não achei a função %q\n", arg)
			return 2
		}
		fi, err = m.repo.FuncAt(repo.WorkTree, pth, line)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	chat, err := llm.New(m.prov.LLM(), llm.Options{Model: m.modelName, System: funcSystemPrompt, MaxSteps: 1, ContextTokens: m.effectiveContext()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	if _, err := sendHeadless(m, chat, m.funcPrompt(funcPackMsg{info: fi, version: "o disco"}), true); err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	return 0
}

func headlessAsk(m model, q string) int {
	if strings.TrimSpace(q) == "" {
		fmt.Fprint(os.Stderr, headlessUsage)
		return 2
	}
	if err := m.needModel(); err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	if t, l := detectMode(q); t != nil || l != nil {
		if t != nil {
			m.task = *t
		}
		if l != nil {
			m.level = *l
		}
	} else {
		m.task = taskExplicar
	}
	if err := m.ensureChat(); err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	prompt := m.contextBlock() + m.diffBlock("") + modeBlock(m.task, m.level, m.agentBlock()) + q
	r, err := sendHeadless(m, m.chat, prompt, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	if len(r.findings) > 0 {
		fmt.Print("\n" + findingsMarkdown(r.findings))
	}
	return 0
}
