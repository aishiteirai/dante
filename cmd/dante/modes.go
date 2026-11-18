package main

import (
	"strings"
)

// Modos de conversa: a tarefa (revisar ou explicar) e o formato da resposta (normal,
// aprendiz ou rápido). Mudam por comando no começo da mensagem ou pela detecção no pedido.
// A detecção nunca muda permissões: só a tarefa e o formato.

type taskMode int

const (
	taskRevisar taskMode = iota
	taskExplicar
)

type levelMode int

const (
	levelNormal levelMode = iota
	levelAprendiz
	levelRapido
)

var taskNames = map[taskMode]string{taskRevisar: "revisar", taskExplicar: "explicar"}
var levelNames = map[levelMode]string{levelNormal: "normal", levelAprendiz: "aprendiz", levelRapido: "rápido"}

type chatCommand struct{ name, desc string }

var chatCommands = []chatCommand{
	{"/revisar", "procura problemas (padrão)"},
	{"/explicar", "diz o que o código faz e por quê, sem julgar"},
	{"/aprendiz", "sem jargão, com analogias e por onde começar"},
	{"/rapido", "cinco linhas no máximo"},
	{"/normal", "o equilíbrio de sempre"},
}

func modeLabel(t taskMode, l levelMode) string { return taskNames[t] + " · " + levelNames[l] }

// parseCommands lê os comandos no começo da mensagem ("/explicar /aprendiz como funciona
// o chat?") e devolve as mudanças de modo e o resto do texto.
func parseCommands(text string) (t *taskMode, l *levelMode, rest, unknown string) {
	words := strings.Fields(text)
	i := 0
	for ; i < len(words) && strings.HasPrefix(words[i], "/"); i++ {
		switch fold(words[i]) {
		case "/revisar", "/revisao":
			v := taskRevisar
			t = &v
		case "/explicar", "/explica":
			v := taskExplicar
			t = &v
		case "/aprendiz", "/leigo", "/estagiario":
			v := levelAprendiz
			l = &v
		case "/rapido", "/pontual":
			v := levelRapido
			l = &v
		case "/normal":
			v := levelNormal
			l = &v
		default:
			return nil, nil, text, words[i]
		}
	}
	if i == 0 {
		return nil, nil, strings.TrimSpace(text), ""
	}
	return t, l, strings.Join(words[i:], " "), ""
}

// fold deixa minúsculo e sem acentos, para comparar comandos e palavras-chave.
func fold(s string) string {
	return strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i",
		"ó", "o", "ô", "o", "õ", "o", "ú", "u", "ü", "u", "ç", "c",
	).Replace(strings.ToLower(s))
}

var (
	kwAprendiz = []string{"estagiari", "iniciante", "leigo", "como se eu fosse", "nao entendi", "nao entendo",
		"de forma simples", "bem simples", "do zero", "sou novo", "sou nova", "estou aprendendo", "to aprendendo",
		"para quem esta aprendendo", "didatic", "sem jargao", "crianca"}
	kwRapido = []string{"seja pontual", "pontual", "resumo", "resumid", "resuma", "rapid", "direto ao ponto",
		"tl;dr", "tldr", "em poucas palavras", "curto", "sem enrolar", "objetiv"}
	kwNormal   = []string{"detalhad", "aprofund", "com detalhes", "em detalhe"}
	kwExplicar = []string{"explique", "explica", "o que faz", "o que esse", "o que essa", "o que este", "o que isso",
		"como funciona", "para que serve", "pra que serve", "o que e ", "me ajuda a entender", "quero entender",
		"me apresenta", "como esta organizado", "como e organizado"}
	kwRevisar = []string{"revise", "revisa", "revisao", "review", "tem bug", "tem algum bug", "tem algum problema",
		"tem problema", "algum erro", "esta certo", "ta certo", "esta correto", "pode melhorar", "o que melhorar",
		"falha de seguranca", "vulnerab"}
)

func hasAny(s string, kws []string) bool {
	for _, k := range kws {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// detectMode procura no pedido sinais de tarefa e de formato. nil = sem sinal.
func detectMode(text string) (t *taskMode, l *levelMode) {
	s := " " + fold(text) + " "
	switch {
	case hasAny(s, kwRevisar):
		v := taskRevisar
		t = &v
	case hasAny(s, kwExplicar):
		v := taskExplicar
		t = &v
	}
	switch {
	case hasAny(s, kwAprendiz):
		v := levelAprendiz
		l = &v
	case hasAny(s, kwRapido):
		v := levelRapido
		l = &v
	case hasAny(s, kwNormal):
		v := levelNormal
		l = &v
	}
	return
}

// modeBlock é a instrução do modo que vai em cada mensagem ao modelo.
func modeBlock(t taskMode, l levelMode) string {
	var b strings.Builder
	b.WriteString("<modo>" + modeLabel(t, l) + "\n")
	switch t {
	case taskRevisar:
		b.WriteString(reviewInstructions)
	case taskExplicar:
		b.WriteString("Tarefa: explicar. Diga o que o código ou a mudança faz e por quê, sem procurar problemas. " +
			"Organize em seções curtas com títulos (## O que faz, ## Como funciona, ## Onde fica) e cite " +
			"arquivo:linha sempre que mencionar um trecho de código.\n")
	}
	switch l {
	case levelAprendiz:
		b.WriteString("Público: alguém que está aprendendo. Evite jargão; quando usar um termo técnico, explique-o " +
			"em poucas palavras. Inclua uma analogia do dia a dia, um passo a passo numerado, uma seção " +
			"## Palavras novas com os termos usados e uma seção ## Por onde continuar dizendo qual arquivo ler primeiro.\n")
	case levelRapido:
		b.WriteString("Formato: rápido. No máximo cinco linhas de texto, sem seções nem introdução. Numa revisão, " +
			"aponte só os problemas de gravidade média ou maior, cada um em uma frase.\n")
	}
	b.WriteString("</modo>\n\n")
	return b.String()
}

// Instruções da tarefa de revisão (a parte de achados muda com a ferramenta registrar_achado).
var reviewInstructions = "Tarefa: revisar. Procure problemas nas mudanças. Escreva primeiro um resumo de uma ou duas " +
	"frases. Depois chame registrar_achado uma vez para cada problema, do mais grave para o menos grave, e não repita " +
	"no texto o que já registrou. Se estiver tudo bem, diga isso no resumo e não registre nada. Se não puder usar " +
	"ferramentas, liste os pontos no texto, cada um com arquivo:linha, o problema, por que importa e a sugestão.\n"

// commandSuggestions: comandos que começam com o que foi digitado (só enquanto se digita
// o primeiro comando).
func commandSuggestions(input string) []chatCommand {
	if !strings.HasPrefix(input, "/") || strings.ContainsAny(input, " \n") {
		return nil
	}
	var out []chatCommand
	for _, c := range chatCommands {
		if strings.HasPrefix(c.name, fold(input)) {
			out = append(out, c)
		}
	}
	return out
}
