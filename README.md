# dante

Revisor de código para o terminal. Você navega pelos commits e arquivos alterados e
conversa com um modelo, que abre os arquivos e opina sobre as mudanças. Funciona com
**Claude**, **DeepSeek**, **Qwen** e modelos locais (**Ollama**, **LM Studio**).

```
dante · meu-app (main)                                          DeepSeek · deepseek-chat · 3.1k tokens
╭─ [1] Commits ───────────╮╭─ [3] Documento · util.py (aberto pelo modelo) ─╮╭─ [4] Chat ──────────────╮
│ ● Mudanças locais (2)   ││    10  def caminho_upload(base, nome):         ││ você                    │
│ * 6f1940f (HEAD→main) … ││    11      return os.path.join(base, nome)     ││ Revise util.py          │
│ * 91586aa busca por nome││                                                ││                         │
╰─────────────────────────╯│                                                ││ deepseek-chat           │
╭─ [2] Arquivos 6f1940f ──╮│                                                ││   ✓ abriu util.py       │
│ A util.py        +11 -0 ││                                                ││ 1. Path traversal …     │
│ M db.py           +5 -1 ││                                                ││ › _                     │
╰─────────────────────────╯╰────────────────────────────────────────────────╯╰─────────────────────────╯
ctrl+space comandos · alt+setas foco · ↑↓ arquivo · r revisar · o editor           ▾ minimizados
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

### Conectar a API de um provedor

O jeito mais simples é cadastrar a chave pelo próprio dante:

1. Abra o `dante` e vá até a tela de modelos (ela aparece ao abrir um repositório, ou com a
   tecla `m`).
2. Escolha um modelo do provedor e pressione `enter`. Se o provedor ainda não tiver chave, o
   dante pede a chave. Para cadastrar ou trocar a chave a qualquer momento, pressione `c` sobre
   o provedor.
3. Cole a chave e pressione `enter`. O dante testa a chave com o provedor, sem gastar tokens,
   e a salva.

| Provedor | Onde gerar a chave |
|---|---|
| Claude (Anthropic) | https://platform.claude.com/settings/keys |
| DeepSeek | https://platform.deepseek.com/api_keys |
| Qwen (Alibaba Cloud) | Alibaba Cloud Model Studio → API Key |

As chaves ficam em `~/.config/dante/credenciais.json`, um arquivo separado do `config.json`
e legível só pelo seu usuário (permissão 600). Na tela de modelos, cada provedor mostra de
onde vem a chave em uso. `x` remove a chave salva no dante.

Também dá para usar variáveis de ambiente, como antes: `ANTHROPIC_API_KEY`,
`DEEPSEEK_API_KEY` ou `DASHSCOPE_API_KEY`. Quando há as duas, a chave salva no dante tem
prioridade.

### Modelos grátis

Há duas opções sem custo, e as duas aparecem na tela de modelos.

**Ollama (local).** O modelo roda no seu computador: sem conta, sem chave, e seu código não
sai da máquina.

1. Instale o Ollama em https://ollama.com e deixe-o aberto.
2. Na tela de modelos, as sugestões aparecem com `↓`. Pressione `enter` numa delas para
   baixar, com o progresso na tela. Ao terminar, o modelo já fica pronto para usar.

| Modelo | Download | Para quem |
|---|---|---|
| `qwen2.5-coder:7b` | 4,7 GB | bom para começar; ~8 GB de RAM |
| `qwen3:8b` | 5,2 GB | raciocina antes de responder |
| `deepseek-r1:8b` | 5,2 GB | DeepSeek local |
| `qwen2.5-coder:14b` | 9,0 GB | revisões melhores; ~16 GB de RAM |
| `qwen3-coder:30b` | 19 GB | o mais forte; pede GPU ou 32 GB de RAM |

O LM Studio também funciona: os modelos carregados nele aparecem sozinhos.

**OpenRouter (nuvem, grátis com limite).** O dante lista ao vivo os modelos de preço zero do
OpenRouter que aceitam ferramentas, como as versões grátis de DeepSeek e Qwen. Eles aparecem
primeiro na lista.

1. Crie uma conta gratuita em https://openrouter.ai e gere uma chave em
   https://openrouter.ai/settings/keys.
2. Escolha um modelo `…:free` e cole a chave quando o dante pedir.

Os modelos grátis do OpenRouter têm limite de requisições por minuto e por dia. Alguns só
funcionam se você liberar, em https://openrouter.ai/settings/privacy, que o provedor use os
dados enviados. Se faltar essa permissão, o dante mostra o aviso. Para código privado, prefira
o Ollama.

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

O `chave_env` é opcional: um provedor novo também recebe a chave pela tecla `c`. Para
servidores locais, use `"listar_modelos": true, "local": true`.

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
3. **Área de trabalho.** São quatro painéis:
   - **[1] Commits:** o histórico com o grafo. As mudanças não commitadas aparecem no topo.
   - **[2] Arquivos:** o que mudou no commit escolhido.
   - **[3] Diff:** o diff do arquivo selecionado. Quando o modelo abre um arquivo, este
     painel mostra o documento que ele está lendo.
   - **[4] Chat:** a conversa com o modelo. Cada mensagem leva junto o contexto, ou seja, o
     commit e o arquivo em foco.

### Organizar os painéis

Os painéis funcionam como no [noct](https://github.com/AlexandreSJ/noct) e no i3: ficam em
colunas, o foco anda pelas setas para o painel daquele lado, e o layout é salvo em
`~/.config/dante/layout.json`.

`Ctrl+Space` abre o popup de comandos. As mesmas teclas também funcionam direto com `Alt`,
inclusive enquanto você digita no chat:

| Tecla (depois de `Ctrl+Space`, ou com `Alt`) | O que faz |
|---|---|
| `←↑↓→` / `hjkl` | foco no painel ao lado (`Ctrl+Space` continua aberto para andar mais) |
| `a`, depois as setas | move o painel: entre colunas com `←→`, dentro da coluna com `↑↓`; na borda, vira uma coluna nova (`Alt+Shift+setas` move direto) |
| `n`, depois as setas | redimensiona: `→↓` aumentam, `←↑` diminuem |
| `w` | empilha a coluna: só o painel com foco fica aberto, e os outros viram uma linha `▸ título` (de novo, separa) |
| `f` | tela cheia; as setas continuam passando pelos painéis |
| `z` | minimiza o painel para a barra de baixo (`▾ [3] Diff`) |
| `1` `2` `3` `4` | vai para o painel e o restaura se estiver minimizado |
| `0` | volta ao layout padrão |
| `m` / `o` / `u` / `q` | modelo / outro repositório / atualizar / sair |

Com o mouse: clique dá foco, a roda rola o painel embaixo dela e arrastar a borda entre dois
painéis redimensiona. Para selecionar texto com o mouse capturado, segure `Shift` (na maioria
dos terminais), ou abra com `dante --sem-mouse`.

Teclas de cada painel (fora do chat): `Tab` passa para o próximo painel e `q` sai.

| Painel | Teclas |
|---|---|
| Commits | `↑↓` escolhe · `r` revisa o alvo inteiro · `enter` vai para os arquivos · `esc` troca de repositório |
| Arquivos | `↑↓` escolhe · `r` revisa o arquivo · `o` abre no `$EDITOR` · `d` volta ao diff · `enter` vai para o chat |
| Diff | `↑↓` `PgUp` `PgDn` `g` `G` rolam · `o` abre no editor · `d` volta ao diff |
| Chat | `enter` envia · `alt+enter` quebra linha · `PgUp`/`PgDn` rolam · `ctrl+n` nova conversa · `esc` cancela a resposta |

Em terminais com menos de 80 colunas, aparece um painel por vez, com as abas no topo.

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
~/.config/dante/credenciais.json  chaves de API cadastradas no dante (permissão 600)
~/.config/dante/layout.json       organização dos painéis
~/.local/share/dante/conversas/   conversas salvas em markdown, por repositório
~/.local/share/dante/repos/       clones feitos a partir de URLs
```

## Próximos passos

- Addon do noct: o dante continua independente, mas também poderá rodar como mod do noct
  (veja `CLAUDE.md`).
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
