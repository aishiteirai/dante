package llm

import "fmt"

// Orçamento de contexto para provedores sem gestão de contexto no servidor (compatível
// com OpenAI e Ollama). O dante estima os tokens da conversa, se calibra com a contagem
// real que o provedor devolve, e:
//   - corta resultados de ferramenta grandes demais para o contexto;
//   - quando a conversa passa de 70% do contexto, troca resultados de ferramentas de
//     rodadas anteriores por um aviso (o modelo pode chamar a ferramenta de novo);
//   - avisa quando nem assim a conversa cabe.

const prunedResult = "[resultado antigo removido para caber no contexto; chame a ferramenta de novo se precisar]"

type budget struct {
	tokens      int     // tamanho do contexto do modelo (0 = desconhecido, sem poda)
	perByte     float64 // tokens por byte, calibrado pelo uso real
	warnedPrune bool
	warnedOver  bool
}

func newBudget(tokens int) *budget { return &budget{tokens: tokens, perByte: 1.0 / 3} }

func (b *budget) estimate(bytes int) int { return int(float64(bytes) * b.perByte) }

// calibrate ajusta a estimativa com os tokens que o provedor contou para o pedido.
func (b *budget) calibrate(promptTokens int64, bytes int) {
	if promptTokens > 0 && bytes > 1000 {
		b.perByte = min(max(float64(promptTokens)/float64(bytes), 0.15), 0.6)
	}
}

// clip corta um resultado de ferramenta que ocuparia mais de 1/4 do contexto.
func (b *budget) clip(result string) string {
	if b.tokens <= 0 {
		return result
	}
	maxBytes := max(int(float64(b.tokens)/4/b.perByte), 2000)
	if len(result) <= maxBytes {
		return result
	}
	return result[:maxBytes] + fmt.Sprintf("\n… (cortado para caber no contexto do modelo: %d de %d caracteres; "+
		"peça um trecho menor, ex.: read_file com start_line/end_line)", maxBytes, len(result))
}

// prune poda os resultados de ferramentas anteriores a `current` (início da rodada atual)
// até a conversa caber em metade do contexto. Devolve quantos foram removidos.
func (b *budget) prune(n, current int, total int, size func(int) int, isTool func(int) bool, clear func(int)) (removed int, newTotal int) {
	if b.tokens <= 0 || b.estimate(total) <= b.tokens*7/10 {
		return 0, total
	}
	for i := 0; i < current && i < n && b.estimate(total) > b.tokens/2; i++ {
		if isTool(i) {
			before := size(i)
			clear(i)
			total -= before - size(i)
			removed++
		}
	}
	return removed, total
}

// notices devolve os avisos ao usuário sobre o contexto (cada um aparece uma vez).
func (b *budget) notices(removed, total int) []Event {
	var evs []Event
	if removed > 0 && !b.warnedPrune {
		b.warnedPrune = true
		evs = append(evs, Event{Kind: EvNotice, Text: fmt.Sprintf(
			"o contexto do modelo está enchendo: descartei %d resultado(s) antigo(s) de ferramentas", removed)})
	}
	if b.tokens > 0 && b.estimate(total) > b.tokens*9/10 && !b.warnedOver {
		b.warnedOver = true
		evs = append(evs, Event{Kind: EvNotice, IsError: true, Text: fmt.Sprintf(
			"a conversa (~%d mil tokens) está perto do limite do contexto do modelo (%d mil); ctrl+n começa uma nova",
			b.estimate(total)/1000, b.tokens/1000)})
	}
	return evs
}
