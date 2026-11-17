# dante

Revisor de código para o terminal. Você navega pelos commits e arquivos alterados e
conversa com um modelo, que abre os arquivos e opina sobre as mudanças. Funciona com
**Claude**, **DeepSeek**, **Qwen** e modelos locais (**Ollama**, **LM Studio**).

```
dante · meu-app (main)                                         DeepSeek · deepseek-chat · 3.1k tokens
╭ [1] Commits ──────────────╮╭ [2] Arquivos 6f1940f ──────────╮╭ [3] Chat ─────────────────────────╮
│ ● Mudanças locais (2)     ││ A util.py              +11 -0  ││ você                              │
│ * 6f1940f (HEAD→main) …   ││ M db.py                 +5 -1  ││ Revise as mudanças em util.py     │
│ * 91586aa busca por nome  ││ ── aberto pelo modelo ──────── ││                                   │
│ * 28abf76 base            ││   10  def caminho_upload(…     ││ deepseek-chat                     │
│                           ││   11      return os.path.join… ││   ✓ abriu util.py                 │
│                           ││                                ││ ### 1. Path traversal — util.py:11│
╰───────────────────────────╯╰────────────────────────────────╯╰───────────────────────────────────╯
```

## Instalação

Requisitos: Go ≥ 1.25 (ou um Go mais antigo com `GOTOOLCHAIN=auto`, o padrão, que baixa o
1.25 sozinho) e git.

```bash
go install github.com/aishiteirai/dante/cmd/dante@latest
```

Ou a partir do código:

```bash
git clone https://github.com/aishiteirai/dante.git && cd dante
make install        # instala o binário em ~/.local/bin/dante
```

O `dante` é um binário único: não precisa de Python nem da pasta do projeto depois de
instalado.

## Modelos

Defina a chave do provedor que for usar:

| Provedor | Variável | Modelos sugeridos |
|---|---|---|
| Claude (Anthropic) | `ANTHROPIC_API_KEY` | `claude-opus-5-5`, `claude-sonnet-5-5`, `claude-haiku-5-5` |
| DeepSeek | `DEEPSEEK_API_KEY` | `deepseek-chat`, `deepseek-reasoner` |
| Qwen (Alibaba Cloud) | `DASHSCOPE_API_KEY` | `qwen3-coder-plus`, `qwen-max`, `qwen-plus` |
| Ollama (local) | — | os que você baixou (`ollama pull qwen2.5-coder`) |
| LM Studio (local) | — | os carregados no LM Studio |

Os servidores locais são detectados sozinhos, e seus modelos aparecem na lista. Para rodar
DeepSeek ou Qwen de graça na sua máquina:

```bash
ollama pull qwen2.5-coder:14b    # ou deepseek-coder-v2, qwen3-coder...
```

Use um modelo com suporte a ferramentas (*tool calling*). Modelos pequenos (menos de 7B)
costumam errar as chamadas.

Na primeira execução, o dante grava `~/.config/dante/config.json` com os provedores acima.
Edite esse arquivo para:
- trocar os nomes dos modelos;
- mudar a região do Qwen;
- adicionar outros provedores compatíveis com a API da OpenAI (OpenRouter, vLLM, Groq...).

Exemplo:

```json
{"id": "openrouter", "nome": "OpenRouter", "tipo": "openai",
 "url": "https://openrouter.ai/api/v1", "chave_env": "OPENROUTER_API_KEY",
 "modelos": ["qwen/qwen3-coder", "deepseek/deepseek-chat-v3.1"]}
```

Para servidores locais, use `"listar_modelos": true, "local": true` no lugar de `chave_env`.

No Claude, o esforço (`low` a `max`) controla a profundidade da análise. Ele muda com a tecla
`e` na tela de modelos. Nos modelos da Anthropic que aceitam, o dante também ativa o
fallback automático do servidor: se um pedido for recusado, outro modelo o atende.

## Como usar

```bash
dante
```

1. **Repositório.** Se você estiver dentro de um repositório git, o dante pergunta se pode
   confiar e abri-lo, ou se você quer abrir outro. Para outro, aceita:
   - um caminho (`tab` completa);
   - uma URL do git (é clonada em `~/.local/share/dante/repos/`);
   - o atalho `dono/repo` do GitHub.
2. **Modelo.** Escolha quem vai revisar. A escolha fica salva para a próxima vez.
3. **Área de trabalho**, com três painéis:
   - **Commits:** o histórico com o grafo, como no lazygit. As mudanças não commitadas
     aparecem no topo.
   - **Arquivos:** o que mudou no commit escolhido e o diff do arquivo selecionado.
   - **Chat:** converse com o modelo. Cada mensagem leva junto o contexto, ou seja, o commit
     e o arquivo em foco. O modelo abre os arquivos sozinho, e o painel do meio mostra o
     documento que ele está lendo.

| Painel | Teclas |
|---|---|
| Qualquer | `tab`/`shift+tab` ou `1` `2` `3` troca de painel · `m` modelo · `u` atualiza · `q` sai |
| Commits | `↑↓` escolhe · `r` revisa o alvo inteiro · `enter` vai para os arquivos · `esc` troca de repositório |
| Arquivos | `↑↓` escolhe · `J`/`K` rola o diff · `r` revisa o arquivo · `o` abre no `$EDITOR` · `d` volta ao diff |
| Chat | `enter` envia · `alt+enter` nova linha · `pgup`/`pgdn` rola · `ctrl+n` nova conversa · `esc` cancela a resposta |

Em terminais estreitos (menos de 110 colunas), aparece um painel por vez.

Opções: `dante --repo CAMINHO_OU_URL` abre direto o repositório, e
`dante --modelo deepseek/deepseek-chat` pula a escolha do modelo.

### O que o modelo pode fazer

Por enquanto, só ler. As ferramentas são `read_file` (inclusive a versão de um commit
antigo), `git_diff`, `grep`, `list_files`, `git_log` e `git_blame`. Todas ficam presas à
pasta do repositório, sem shell e sem escrita. Para editar um arquivo, use `o`, que abre o
arquivo no seu editor.

### Diretrizes do time

Se o repositório tiver `.dante/diretrizes.md`, `DANTE.md` ou `REVIEW.md`, o conteúdo vai
junto com as instruções do modelo. Há um modelo em `exemplos/diretrizes.md`.

### Onde ficam os dados

```
~/.config/dante/config.json       provedores, último modelo, repositórios confiados
~/.local/share/dante/conversas/   conversas salvas em markdown, por repositório
~/.local/share/dante/repos/       clones feitos a partir de URLs
```

## Próximos passos

- Editar arquivos dentro do dante e salvar.
- Pedir melhorias ao agente: ferramentas de escrita, com confirmação antes de aplicar.

## CLI `revisor` (opcional)

O repositório também traz `revisor`, uma CLI em Python sobre o Claude Agent SDK que revisa
sem interface. Serve para hooks e CI:

```bash
make install-cli
revisor revisar --base origin/main --falhar-em alta   # ex.: num hook de pre-push
```

Veja `revisor --help`. O histórico dela fica em `~/.local/share/dante/historico/`.

## Desenvolvimento

```
cmd/dante/        interface (Bubble Tea): telas, painéis, chat
internal/llm/     provedores: Anthropic (SDK oficial) e compatível com OpenAI; laço de ferramentas
internal/repo/    git (log, arquivos, diffs) e ferramentas de leitura do modelo
internal/config/  configuração de provedores
revisor/          CLI opcional em Python
```

```bash
make test       # testes Go (provedores testados contra servidores falsos)
make test-cli   # testes da CLI Python
```
