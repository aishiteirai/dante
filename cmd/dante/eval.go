package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aishiteirai/dante/internal/config"
	"github.com/aishiteirai/dante/internal/llm"
	"github.com/aishiteirai/dante/internal/repo"
)

// dante eval: um conjunto fixo de perguntas, passadas pelo mesmo caminho da interface, para
// medir tokens, a rota e a tarefa escolhidas e o formato das respostas. Serve para comparar
// modelos e para ver se uma mudança no dante piorou alguma coisa. Com --sem-ia, mede só a
// rota e o tamanho do pedido, sem gastar tokens (dá para rodar no CI).

type evalCase struct {
	Nome     string     `json:"nome"`
	Pergunta string     `json:"pergunta"`
	Espera   evalExpect `json:"espera"`
}

type evalExpect struct {
	Rota      string   `json:"rota,omitempty"`   // cumprimento, conversa, funcao
	Tarefa    string   `json:"tarefa,omitempty"` // conversar, revisar, explicar
	Contem    []string `json:"contem,omitempty"` // expressões regulares, sem diferenciar maiúsculas
	NaoContem []string `json:"nao_contem,omitempty"`
	// Limites de tokens. A estimativa (sem a IA) é o pedido inicial; com a IA, a entrada
	// soma todas as rodadas.
	MaxEstimativa int  `json:"max_estimativa,omitempty"`
	MaxEntrada    int  `json:"max_entrada,omitempty"`
	MaxSaida      int  `json:"max_saida,omitempty"`
	MaxRodadas    int  `json:"max_rodadas,omitempty"`
	MinAchados    *int `json:"min_achados,omitempty"`
	MaxAchados    *int `json:"max_achados,omitempty"`
}

// O conjunto padrão vale para qualquer repositório; .dante/eval.json no repositório o substitui.
var defaultEval = []evalCase{
	{Nome: "cumprimento", Pergunta: "oi", Espera: evalExpect{Rota: "cumprimento", MaxEstimativa: 400, MaxSaida: 150,
		NaoContem: []string{"registrar_achado", "<\\|"}, MaxAchados: ptr(0)}},
	{Nome: "agradecimento", Pergunta: "valeu!", Espera: evalExpect{Rota: "cumprimento", MaxEstimativa: 400, MaxSaida: 150}},
	{Nome: "pergunta solta", Pergunta: "qual linguagem este projeto usa?", Espera: evalExpect{Rota: "conversa",
		Tarefa: "conversar", MaxAchados: ptr(0), MaxRodadas: 3}},
	{Nome: "explicar", Pergunta: "do que se trata este projeto?", Espera: evalExpect{Rota: "conversa", Tarefa: "explicar"}},
	{Nome: "revisar", Pergunta: "revise as mudanças", Espera: evalExpect{Rota: "conversa", Tarefa: "revisar", MaxRodadas: 4}},
	{Nome: "comando", Pergunta: "/rapido o que mudou?", Espera: evalExpect{Rota: "conversa", MaxSaida: 400}},
}

func ptr(n int) *int { return &n }

type evalResult struct {
	Nome       string   `json:"nome"`
	Rota       string   `json:"rota"`
	Tarefa     string   `json:"tarefa,omitempty"`
	Estimativa int      `json:"estimativa"` // tokens do pedido inicial (sistema + ferramentas + mensagem) / 4 caracteres
	Entrada    int64    `json:"entrada,omitempty"`
	Cache      int64    `json:"cache,omitempty"`
	Saida      int64    `json:"saida,omitempty"`
	Rodadas    int      `json:"rodadas,omitempty"`
	Achados    int      `json:"achados"`
	Segundos   float64  `json:"segundos,omitempty"`
	Resposta   string   `json:"resposta,omitempty"`
	Falhas     []string `json:"falhas,omitempty"`
}

func loadEvalCases(root, path string) ([]evalCase, string, error) {
	if path == "" {
		p := filepath.Join(root, ".dante", "eval.json")
		if _, err := os.Stat(p); err != nil {
			return defaultEval, "conjunto padrão", nil
		}
		path = p
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var cs []evalCase
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	return cs, path, nil
}

// evalRoute decide, como o chat da interface, por onde a pergunta vai e monta o pedido.
func evalRoute(m *model, q string) (route, system, prompt string, tools []llm.Tool) {
	switch {
	case isSmallTalk(q):
		return "cumprimento", smallTalkSystem, q, nil
	case asksAboutPastedFunc(q):
		return "funcao", "", "", nil
	}
	cmds, rest, _ := parseCommands(q)
	t, l, _ := messageTask(cmds, rest)
	m.task = *t
	if l != nil {
		m.level = *l
	}
	tools = append(append(repo.Tools(), findingToolDef()), repo.WriteTools()...)
	return "conversa", m.buildSystemPrompt(), m.turnPrompt(rest, "", ""), tools
}

func runEval(base model, path string, noAI, asJSON bool) int {
	cases, from, err := loadEvalCases(base.repo.Root, path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dante:", err)
		return 2
	}
	if !noAI {
		if err := base.needModel(); err != nil {
			fmt.Fprintln(os.Stderr, "dante:", err)
			return 2
		}
	}
	who := "sem IA (só rota e tamanho do pedido)"
	if !noAI {
		who = base.prov.ID + "/" + base.modelName
	}
	fmt.Fprintf(os.Stderr, "dante: %d perguntas (%s) · alvo: %s · %s\n", len(cases), from, targetName(base.target), who)

	var results []evalResult
	failed, unavailable := 0, 0
	for _, c := range cases {
		m := base
		m.chat = nil
		m.packed = newPackedDiffs()
		route, system, prompt, tools := evalRoute(&m, c.Pergunta)
		r := evalResult{Nome: c.Nome, Rota: route}
		if route == "conversa" {
			r.Tarefa = taskNames[m.task]
		}
		tj, _ := json.Marshal(tools)
		r.Estimativa = (len(system) + len(tj) + len(prompt)) / 4
		if !noAI && route != "funcao" {
			start := time.Now()
			var rr reviewResult
			var err error
			if route == "cumprimento" {
				var chat llm.Chat
				if chat, err = newIsolatedChat(m, system); err == nil {
					rr, err = sendHeadless(m, chat, prompt, false)
				}
			} else if err = m.ensureChat(); err == nil {
				rr, err = sendHeadless(m, m.chat, prompt, false)
			}
			r.Segundos = time.Since(start).Round(100 * time.Millisecond).Seconds()
			if errors.Is(err, llm.ErrModelNotFound) || errors.Is(err, llm.ErrInvalidKey) {
				// Vale para todas as perguntas: para aqui, sem repetir o erro seis vezes.
				fmt.Fprintln(os.Stderr, "dante:", err)
				if errors.Is(err, llm.ErrModelNotFound) {
					if s := similarModels(m.prov, m.modelName); len(s) > 0 {
						fmt.Fprintf(os.Stderr, "\nModelos parecidos em %s:\n  %s/%s\n", m.prov.Nome, m.prov.ID,
							strings.Join(s, "\n  "+m.prov.ID+"/"))
					}
					fmt.Fprintln(os.Stderr, "\nPara ver todos: dante modelos "+m.prov.ID)
				}
				return 2
			}
			if errors.Is(err, llm.ErrUnavailable) {
				unavailable++
			} else {
				unavailable = 0
			}
			if unavailable >= 2 { // duas perguntas seguidas sem resposta: não adianta seguir agora
				fmt.Fprintln(os.Stderr, "dante:", err)
				fmt.Fprintln(os.Stderr, "dante: o provedor não está respondendo; tente de novo em alguns minutos ou use outro modelo (dante modelos)")
				return 2
			}
			if err != nil {
				r.Falhas = append(r.Falhas, "erro: "+err.Error())
			}
			r.Entrada, r.Cache, r.Saida, r.Rodadas = rr.usage.InputTokens, rr.usage.CachedTokens, rr.usage.OutputTokens, rr.usage.Rounds
			r.Achados, r.Resposta = len(rr.findings), strings.TrimSpace(rr.text)
		}
		r.Falhas = append(r.Falhas, checkEval(c.Espera, r, noAI)...)
		if len(r.Falhas) > 0 {
			failed++
		}
		if !asJSON {
			printEvalLine(r, noAI)
		}
		results = append(results, r)
	}
	if asJSON {
		b, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(b))
	} else {
		printEvalSummary(results, failed, noAI)
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func checkEval(e evalExpect, r evalResult, noAI bool) []string {
	var f []string
	if e.Rota != "" && e.Rota != r.Rota {
		f = append(f, fmt.Sprintf("rota %s, esperava %s", r.Rota, e.Rota))
	}
	if e.Tarefa != "" && e.Tarefa != r.Tarefa {
		f = append(f, fmt.Sprintf("tarefa %s, esperava %s", orDash(r.Tarefa), e.Tarefa))
	}
	if e.MaxEstimativa > 0 && r.Estimativa > e.MaxEstimativa {
		f = append(f, fmt.Sprintf("pedido de ~%d tokens, limite %d", r.Estimativa, e.MaxEstimativa))
	}
	if noAI || r.Rota == "funcao" {
		return f
	}
	if e.MaxEntrada > 0 && r.Entrada > int64(e.MaxEntrada) {
		f = append(f, fmt.Sprintf("entrada de %d tokens, limite %d", r.Entrada, e.MaxEntrada))
	}
	if e.MaxSaida > 0 && r.Saida > int64(e.MaxSaida) {
		f = append(f, fmt.Sprintf("saída de %d tokens, limite %d", r.Saida, e.MaxSaida))
	}
	if e.MaxRodadas > 0 && r.Rodadas > e.MaxRodadas {
		f = append(f, fmt.Sprintf("%d rodadas, limite %d", r.Rodadas, e.MaxRodadas))
	}
	if e.MinAchados != nil && r.Achados < *e.MinAchados {
		f = append(f, fmt.Sprintf("%d achados, esperava ao menos %d", r.Achados, *e.MinAchados))
	}
	if e.MaxAchados != nil && r.Achados > *e.MaxAchados {
		f = append(f, fmt.Sprintf("%d achados, esperava no máximo %d", r.Achados, *e.MaxAchados))
	}
	for _, p := range e.Contem {
		if re, err := regexp.Compile("(?i)" + p); err != nil || !re.MatchString(r.Resposta) {
			f = append(f, fmt.Sprintf("a resposta não contém %q", p))
		}
	}
	for _, p := range e.NaoContem {
		if re, err := regexp.Compile("(?i)" + p); err == nil && re.MatchString(r.Resposta) {
			f = append(f, fmt.Sprintf("a resposta contém %q", p))
		}
	}
	return f
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func printEvalLine(r evalResult, noAI bool) {
	mark := "✓"
	if len(r.Falhas) > 0 {
		mark = "✗"
	}
	route := r.Rota
	if r.Tarefa != "" {
		route += "/" + r.Tarefa
	}
	line := fmt.Sprintf("%s %-16s %-20s pedido ~%s", mark, r.Nome, route, humanTokens(int64(r.Estimativa)))
	if !noAI && r.Rota != "funcao" {
		line += fmt.Sprintf(" · entrada %s (cache %s) · saída %s · %d rodada(s) · %d achado(s) · %.1fs",
			humanTokens(r.Entrada), humanTokens(r.Cache), humanTokens(r.Saida), r.Rodadas, r.Achados, r.Segundos)
	}
	fmt.Println(line)
	for _, f := range r.Falhas {
		fmt.Println("    ✗ " + f)
	}
}

func printEvalSummary(rs []evalResult, failed int, noAI bool) {
	var in, cache, out int64
	for _, r := range rs {
		in, cache, out = in+r.Entrada, cache+r.Cache, out+r.Saida
	}
	s := fmt.Sprintf("\n%d de %d passaram", len(rs)-failed, len(rs))
	if !noAI {
		s += fmt.Sprintf(" · total: entrada %s (cache %s), saída %s", humanTokens(in), humanTokens(cache), humanTokens(out))
	}
	fmt.Println(s)
}

// similarModels lista os modelos do provedor com o nome mais parecido com o pedido (mesma
// família e mesmo tipo, como "gemini" e "flash"), para corrigir um nome digitado errado.
func similarModels(p config.Provider, name string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	all, err := llm.ListModels(ctx, p.LLM(), "")
	if err != nil {
		return nil
	}
	return rankSimilar(all, name, 6)
}

func rankSimilar(all []string, name string, n int) []string {
	parts := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '-' || r == '/' || r == ':' || r == '.' })
	type scored struct {
		id string
		s  int
	}
	var ss []scored
	for _, id := range all {
		l, s := strings.ToLower(id), 0
		for _, p := range parts {
			if len(p) > 1 && strings.Contains(l, p) {
				s += len(p)
			}
		}
		if s > 0 {
			ss = append(ss, scored{id, s})
		}
	}
	sort.SliceStable(ss, func(i, j int) bool { return ss[i].s > ss[j].s })
	var out []string
	for i := 0; i < len(ss) && i < n; i++ {
		out = append(out, ss[i].id)
	}
	return out
}
