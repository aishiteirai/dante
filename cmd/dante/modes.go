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
	taskConversar // responder ao que foi perguntado, sem revisão nem análise não pedida
)

type levelMode int

const (
	levelNormal levelMode = iota
	levelAprendiz
	levelRapido
)

var taskNames = map[taskMode]string{taskRevisar: "revisar", taskExplicar: "explicar", taskConversar: "conversar"}
var levelNames = map[levelMode]string{levelNormal: "normal", levelAprendiz: "aprendiz", levelRapido: "rápido"}

type chatCommand struct{ name, desc string }

var chatCommands = []chatCommand{
	{"/revisar", "procura problemas nas mudanças (ou a tecla r)"},
	{"/explicar", "diz o que o código faz e por quê, sem julgar"},
	{"/aprendiz", "sem jargão, com analogias e por onde começar"},
	{"/rapido", "cinco linhas no máximo"},
	{"/normal", "o equilíbrio de sempre"},
	{"/ler", "o modelo só lê e sugere (padrão)"},
	{"/editar", "o modelo pode alterar arquivos, com a sua aprovação"},
	{"/permissoes", "o que o modelo pode fazer no modo editar"},
	{"/regras", "rascunho do CLAUDE.md do projeto para os agentes (/regras salvar grava)"},
	{"/verificar", "roda os linters e checagens do projeto nos arquivos alterados (sem tokens)"},
	{"/onde", "onde mexer para uma tarefa, com um briefing pronto para o Claude"},
	{"/como", "como uma funcionalidade funciona de ponta a ponta"},
	{"/commits", "planeja como dividir as mudanças locais em commits (você confirma cada um)"},
	{"/pr", "escreve a descrição do PR da branch atual"},
	{"/corrigir", "copia um pedido de correção dos achados, para colar no Claude"},
	{"/funcao", "explica uma função (pelo nome ou colada) numa pergunta barata"},
	{"/tecnologias", "o que o projeto usa (linguagens, frameworks, bibliotecas), sem tokens"},
	{"/trilha", "roteiro para aprender uma tecnologia lendo este projeto (ex.: /trilha react)"},
	{"/oque", "o que é um termo, com exemplos do projeto (ex.: /oque goroutine)"},
	{"/mapa", "gera o mapa do projeto com um modelo barato (mostra o custo antes)"},
	{"/tour", "percorre o mapa do projeto, capítulo por capítulo"},
}

func modeLabel(t taskMode, l levelMode) string { return taskNames[t] + " · " + levelNames[l] }

// cmdSet são as mudanças pedidas por comandos no começo da mensagem.
type cmdSet struct {
	task  *taskMode
	level *levelMode
	agent *agentMode
	perms bool // abrir o painel de permissões
	mapa  bool // gerar ou atualizar o mapa do projeto
	tour  bool // percorrer o mapa
}

// parseCommands lê os comandos no começo da mensagem ("/explicar /aprendiz como funciona
// o chat?") e devolve as mudanças de modo e o resto do texto.
func parseCommands(text string) (c cmdSet, rest, unknown string) {
	words := strings.Fields(text)
	i := 0
	for ; i < len(words) && strings.HasPrefix(words[i], "/"); i++ {
		switch fold(words[i]) {
		case "/revisar", "/revisao":
			v := taskRevisar
			c.task = &v
		case "/explicar", "/explica":
			v := taskExplicar
			c.task = &v
		case "/aprendiz", "/leigo", "/estagiario":
			v := levelAprendiz
			c.level = &v
		case "/rapido", "/pontual":
			v := levelRapido
			c.level = &v
		case "/normal":
			v := levelNormal
			c.level = &v
		case "/ler":
			v := agentLer
			c.agent = &v
		case "/editar":
			v := agentEditar
			c.agent = &v
		case "/permissoes", "/permissao":
			c.perms = true
		case "/mapa":
			c.mapa = true
		case "/tour":
			c.tour = true
		default:
			return cmdSet{}, text, words[i]
		}
	}
	if i == 0 {
		return c, strings.TrimSpace(text), ""
	}
	return c, strings.Join(words[i:], " "), ""
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
		"me apresenta", "como esta organizado", "como e organizado", "do que se trata", "se trata de", "sobre o que",
		"o que mudou", "o que muda", "o que foi feito", "o que foi alterado", "o que aconteceu", "me conta", "me conte",
		"resuma o commit", "descreva", "o que tem nesse", "o que tem neste"}
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

var kwEditar = []string{"corrija", "corrige", "conserte", "arrume", "aplique", "aplica ", "altere", "edite", "refatore",
	"implemente", "adicione", "remova", "renomeie", "mude o", "mude a", "troque", "faca a mudanca", "faz a mudanca"}

// asksForEdit: o pedido parece pedir uma mudança nos arquivos.
func asksForEdit(text string) bool { return hasAny(" "+fold(text)+" ", kwEditar) }

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
func modeBlock(t taskMode, l levelMode, agent string) string {
	var b strings.Builder
	b.WriteString("<modo>" + modeLabel(t, l) + "\n")
	b.WriteString(agent)
	switch t {
	case taskConversar:
		b.WriteString(chatInstructions)
	case taskRevisar:
		b.WriteString(reviewInstructions)
	case taskExplicar:
		b.WriteString(explainInstructions)
	}
	switch l {
	case levelAprendiz:
		b.WriteString("Público: alguém que está aprendendo. Evite jargão; quando usar um termo técnico, explique-o " +
			"em poucas palavras. Inclua uma analogia do dia a dia, um passo a passo numerado e, no fim, " +
			"**Palavras novas:** com os termos usados e **Por onde continuar:** com o arquivo para ler primeiro.\n")
	case levelRapido:
		b.WriteString("Formato: rápido. Até 5 linhas: a primeira diz o essencial (o efeito da mudança); depois, no " +
			"máximo 3 tópicos curtos. Sem títulos. Numa revisão, aponte só os problemas de gravidade média ou maior.\n")
	}
	b.WriteString("</modo>\n\n")
	return b.String()
}

// Instruções da conversa: o padrão quando o pedido não é revisar nem explicar.
var chatInstructions = "Tarefa: conversar. Responda só ao que foi perguntado, de forma direta e curta; não faça " +
	"revisão, análise ou lista de problemas que não foram pedidas, nem chame registrar_achado. Se a mensagem for um " +
	"cumprimento ou não tiver pergunta, responda em uma frase e diga, em uma linha, o que você pode fazer (revisar as " +
	"mudanças, explicar o código, dizer onde mexer). Se a resposta depender do código, leia só o necessário.\n"

// Instruções da tarefa de explicar: o efeito, o antes e depois, o porquê e o impacto, em
// rótulos curtos (cabem melhor no painel do chat que títulos).
var explainInstructions = "Tarefa: explicar, sem procurar problemas, para quem vai trabalhar nesse código. Estrutura " +
	"(omita a parte que não se aplicar):\n" +
	"- primeira linha, em negrito: o que muda na prática, numa frase;\n" +
	"- **Antes → depois:** o comportamento antes e depois da mudança (fora de um commit: o papel do código no projeto);\n" +
	"- **Por quê:** a intenção, a partir da mensagem do commit e do código;\n" +
	"- **Como funciona:** o caminho principal em 3 a 6 tópicos, cada um com a função e o arquivo:linha;\n" +
	"- **Impacto:** quem usa isso, o que muda para quem chama e o que merece atenção.\n" +
	"Agrupe por ideia, não por arquivo.\n"

// Instruções da tarefa de revisão (a parte de achados muda com a ferramenta registrar_achado).
var reviewInstructions = "Tarefa: revisar. Parta do <diff>; abra código só se uma conclusão depender de algo fora " +
	"dele, tudo numa rodada de read_files com trechos (há um limite de rodadas de leitura por pergunta). Escreva:\n" +
	"- primeira linha, em negrito: o que a mudança faz na prática e o veredito (pronta, ou o principal risco);\n" +
	"- **Impacto:** quem é afetado (use <quem_chama>), o que muda para quem chama, o que pode quebrar e se há " +
	"testes cobrindo; 2 a 4 tópicos curtos.\n" +
	"Depois chame registrar_achado uma vez para cada problema (bug, segurança, impedimento ou limpeza que valha a " +
	"pena), do mais grave para o menos grave, todos na mesma resposta, e não repita no texto o que já registrou. " +
	"Siga o bloco <verificar>, quando houver. Se estiver tudo bem, diga isso e não registre nada. Se não puder usar " +
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

// Cumprimentos e agradecimentos ("oi", "bom dia", "valeu!") não precisam do repositório:
// vão num pedido avulso, sem ferramentas, diretrizes nem mapa, que custa algumas centenas
// de tokens em vez de milhares, e não entram no histórico da conversa. Confirmações ("ok",
// "pode", "certo") ficam de fora: podem ser a resposta a uma pergunta do modelo.
var smallTalkWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("oi oie ola opa salve hey hi hello e ai eai eae fala bom boa dia tarde noite tudo bem " +
		"como vai voce vc tranquilo tranquila " +
		"obrigado obrigada brigado valeu vlw thanks thx tchau ate mais logo dante") {
		smallTalkWords[w] = true
	}
}

func isSmallTalk(text string) bool {
	words := strings.FieldsFunc(fold(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	if len(words) == 0 || len(words) > 6 {
		return false
	}
	for _, w := range words {
		if !smallTalkWords[w] {
			return false
		}
	}
	return true
}

const smallTalkSystem = "Você é o dante, um revisor de código no terminal. A pessoa mandou um cumprimento ou " +
	"agradecimento, sem pergunta. Responda em português, numa frase curta e cordial, e, se for um cumprimento, diga em " +
	"uma linha o que você pode fazer: revisar as mudanças (tecla r), explicar o código, dizer onde mexer (/onde)."
