package main

// visibleModels são os modelos mostrados de um provedor que lista os modelos da conta.
// Com a lista em mãos, valem só os que a conta tem: os da lista fixa do dante entram
// primeiro (são as recomendações) se ainda existirem, e os nomes antigos que o provedor
// descontinuou somem.
func visibleModels(fixed, list []string) []string {
	if len(list) == 0 {
		return fixed
	}
	have := map[string]bool{}
	for _, x := range list {
		have[x] = true
	}
	var keep []string
	for _, x := range fixed {
		if have[x] {
			keep = append(keep, x)
		}
	}
	return mergeUnique(keep, list)
}
