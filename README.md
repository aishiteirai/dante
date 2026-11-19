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

## O que o dante é (e o que não é)

O dante é a **camada de entendimento e revisão com IA** para quem trabalha com agentes de
código: entender um projeto, saber onde mexer, revisar o que o agente fez e organizar commits
e PRs. Ele junta o que as ferramentas fixas já sabem (git, linters) com o raciocínio do modelo.

Ele **não** substitui linters, formatadores ou plataformas de qualidade como
[Qlty](https://qlty.sh) e [Ultracite](https://www.ultracite.ai): **usa** essas ferramentas
quando o projeto as tem e mostra só o que a mudança introduziu. Elas respondem "o código
segue as regras?"; o dante responde "o que a mudança faz, quem ela afeta e onde está o erro
de lógica?".

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

Também dá para publicar sem usar o git: na aba **Actions → release → Run workflow**,
escolha a branch (`main`) e escreva a tag no campo "tag" (ex.: `v0.6.0-alpha`). Se a tag não
existe, o workflow a cria no último commit da branch; se já existe, recompila e anexa os
arquivos de novo. Com o campo vazio, ele só compila, e os binários ficam como artefato da
execução. Localmente, `make dist VERSAO=v0.5.0`
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
| Gemini (Google) | https://aistudio.google.com/apikey |
| DeepSeek | https://platform.deepseek.com/api_keys |
| Qwen (Alibaba Cloud) | Alibaba Cloud Model Studio → API Key |
| OpenCode Console / Go | https://opencode.ai/console → Account → Personal API keys |

Na tela de modelos, os provedores prontos para usar vêm primeiro, e só o do cursor fica aberto
com os modelos (`↑↓` passa de um provedor para outro). Os que ainda não têm chave ficam no fim,
uma linha cada. `/` busca um modelo pelo nome em todos os provedores.

As chaves ficam em `~/.config/dante/credenciais.json`, um arquivo separado do `config.json`
e legível só pelo seu usuário (permissão 600). Na tela de modelos, cada provedor mostra de
onde vem a chave em uso. `x` remove a chave salva no dante.

Também dá para usar variáveis de ambiente, como antes: `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`,
`DEEPSEEK_API_KEY`, `DASHSCOPE_API_KEY` ou `OPENCODE_API_KEY`. Quando há as duas, a chave salva no dante tem
prioridade.

### Gemini

O Gemini entra pela API do Google compatível com a da OpenAI. Gere a chave no
[Google AI Studio](https://aistudio.google.com/apikey) e cadastre-a no dante (tecla `c` sobre
"Gemini (Google)" na tela de modelos). A lista de modelos vem da própria conta, só com os de
conversa (sem embeddings, imagem ou áudio), com os "pro" primeiro e os mais novos antes.

- O contexto é de 1 milhão de tokens, então o pacote de revisão vai sempre no tamanho cheio.
- Os modelos que raciocinam devolvem uma "assinatura do raciocínio" em cada chamada de
  ferramenta, e o dante a devolve na rodada seguinte, como o Gemini exige.
- A chave do AI Studio tem uma camada gratuita, com limite de requisições por minuto e por dia.
  Nela, o Google pode usar o que você envia para melhorar os produtos dele; com o
  faturamento ativado no projeto, não.
- No `/mapa`, o modelo barato do Gemini é o `gemini-2.5-flash`.

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
3. **Área de trabalho.** São três painéis, como numa IDE: o Navegador à esquerda e, à
   direita, o Visualizador largo em cima e o chat embaixo.
   - **[1] Navegador:** no topo, quando a branch atual tem commits que a principal não tem,
     a linha **◆ branch × main**: a branch inteira como um PR (os commits dela desde que saiu
     da principal), para revisar tudo de uma vez. Depois, o histórico com o grafo, duas linhas por commit (assunto; autor,
     data e tamanho da mudança) e "✓ revisado" nos que você já conversou. As mudanças não
     commitadas aparecem no topo. Os arquivos do commit selecionado aparecem logo abaixo
     dele, em árvore por pasta (`~` modificado, `+` novo, `−` removido, `→` renomeado).
   - **[2] Visualizador:** uma coisa por vez, escolhida pelas abas do título. **Diff** é o
     arquivo selecionado, com numeração antiga e nova, fundo colorido e as palavras que
     mudaram destacadas. **Documento** é o arquivo que o modelo abriu, e **Mapa**, o mapa
     do projeto.
   - **[3] Chat:** a conversa com o modelo, com a largura toda. Cada mensagem leva junto o
     contexto, ou seja, o commit e o arquivo em foco.

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
| `z` | minimiza o painel para a barra de baixo (`▾ [2] Visualizador`) |
| `1` `2` `3` | vai para o painel e o restaura se estiver minimizado |
| `0` | volta ao layout padrão |
| `m` / `o` / `u` / `q` | modelo / outro repositório / atualizar / sair |
| `p` | permissões do agente (ler ou editar) |

Com o mouse: clique dá foco, a roda rola o painel embaixo dela e arrastar a borda entre dois
painéis redimensiona. Para selecionar texto com o mouse capturado, segure `Shift` (na maioria
dos terminais), ou abra com `dante --sem-mouse`.

Teclas de cada painel (fora do chat): `Tab` passa para o próximo painel e `q` sai.

| Painel | Teclas |
|---|---|
| Navegador | `↑↓` andam pelos commits e, com o commit aberto, pelos arquivos dele (o diff acompanha) · `→` abre os arquivos do commit, `←` volta ao commit e fecha · `J`/`K` próximo e anterior arquivo · `enter` num arquivo foca o diff · `r` revisa o commit ou o arquivo · `o` abre no `$EDITOR` · `d` volta ao diff · `esc` troca de repositório |
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
| `/rapido` | até cinco linhas: o essencial e no máximo três tópicos | "seja pontual", "resumo", "direto ao ponto" |
| `/normal` | o equilíbrio de sempre | "em detalhes", "aprofunde" |

Os comandos vão no começo da mensagem e podem ser combinados (`/explicar /aprendiz como
funciona o chat?`). Sozinhos, só trocam o modo. Sem comando, o dante lê o pedido e avisa
quando muda o modo por conta própria. A detecção nunca mexe em permissões.

**Como as respostas aparecem:**
- **Explicação:** começa por uma frase com o efeito da mudança na prática e segue em rótulos
  curtos: **Antes → depois**, **Por quê**, **Como funciona** (o caminho principal, com
  `arquivo:linha`) e **Impacto**. O modelo agrupa por ideia, não arquivo por arquivo. Num
  merge, ele recebe também as mensagens dos commits que o merge trouxe, onde está a intenção.
- **Revisão:** a primeira linha diz o que a mudança faz e o veredito; depois vem o
  **Impacto** (quem é afetado, o que pode quebrar, se há testes). O dante escolhe, pelo que o
  diff toca, as listas de verificação que valem (injeção, HTTP e permissões, concorrência,
  arquivos, segredos, banco de dados, desempenho, além de erros, código limpo e testes). Em
  seguida, um cartão por problema, com a gravidade em cor, o local, a
  explicação e a sugestão. O modelo registra cada achado por uma ferramenta própria. Alguns
  modelos pequenos escrevem a chamada como texto (`registrar_achado(arquivo="…", …)`) em vez
  de usar a ferramenta: o dante reconhece e transforma em cartões do mesmo jeito.
- **Pedir a correção ao Claude:** `/corrigir` copia um pedido pronto com todos os achados
  (arquivo, linha, problema e sugestão) para colar no Claude Code ou em outro agente. Na lista
  de referências (`ctrl+g`), `c` copia só o achado selecionado. O pedido também fica em
  `~/.local/share/dante/pedidos/ultimo.md`.
- **Referências:** cada `arquivo:linha` citado ganha um número `[n]`. `ctrl+g` lista as
  referências, e o número, ou `enter`, mostra o trecho no Visualizador, com a linha marcada
  por `▶`: no diff, se o arquivo mudou no commit, ou o arquivo inteiro na aba Documento.
- **Leituras do modelo:** ficam agrupadas numa linha ("✓ 3 leituras: …"); `ctrl+o` mostra
  uma por linha.

### Verificações do projeto (linters, sem tokens)

O dante não tem linter próprio: ele **usa as ferramentas que o projeto já tem** e mostra só
os problemas que a mudança introduziu (os antigos ficam de fora), como cartões, sem gastar
tokens.

- **`/verificar`** roda as ferramentas nos arquivos alterados (mudanças locais, ou a branch
  atual × main). Ferramentas detectadas: **Qlty** (com `.qlty/qlty.toml`, ele basta: roda os
  linters do projeto), **Biome**, **Oxlint** ou **ESLint** (incluindo a configuração do
  Ultracite), **tsc**, **ruff**, **golangci-lint** ou **go vet** e **clippy**. As de
  JavaScript precisam estar em `node_modules/.bin`: o dante nunca baixa nada.
- **Na revisão (`r`),** com as verificações permitidas, elas rodam antes, e os problemas vão
  ao modelo como fatos ("não repita; avalie o impacto"). A revisão fica mais precisa e foca
  no que só a IA consegue ver.
- **Permissão por repositório:** rodar essas ferramentas executa código do projeto
  (configurações, plugins). Na primeira vez, o dante mostra os comandos e pergunta: `s` sempre
  neste repositório, `u` só desta vez ou `n` não. A lista fica em `verificacoes_permitidas`
  no `config.json`.

### Chegando num projeto novo

- **Explorador (`p` no Navegador):** troca os commits pela árvore do projeto inteiro. `→`
  abre uma pasta, e ao andar pelos arquivos cada um aparece na aba **Arquivo**. `x` explica o
  arquivo: para que serve, as peças principais com a linha de cada uma, quem usa e por onde
  ler. `p` volta aos commits.
- **Aba Arquivo (`v` no diff ou no Navegador):** o arquivo inteiro na versão do alvo, com as
  linhas alteradas marcadas na margem (`▎`). `n`/`N` andam entre as mudanças, `/` busca (e aí
  `n`/`N` andam entre os resultados), `:` vai para uma linha, e clique + `f` explica a função.
- **`/onde <tarefa>`:** "onde mexo para fazer isto?". O modelo sugere termos de busca (uma
  pergunta pequena, porque a tarefa vem em português e o código costuma estar em inglês), o
  dante procura no código sem gastar tokens e manda os trechos mais relevantes. A resposta diz
  onde mexer, na ordem de leitura, o que já existe parecido e os cuidados, e traz um
  **briefing pronto para colar no Claude**, que já vai para a área de transferência e para
  `~/.local/share/dante/pedidos/briefing.md`.
- **`/como <funcionalidade>`:** como algo funciona de ponta a ponta, com o caminho passo a
  passo pelo código, usando a mesma busca.

Com o mapa do projeto (`/mapa`) gerado, o resumo dele também vai no `/onde` e no `/como`.

### Commits e descrição do PR

- **`/commits`** (ou `c` sobre "Mudanças locais" no Navegador): o dante junta, sem o modelo,
  os arquivos alterados, o diff e os assuntos dos commits recentes (para seguir o estilo do
  repositório), e pergunta como dividir tudo em commits, com mensagens no padrão Conventional
  Commits e o porquê no corpo. O plano aparece numa janela: `enter` commita o selecionado, `a`
  commita todos em ordem, `e` ajusta a mensagem no editor e `d` tira um commit do plano.
  **O git só roda quando você aperta a tecla**, com o autor do seu `git config`, e só leva os
  arquivos de cada commit (o que mais estiver no índice fica de fora). Nesta versão, a divisão
  é por arquivo.
- **`/pr`** (ou `c` sobre a linha **◆ branch × main**): a descrição do PR da branch, com
  título, o que muda, por quê, como testar e riscos, a partir dos commits e do resumo dos
  arquivos. Vai para a área de transferência e para `~/.local/share/dante/pedidos/pr.md`.

As duas são perguntas avulsas e sem ferramentas: costumam ficar entre 1 e 5 mil tokens.

### O que faz esta função?

Para entender uma função sem gastar uma conversa inteira:

- **No Visualizador:** clique numa linha da função (ela ganha a marca ▶) e aperte `f`. Sem
  clique, vale o trecho atual do diff, ou a primeira linha visível na aba Documento.
- **No chat:** `/funcao NomeDaFuncao`, ou `/funcao` seguido do código colado. Uma mensagem com
  uma função colada e "o que faz?" também vai por esse caminho.

O dante acha a função e monta, sem gastar tokens, o pacote: o corpo (com o comentário que a
documenta), **onde ela é usada** (até 8 chamadas, com a linha de código de cada uma) e as
funções do projeto que ela chama. A pergunta vai numa conversa à parte, sem ferramentas e
sem o histórico do chat: costuma ficar entre 1 e 3 mil tokens. A resposta diz o que a função
faz, como funciona, onde é usada e os cuidados, com referências `[n]` para abrir as linhas.
A conversa principal continua como estava.

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
- Uma versão compacta do mapa (até ~1,5 mil tokens) vai como contexto no começo de cada
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
provedor cobra pelo reenvio. Por isso o objetivo é responder **sem ferramentas ou com uma
rodada só**:

1. **O pacote de revisão vai na sua mensagem.** Na primeira pergunta sobre um commit, o diff
   do código vai junto, com 3 linhas de contexto (o cabeçalho de cada trecho diz a função).
   Testes, documentação, arquivos gerados e os que mudaram só em espaços vão só como lista,
   e o modelo pede o diff deles se precisar. Em Python e YAML, onde a indentação muda o
   comportamento, espaços continuam contando como mudança. Se o código não couber (60 KB ou
   1/4 do contexto do modelo), entram primeiro os arquivos com mais mudanças, e o resto vai
   para a lista, com as funções alteradas em cada um (lidas do diff, sem custo). O tamanho
   acompanha o modo: ~6 KB no rápido, ~12 KB ao explicar e o limite cheio ao revisar. O
   pacote também diz **onde as funções alteradas são usadas** (só
   `arquivo:linha`, achado com `git grep`, sem gastar tokens), para o modelo ir direto ao
   trecho certo em vez de gastar uma rodada procurando.
2. **Leituras recortadas.** As instruções pedem para abrir código só quando uma conclusão
   depender de algo fora do diff, com todos os trechos numa chamada de `read_files`. Um
   arquivo com mais de 250 linhas, lido sem trecho, volta como **esqueleto**: as declarações
   com o número da linha, para o modelo pedir só o que interessa. O `grep` devolve no máximo
   40 resultados.
3. **Limite de rodadas de leitura por pergunta:** 2 ao revisar, 3 ao explicar, 1 no modo
   rápido (sem limite no modo editar). Passado o limite, o modelo é avisado para concluir com
   o que já leu.
4. **Os achados encerram a resposta.** Uma rodada só com `registrar_achado` não chama o modelo
   de novo, o que poupa uma rodada inteira.
5. **Orçamento por pergunta:** 100 mil tokens por padrão, conferido **antes** de cada rodada
   (o gasto até ali mais o tamanho da próxima requisição). Ajuste em `orcamento_tokens` no
   `config.json` (`-1` desliga).
6. **Nada é enviado duas vezes.** Se o modelo pedir com `git_diff` um diff que já veio no
   pacote, recebe um aviso curto em vez do diff repetido (com mais linhas de contexto, o diff
   é gerado normalmente).
7. **Conversas longas custam mais:** cada pergunta reenvia a conversa inteira. Quando cada
   requisição passa de ~20 mil tokens, o chat sugere `ctrl+n` para um assunto novo.
8. **Cada resposta mostra o gasto:** `↳ 23,6 mil tokens nesta resposta · 2 rodadas (entrada
   23,2 mil, 18 mil do cache · saída 400)`. A parte do cache, informada pelo OpenRouter,
   OpenAI, DeepSeek, Qwen e Anthropic, costuma custar bem menos.

Medido na revisão de um commit de 13 arquivos (+291 −195): o fluxo antigo levava 5 rodadas e
~70 mil tokens (um modelo real chegou a 220 mil); o novo, 2 rodadas e ~24 mil.

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
