# dante — notas do projeto

Revisor de código para o terminal: o usuário navega por commits e arquivos alterados e
conversa com um modelo (Claude, DeepSeek, Qwen, Ollama, OpenRouter...) que abre os arquivos
com ferramentas de leitura e opina sobre as mudanças.

## Convenções

- Interface, mensagens, docs e commits em português.
- Commits com o nome do dono do repositório (Ryan Alvarenga), sem linhas de co-autoria,
  até ele pedir o contrário.
- Antes de commitar: `go vet ./... && go test ./...` precisam passar (`make test`).
- O dante é um binário Go independente; a CLI Python `revisor/` é opcional (hooks/CI).

## Mapa

- `cmd/dante/` interface (Bubble Tea v1): telas (confiança, repositório, modelo, chave),
  área de trabalho com painéis, chat.
- `internal/llm/` provedores: Anthropic (SDK oficial) e compatível com OpenAI; laço de
  ferramentas; validação de chave; download de modelos do Ollama.
- `internal/repo/` git (log com grafo, arquivos, diffs) e ferramentas de leitura do modelo,
  presas à pasta do repositório.
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
- Pedir melhorias ao agente (ferramentas de escrita, com confirmação antes de aplicar).
