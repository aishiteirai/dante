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

### Windows (PowerShell, sem administrador)

```powershell
irm https://raw.githubusercontent.com/aishiteirai/dante/main/scripts/instalar.ps1 | iex
```

O script baixa o `dante.exe` da última Release, confere o SHA-256, instala em
`%LOCALAPPDATA%\dante` e coloca essa pasta no PATH do usuário. Para atualizar, rode de novo.
Se a rede bloquear o script, baixe `dante-windows-amd64.exe` na página de
[Releases](https://github.com/aishiteirai/dante/releases).

### Linux e macOS

```bash
curl -fsSL https://raw.githubusercontent.com/aishiteirai/dante/main/scripts/instalar.sh | sh
```

Instala em `~/.local/bin` (troque com `DANTE_DIR`). Nos dois scripts, `DANTE_VERSAO=v0.5.0`
escolhe uma versão específica.

### A partir do código

Requisitos: Go ≥ 1.25 (ou um Go mais antigo com `GOTOOLCHAIN=auto`, o padrão, que baixa o
1.25 sozinho) e git.

```bash
go install github.com/aishiteirai/dante/cmd/dante@latest
```

Ou, num clone, `make install`, que instala o binário em `~/.local/bin/dante`.

Em qualquer caso, o dante precisa do `git` instalado. `dante --versao` mostra a versão.

### Publicar uma versão

Cada tag `v*` enviada ao GitHub dispara o workflow `release`. Ele testa, compila para
Windows, Linux e macOS (amd64 e arm64) e publica uma Release com os binários e o
`SHA256SUMS.txt`, que é de onde os scripts acima baixam:

```bash
git tag v0.5.0 && git push origin v0.5.0
```

Para só compilar, sem publicar nada, rode o workflow pela aba **Actions → release → Run
workflow**: os binários ficam como artefato da execução. Localmente, `make dist VERSAO=v0.5.0`
gera os mesmos arquivos em `dist/`.

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
| OpenCode Console / Go | https://opencode.ai/console → Account → Personal API keys |

As chaves ficam em `~/.config/dante/credenciais.json`, um arquivo separado do `config.json`
e legível só pelo seu usuário (permissão 600). Na tela de modelos, cada provedor mostra de
onde vem a chave em uso. `x` remove a chave salva no dante.

Também dá para usar variáveis de ambiente, como antes: `ANTHROPIC_API_KEY`,
`DEEPSEEK_API_KEY`, `DASHSCOPE_API_KEY` ou `OPENCODE_API_KEY`. Quando há as duas, a chave salva no dante tem
prioridade.

### OpenCode

O dante traz os dois planos do [OpenCode](https://opencode.ai), que dão acesso a modelos como
DeepSeek, Qwen, Kimi, GLM e MiniMax com uma única chave (`oc_sk_…`):

- **OpenCode Console · pago por uso:** você coloca créditos e paga por requisição.
- **OpenCode Go · assinatura:** a partir de US$ 10 por mês, com uso incluído.

A mesma chave serve aos dois: cadastre num, e o outro já a usa. A lista de modelos é buscada
ao vivo no OpenCode, com uma lista fixa de reserva.

Os modelos grátis do OpenCode (`…-free`) **não funcionam no dante**: segundo a documentação
do OpenCode, eles só funcionam dentro do próprio opencode. Por isso o dante os esconde. Para
usar de graça, veja abaixo.

Com o Go, o dante se identifica (`dante/…`) e manda um ID por conversa no cabeçalho
`x-opencode-session`, como o OpenCode pede a clientes de fora. Modelos que treinam com os
dados ou que rodam fora da Europa e dos EUA precisam ser liberados em **Settings → Privacy**
no console do OpenCode. É o caso do DeepSeek, que pede a região **Global**.

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
   - **[1] Commits:** o histórico com o grafo, duas linhas por commit (assunto; autor, data e
     tamanho da mudança) e "✓ revisado" nos que você já conversou. As mudanças não
     commitadas aparecem no topo.
   - **[2] Arquivos:** o que mudou no commit escolhido, em árvore por pasta, com a situação
     por extenso (novo, modificado, removido, renomeado).
   - **[3] Visualizador:** uma coisa por vez, escolhida pelas abas do título. **Diff** é o
     arquivo selecionado, com numeração antiga e nova, fundo colorido e as palavras que
     mudaram destacadas. **Documento** é o arquivo que o modelo abriu.
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
| `f` | tela cheia; as setas continuam passando pelos painéis. No Visualizador, o diff passa a lado a lado |
| `z` | minimiza o painel para a barra de baixo (`▾ [3] Visualizador`) |
| `1` `2` `3` `4` | vai para o painel e o restaura se estiver minimizado |
| `0` | volta ao layout padrão |
| `m` / `o` / `u` / `q` | modelo / outro repositório / atualizar / sair |
| `p` | permissões do agente (ler ou editar) |

Com o mouse: clique dá foco, a roda rola o painel embaixo dela e arrastar a borda entre dois
painéis redimensiona. Para selecionar texto com o mouse capturado, segure `Shift` (na maioria
dos terminais), ou abra com `dante --sem-mouse`.

Teclas de cada painel (fora do chat): `Tab` passa para o próximo painel e `q` sai.

| Painel | Teclas |
|---|---|
| Commits | `↑↓` escolhe · `r` revisa o alvo inteiro · `enter` vai para os arquivos · `esc` troca de repositório |
| Arquivos | `↑↓` escolhe · `r` revisa o arquivo · `o` abre no `$EDITOR` · `d` volta ao diff · `enter` vai para o chat |
| Visualizador | `↑↓` `PgUp` `PgDn` `g` `G` rolam · `n`/`N` próximo e anterior trecho · `J`/`K` próximo e anterior arquivo · `[` `]` trocam a aba · `s` lado a lado · `+`/`-` mais ou menos contexto · `o` abre no editor na linha do trecho |
| Chat | `enter` envia · `alt+enter` quebra linha · `/` comandos de modo (`tab` completa) · `ctrl+g` referências · `ctrl+o` leituras em detalhe · `ctrl+z` desfaz as mudanças da última resposta · `PgUp`/`PgDn` rolam · `ctrl+n` nova conversa · `esc` cancela a resposta |

Em terminais com menos de 80 colunas, aparece um painel por vez, com as abas no topo.

Opções: `dante --repo CAMINHO_OU_URL` abre direto o repositório, e
`dante --modelo deepseek/deepseek-chat` pula a escolha do modelo.

### Revisar ou explicar

O chat tem dois tipos de tarefa e três formatos de resposta. O modo atual aparece no título
do chat (`‹revisar · normal›`) e vale até você trocar.

| Comando | O que faz | Também detectado quando você escreve… |
|---|---|---|
| `/revisar` | procura problemas (padrão) | "revise", "tem bug?", "está certo?" |
| `/explicar` | diz o que o código faz e por quê, sem julgar | "explique", "como funciona", "o que faz" |
| `/aprendiz` | sem jargão, com analogia, passo a passo e glossário | "como se eu fosse um estagiário", "sou iniciante" |
| `/rapido` | cinco linhas no máximo | "seja pontual", "resumo", "direto ao ponto" |
| `/normal` | o equilíbrio de sempre | "em detalhes", "aprofunde" |

Os comandos vão no começo da mensagem e podem ser combinados (`/explicar /aprendiz como
funciona o chat?`). Sozinhos, só trocam o modo. Sem comando, o dante lê o pedido e avisa
quando muda o modo por conta própria. A detecção nunca mexe em permissões.

**Como as respostas aparecem:**
- **Revisão:** um RESUMO e um cartão por problema, com a gravidade em cor, o local, a
  explicação e a sugestão. O modelo registra cada achado por uma ferramenta própria. Se ele
  não usar a ferramenta, como acontece com alguns modelos locais, a revisão vem em texto.
- **Referências:** cada `arquivo:linha` citado ganha um número `[n]`. `ctrl+g` lista as
  referências, e o número, ou `enter`, mostra o trecho no Visualizador, com a linha marcada
  por `▶`: no diff, se o arquivo mudou no commit, ou o arquivo inteiro na aba Documento.
- **Leituras do modelo:** ficam agrupadas numa linha ("✓ 3 leituras: …"); `ctrl+o` mostra
  uma por linha.

### Mapa do projeto e tour

`/mapa` gera um mapa do projeto, só quando você pede. Antes, o dante mostra quantos arquivos
vai ler, a estimativa de tokens e a lista de modelos. Escolha um modelo barato, como o
Haiku, o `deepseek-chat` ou um modelo local do Ollama (grátis, mais lento). O último modelo
escolhido fica lembrado.

- Cada pasta é resumida à parte: o papel dela, uma linha por arquivo com `arquivo:linha` e
  com o que ela se liga. No fim vêm a visão geral, por onde começar, o fluxo principal e um
  glossário.
- Arquivos sensíveis, binários, muito grandes e de trava (`go.sum`, `package-lock.json`…)
  ficam de fora, e valores com cara de segredo são mascarados.
- O mapa fica salvo em `~/.local/share/dante/mapas/`. Rodar `/mapa` de novo resume só as
  pastas que mudaram; se nada mudou, ele só abre o mapa.
- Uma versão compacta do mapa (~1 a 4 mil tokens) vai como contexto no começo de cada
  conversa nova, para o modelo se orientar sem reabrir tudo.

`/tour` abre a aba **Mapa** do Visualizador no primeiro capítulo: visão geral, por onde
começar, uma pasta por capítulo, o fluxo principal e o glossário. `←`/`→` trocam de
capítulo, `[`/`]` trocam de aba, e cada `arquivo:linha` vira uma referência `[n]` que
`ctrl+g` abre. Combina com `/explicar /aprendiz` para perguntar sobre o que leu.

O modelo barato de cada provedor fica em `modelo_mapa` no `config.json`.

### O que o modelo pode fazer

O agente tem dois modos, e o atual aparece no título do chat:

| Modo | Chip | O que o modelo pode fazer |
|---|---|---|
| `/ler` (padrão) | `◉ ler` | ler o repositório e sugerir; uma mudança proposta aparece como cartão "sugestão", e nada é gravado |
| `/editar` | `✎ editar · perguntar` | editar, criar e apagar arquivos, conforme as permissões |

O dante sempre abre no modo ler e nunca troca de modo sozinho. Se você pedir uma mudança
no modo ler ("corrija…", "refatore…"), ele só avisa que o modelo vai sugerir.

**Permissões do modo editar** (`/permissoes` ou `ctrl+space p`; salvas no `config.json`):

| Ação | Padrão | Valores |
|---|---|---|
| editar arquivos | perguntar | perguntar, permitir, negar |
| criar arquivos | perguntar | perguntar, permitir, negar |
| apagar arquivos | negar | perguntar, permitir, negar |

```json
"permissoes": { "editar": "perguntar", "criar": "perguntar", "apagar": "negar" }
```

**Aprovação:** cada mudança chega como um cartão no chat, com o mini-diff, e o diff inteiro
aparece no Visualizador. Teclas, de qualquer painel:

- `s` aplica; `n` (ou `esc`) recusa, e o modelo é avisado para não insistir;
- `t` aplica esta e as próximas mudanças da mesma resposta sem perguntar;
- `e` abre a proposta no seu editor (`$VISUAL`/`$EDITOR`); o que você salvar é aplicado.

**Desfazer:** `ctrl+z` desfaz todas as mudanças da última resposta que mudou arquivos (e
`ctrl+z` de novo, a anterior). Um arquivo que você editou depois não é sobrescrito: o dante
avisa e deixa como está.

**Regras fixas,** que nenhuma permissão muda: o agente nunca escreve em arquivos sensíveis
(os mesmos que ele não lê), na pasta `.git` ou fora do repositório, nem por link simbólico.
Uma mudança só é aplicada se o arquivo continua como estava quando o modelo a propôs.

As ferramentas de leitura são `read_file` (inclusive a versão de um commit antigo),
`read_files`, `git_diff`, `grep`, `list_files`, `git_log` e `git_blame`; as de escrita,
`edit_file`, `write_file` e `delete_file`. Todas ficam presas à pasta do repositório, sem
shell. Para editar você mesmo, use `o`, que abre o arquivo no seu editor.

### Segredos não saem da sua máquina

- **Arquivos sensíveis são bloqueados** em todas as ferramentas: `.env*`, `*.pem`, `*.key`,
  `id_rsa`, `credentials.json`, `.npmrc`, `*.tfstate` e outros. Os arquivos de exemplo
  (`.env.example`, `*.pub`) continuam liberados. O diff de um arquivo bloqueado aparece como
  "(diff omitido)", e o modelo vê só o nome.
- **Valores com cara de segredo são mascarados** em tudo o que vai para o modelo: chaves da
  Anthropic, OpenAI, AWS, GitHub, Slack e Google, JWTs, chaves privadas e atribuições como
  `password = "..."`. Eles viram `«segredo omitido»`.
- Ajuste no `config.json` com `arquivos_bloqueados` e `arquivos_liberados` (padrões glob).

### Gasto de tokens

Cada vez que o modelo pede uma ferramenta, a conversa inteira é reenviada a ele, e o
provedor cobra por esse reenvio. Por isso o dante evita rodadas desnecessárias:

- **O diff vai na sua mensagem.** Ao perguntar sobre um commit ou arquivo, o diff com 10
  linhas de contexto vai junto, uma vez por conversa. Diffs grandes (mais de 60 KB ou de
  1/4 do contexto) viram um aviso, e o modelo pede o diff por arquivo.
- **O modelo lê em lote.** A ferramenta `read_files` abre vários arquivos ou trechos numa
  chamada só, e as instruções pedem para abrir só o que o diff não mostra.
- **Cada pergunta tem um orçamento.** Por padrão são 150 mil tokens, somando todas as
  rodadas. Passado o orçamento, o dante não executa mais ferramentas e pede ao modelo para
  concluir com o que já leu. Ajuste em `orcamento_tokens` no `config.json` (`-1` desliga).
- **Cada resposta mostra quanto gastou** (`↳ 58,2 mil tokens nesta resposta`), e o cabeçalho
  mostra o total da conversa.

Numa simulação da revisão de um commit de 18 arquivos, essas mudanças levaram o gasto de
20 requisições e cerca de 1 milhão de tokens para 3 requisições e cerca de 80 mil. O número
real depende de o modelo seguir as instruções.

### Conversas longas

Cada modelo tem um limite de contexto, e o dante cuida dele:
- **Claude:** o próprio servidor limpa os resultados antigos de ferramentas quando a conversa
  passa de 150 mil tokens.
- **Outros provedores:** quando a conversa passa de 70% do `contexto` do provedor (no
  `config.json`; sem o campo, o dante assume 64 mil tokens), o dante troca os resultados de
  ferramentas de rodadas anteriores por um aviso. O modelo pode ler o arquivo de novo se
  precisar. Resultados grandes demais (mais de 1/4 do contexto) são cortados. Quando nem
  assim a conversa cabe, o chat avisa, e `ctrl+n`
  começa outra.
- **Ollama:** o dante usa a API nativa e define o tamanho do contexto (`num_ctx`, 16 mil
  tokens por padrão). Sem isso, o Ollama usa uma janela pequena e corta o começo da conversa
  sem avisar. Para revisões maiores, aumente `"contexto"` no provedor `ollama`. Mais contexto
  usa mais memória.

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
