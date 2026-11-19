# dante — notas do projeto

Revisor de código para o terminal: o usuário navega por commits e arquivos alterados e
conversa com um modelo (Claude, DeepSeek, Qwen, Ollama, OpenRouter...) que abre os arquivos
com ferramentas de leitura e opina sobre as mudanças.

## Propósito (decidido com o Ryan)

O dante é um **copiloto de leitura** que fica entre a pessoa e o agente que escreve código
(Claude Code e afins): ajuda a entender um projeto, saber onde mexer, revisar o que o agente
fez e organizar commits. **Não compete em autonomia de edição.** É **open source** para uso
diário de qualquer pessoa: funcionalidades indispensáveis de um bom harness entram mesmo sem
pedido explícito (atalhos claros, erros úteis, config simples, docs, testes, estabilidade).

Princípios: montar o contexto **sem o modelo** (git, grep, mapa) e gastar o mínimo de tokens;
funcionar com qualquer provedor, inclusive gratuitos e locais; segredos nunca saem da máquina;
interface e respostas em português, didáticas quando pedido.

Escopo: o dante é a **camada de entendimento e revisão com IA** do fluxo "pessoa + agente de
código". Junta o que as ferramentas fixas já sabem (git, Qlty, Ultracite, os linters do
projeto) com o raciocínio do modelo, para entender um projeto, saber onde mexer, revisar o
que o agente fez (antes, durante e depois) e organizar commits e PRs. Funciona no terminal,
dentro do Claude Code (hook e MCP), na IDE e no CI.
**Fora do escopo:** lintar, formatar ou medir por conta própria (usa Qlty, Ultracite/Biome/
Oxlint/ESLint, ruff, go vet, golangci-lint, tsc), painel na nuvem e autonomia de edição.
Das ferramentas fixas, o dante mostra **só o que a mudança introduziu** (como o Qlty).

Fluxo que guia o roteiro: projeto novo → entender a estrutura → saber onde mexer → pedir ao
Claude → revisar o que ele fez → commits e PR organizados.

Fases:
1. Feito. Revisão: branch como um PR, seção Impacto, listas de verificação, /corrigir.
2. Feito. Commits: /commits (plano em Conventional Commits, o git só roda com confirmação), /pr.
3. Feito. Onboarding: aba Arquivo, explorador do projeto, /onde (com briefing), /como.
4. Feito. Verificações locais: /verificar roda as ferramentas do projeto nos arquivos
   alterados (permissão por repositório) e mostra só os problemas novos; na revisão, vão ao
   modelo como fatos (`internal/checks/`, `cmd/dante/checksview.go`).
5. Feito. Integração com o agente e o CI (`cmd/dante/headless.go`, `mcp.go`, `regras.go`):
   `dante review/check/explain/ask` sem interface; `dante review --hook` com `dante hook
   instalar` (devolve os problemas novos ao Claude Code antes de a pessoa revisar);
   `--ci --falhar-em` (quality gate); `dante mcp` (ferramentas de contexto sem tokens, com
   `dante mcp instalar`); /regras e `dante regras` (rascunho do CLAUDE.md/AGENTS.md).
6. Feito. Aprendizado (`internal/tech/`, `cmd/dante/learn.go`): /tecnologias e `dante tecnologias`
   (manifestos + código, sem tokens; catálogo em `internal/tech/catalog.go`), /trilha NOME,
   /oque TERMO e `i` no Visualizador (perguntas à parte com o contexto montado pelo dante).
Em paralelo, feito: `dante eval` (`cmd/dante/eval.go`), perguntas de teste pelo caminho do chat
(tokens, rota, tarefa, formato; `--sem-ia` sem tokens).

## Convenções

- Interface, mensagens, docs e commits em português.
- Commits como `Ryan Alvarenga <ryancorazzaalvarenga@gmail.com>` (a conta do GitHub dele,
  não o e-mail profissional), sem linhas de co-autoria, até ele pedir o contrário.
- Antes de commitar: `go vet ./... && go test ./...` precisam passar (`make test`).
- Commitar e fazer merge só com a aprovação do Ryan.
- O dante é um binário Go independente; a CLI Python `revisor/` é opcional (hooks/CI).

## Mapa

- `cmd/dante/` interface (Bubble Tea v1): telas (confiança, repositório, modelo, chave),
  área de trabalho com três painéis (Navegador com commits e arquivos em árvore;
  Visualizador em cima do chat, à direita), chat.
- `internal/llm/` provedores: Anthropic (SDK oficial) e compatível com OpenAI; laço de
  ferramentas; validação de chave; download de modelos do Ollama.
- `internal/repo/` git (log com grafo, arquivos, diffs) e ferramentas de leitura do modelo,
  presas à pasta do repositório.
- `internal/mapa/` mapa do projeto (/mapa): varredura, resumo por pasta com um modelo
  barato, visão geral, salvamento incremental por hash de pasta. Sem dependência da interface.
- `internal/tech/` tecnologias do projeto (manifestos, linguagens, onde cada uma é usada).
- `internal/config/` provedores, último modelo, repositórios confiados; chaves em
  `credenciais.json` (0600).

## Roteiro

### Addon do noct (decidido, ainda não começado)

O dante deve continuar sendo um **aplicativo independente** e, além disso, virar um
**addon do multiplexador noct** (https://github.com/AlexandreSJ/noct, Go, lipgloss v2).

O que o noct diz sobre mods (`docs/instalador.md`, linha "Mods de usuário que desenham na
TUI", e `docs/mcp.md`): em Go não há carregamento seguro de código, então um mod é **um
programa à parte** que desenha no próprio painel (um terminal do noct, o que já funciona com
o dante hoje) e pede coisas ao workspace pelo **mesmo protocolo do MCP do noct**
(`list_panes`, `run`, `send_input`), com a política de aprovações que o noct já tem. Falta, do
lado do noct, um cliente local e um manifesto de permissões.

Implicações para o desenho do dante:
- Manter a lógica (llm, repo, config) separada da interface, para poder ter modos como
  `dante chat` ou `dante log` rodando cada um num painel do noct, deixando o layout para ele.
- O gerenciador de painéis do próprio dante (inspirado no noct: foco por direção, mover,
  redimensionar, empilhar, tela cheia, minimizar) vale para o modo independente; dentro do
  noct, o natural é ceder o layout ao noct.
- Atalhos do dante não devem brigar com o prefixo do noct (`Ctrl+Space`) quando rodar
  dentro dele: tornar a tecla prefixo configurável.

### Próximos passos já combinados

- Editar arquivos dentro do dante e salvar.

Feito: agente ler/editar (`cmd/dante/agent.go`, `internal/repo/write.go`). As ferramentas
de escrita vão em toda conversa, porque a lista não pode mudar no meio dela (o Claude trata
isso como edição do histórico); no modo ler, elas recusam.

Feito: `/mapa` e `/tour` (`internal/mapa/`, `cmd/dante/mapview.go`). O mapa compacto vai no
prompt de sistema, então só vale a partir da conversa seguinte à geração.
