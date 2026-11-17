package llm

import "fmt"

// turnGuard aplica o orçamento de tokens de uma pergunta (Options.TurnBudget). Passado o
// orçamento, as ferramentas pedidas não rodam: o modelo recebe um aviso para concluir com
// o que já leu. Se ele insistir em pedir ferramentas, o laço para.
type turnGuard struct {
	budget    int64
	spent     int64
	exhausted bool
}

const budgetResult = "Orçamento de tokens desta pergunta esgotado: esta ferramenta não foi executada. " +
	"Não chame mais ferramentas. Conclua a resposta com o que já leu e diga o que ficou sem verificar."

func newGuard(budget int) *turnGuard { return &turnGuard{budget: int64(budget)} }

func (g *turnGuard) add(u Usage) { g.spent += u.InputTokens + u.OutputTokens }

// check é chamado quando o modelo pede ferramentas. run=false: não executar e devolver
// budgetResult; stop=true: além disso, encerrar o laço.
func (g *turnGuard) check(emit func(Event)) (run, stop bool) {
	if g.budget <= 0 || g.spent < g.budget {
		return true, false
	}
	if !g.exhausted {
		g.exhausted = true
		emit(Event{Kind: EvNotice, Text: fmt.Sprintf(
			"orçamento desta pergunta atingido (%s tokens): pedi ao modelo para concluir com o que já leu", thousands(g.spent))})
		return false, false
	}
	emit(Event{Kind: EvNotice, IsError: true, Text: "o modelo continuou pedindo ferramentas depois do orçamento; parei a resposta"})
	return false, true
}

func thousands(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1f mil", float64(n)/1000)
	}
	return fmt.Sprint(n)
}
