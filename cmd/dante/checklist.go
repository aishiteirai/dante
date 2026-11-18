package main

import (
	"regexp"
	"strings"
)

// Listas de verificação da revisão, escolhidas pelo que o diff toca (sem o modelo). Só
// entram as que se aplicam, para não gastar tokens nem diluir a atenção do modelo.

type check struct {
	name  string
	when  *regexp.Regexp // nil = sempre
	items string
}

var checks = []check{
	{"Erros e casos de borda", nil,
		"erros ignorados ou engolidos; valores nulos, vazios ou no limite; o que acontece se a chamada falhar no meio"},
	{"Código limpo", nil,
		"duplicação que já existe em outro lugar; nomes que não dizem o que a coisa é; funções longas demais; código morto ou comentado"},
	{"Testes", nil,
		"o comportamento novo tem teste? algum teste existente deixou de cobrir o que mudou?"},
	{"Entrada externa e injeção", regexp.MustCompile(`(?i)\b(select|insert|update|delete)\b.{0,40}\b(from|into|set|where)\b|\bquery|\bexec(ute)?\(|exec\.Command|subprocess|os\.system|eval\(|innerHTML|dangerouslySetInnerHTML|template`),
		"SQL, comandos, HTML ou templates montados com dados de fora sem parâmetros ou escape"},
	{"HTTP, autenticação e permissões", regexp.MustCompile(`(?i)http\.|request|response|handler|router|middleware|\bauth|token|session|cookie|jwt|permission|role|cors`),
		"rota nova sem checar quem pode chamar; dados de outro usuário acessíveis; validação de entrada; respostas que vazam detalhes internos"},
	{"Concorrência", regexp.MustCompile(`(?i)\bgo func|goroutine|sync\.|mutex|\bchan\b|<-|async |await |thread|lock\(|atomic|promise\.all`),
		"dado compartilhado sem trava; ordem de execução suposta; goroutine ou tarefa que nunca termina; cancelamento"},
	{"Arquivos e caminhos", regexp.MustCompile(`(?i)os\.(open|create|remove|writefile|readfile)|filepath\.|path\.join|open\(|fs\.|unlink|mkdir`),
		"caminho vindo de fora (../), permissões do arquivo criado, arquivo que fica aberto"},
	{"Segredos e criptografia", regexp.MustCompile(`(?i)password|passwd|secret|api[_-]?key|private[_-]?key|crypto|hash|bcrypt|md5|sha1|random`),
		"segredo no código ou em log; algoritmo fraco (md5, sha1 para senha); aleatório que não é criptográfico"},
	{"Banco de dados e migrações", regexp.MustCompile(`(?i)migration|migrate|schema|alter table|create table|transaction|\.begin\(|commit\(|rollback`),
		"migração que perde dados ou trava a tabela; falta de transação; índice para a consulta nova"},
	{"Desempenho", regexp.MustCompile(`(?i)for .* range|for \(|\.map\(|\.foreach|while |select .* from|findall|query`),
		"consulta ou chamada de rede dentro de laço (N+1); trabalho repetido que podia ser feito uma vez"},
}

// reviewChecklist monta o bloco <verificar> para o diff da revisão.
func reviewChecklist(diff string) string {
	// Só as linhas alteradas contam: o contexto não diz o que a mudança toca.
	var changed strings.Builder
	for _, l := range strings.Split(diff, "\n") {
		if (strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-")) && !strings.HasPrefix(l, "+++") && !strings.HasPrefix(l, "---") {
			changed.WriteString(l[1:] + "\n")
		}
	}
	c := changed.String()
	var b strings.Builder
	b.WriteString("<verificar>\nConfira, nas mudanças, o que se aplicar (sem forçar achados):\n")
	for _, ch := range checks {
		if ch.when == nil || ch.when.MatchString(c) {
			b.WriteString("- " + ch.name + ": " + ch.items + "\n")
		}
	}
	b.WriteString("</verificar>\n")
	return b.String()
}
