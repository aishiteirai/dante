package llm

import (
	"context"
	"fmt"
)

// turnGuard aplica os limites de uma pergunta:
//   - o orçamento de tokens (Options.TurnBudget), conferido ANTES de cada rodada: a próxima
//     requisição reenvia pelo menos a conversa da última, então, se o gasto mais essa
//     entrada passar do orçamento, as ferramentas não rodam;
//   - o limite de rodadas de leitura (Limits.ReadRounds, vindo do contexto da pergunta).
//
// Quando um limite bate, as ferramentas pedidas não rodam (menos as livres, como
// registrar_achado) e o modelo recebe um aviso para concluir com o que já leu. Se ele
// insistir em pedir ferramentas, o laço para.
type turnGuard struct {
	budget    int64
	spent     int64
	lastInput int64 // entrada da última requisição: a próxima é pelo menos deste tamanho
	limits    Limits
	rounds    int // rodadas de leitura feitas
	exhausted bool
}

// Limits vale para uma pergunta; vai no contexto do Send (WithLimits).
type Limits struct {
	ReadRounds int      // rodadas com ferramentas que não são livres (0 = sem limite)
	Free       []string // ferramentas que não contam nem são bloqueadas
	// Final são ferramentas que encerram a resposta: numa rodada só com elas, os
	// resultados entram no histórico e o modelo não é chamado de novo (ex.: registrar_achado,
	// cujo resultado é só "registrado"; o resumo já veio antes).
	Final []string
}

type limitsKey struct{}

// WithLimits anexa os limites da pergunta ao contexto do Send.
func WithLimits(ctx context.Context, l Limits) context.Context {
	return context.WithValue(ctx, limitsKey{}, l)
}

func limitsFrom(ctx context.Context) Limits {
	l, _ := ctx.Value(limitsKey{}).(Limits)
	return l
}

const budgetResult = "Orçamento de tokens desta pergunta esgotado: esta ferramenta não foi executada. " +
	"Não chame mais ferramentas. Conclua a resposta com o que já leu e diga o que ficou sem verificar."

const roundsResult = "Limite de leituras desta pergunta atingido: esta ferramenta não foi executada. " +
	"Não chame mais ferramentas de leitura. Conclua a resposta com o que já leu e diga o que ficou sem verificar."

func newGuard(ctx context.Context, budget int) *turnGuard {
	return &turnGuard{budget: int64(budget), limits: limitsFrom(ctx)}
}

func (g *turnGuard) add(u Usage) {
	g.spent += u.InputTokens + u.OutputTokens
	if u.InputTokens > 0 {
		g.lastInput = u.InputTokens
	}
}

func (g *turnGuard) free(name string) bool {
	for _, f := range g.limits.Free {
		if f == name {
			return true
		}
	}
	return false
}

// final: todas as ferramentas da rodada encerram a resposta.
func (g *turnGuard) final(names []string) bool {
	if len(names) == 0 || len(g.limits.Final) == 0 {
		return false
	}
	for _, n := range names {
		ok := false
		for _, f := range g.limits.Final {
			ok = ok || f == n
		}
		if !ok {
			return false
		}
	}
	return true
}

// verdict diz o que fazer com as ferramentas pedidas numa rodada.
type verdict struct {
	block string // "" = rodam todas; senão, o resultado das que não são livres
	stop  bool   // encerrar o laço depois desta rodada
}

func (v verdict) blocked(g *turnGuard, name string) bool { return v.block != "" && !g.free(name) }

// check é chamado quando o modelo pede ferramentas.
func (g *turnGuard) check(names []string, emit func(Event)) verdict {
	reads := false
	for _, n := range names {
		if !g.free(n) {
			reads = true
		}
	}
	if !reads {
		return verdict{}
	}
	block := ""
	switch {
	case g.budget > 0 && g.spent+g.lastInput >= g.budget:
		block = budgetResult
		if !g.exhausted {
			emit(Event{Kind: EvNotice, Text: fmt.Sprintf(
				"orçamento desta pergunta quase no fim (%s de %s tokens): pedi ao modelo para concluir com o que já leu",
				thousands(g.spent), thousands(g.budget))})
		}
	case g.limits.ReadRounds > 0 && g.rounds >= g.limits.ReadRounds:
		block = roundsResult
		if !g.exhausted {
			emit(Event{Kind: EvNotice, Text: fmt.Sprintf(
				"limite de %s de leitura desta pergunta: pedi ao modelo para concluir com o que já leu",
				pluralRounds(g.limits.ReadRounds))})
		}
	default:
		g.rounds++
		return verdict{}
	}
	if g.exhausted {
		emit(Event{Kind: EvNotice, IsError: true, Text: "o modelo continuou pedindo ferramentas depois do limite; parei a resposta"})
		return verdict{block: block, stop: true}
	}
	g.exhausted = true
	return verdict{block: block}
}

func pluralRounds(n int) string {
	if n == 1 {
		return "1 rodada"
	}
	return fmt.Sprintf("%d rodadas", n)
}

func thousands(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1f mil", float64(n)/1000)
	}
	return fmt.Sprint(n)
}
