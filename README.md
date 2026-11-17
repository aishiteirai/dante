# dante

Revisor de código para o terminal, construído sobre o **Claude Agent SDK**.

- **`revisor-tui`** (Go, Bubble Tea): interface interativa. Você escolhe o que revisar,
  acompanha o agente trabalhando e navega pelos achados.
- **`revisor`** (Python): o harness. O agente lê o diff e explora o repositório (abre
  arquivos, faz buscas, roda `git log`/`git blame`) para confirmar cada suspeita. Os achados
  saem em JSON estruturado. Também funciona sozinho, em scripts e hooks.

## Instalação

Requisitos: Python ≥ 3.10 com o módulo `venv`, Go ≥ 1.24, git e credenciais do Claude
Code (`ANTHROPIC_API_KEY` ou login do `claude`).

```bash
git clone https://github.com/aishiteirai/dante.git && cd dante
make install
```

O `make install` cria um ambiente virtual em `.venv/`, sem mexer no Python do sistema.
Em seguida, coloca `revisor` e `revisor-tui` em `~/.local/bin`. Esse diretório precisa
estar no `PATH`; o comando avisa se não estiver. O `revisor-tui` já sai configurado para
usar o Python do `.venv`, então não mova nem apague a pasta do projeto depois de instalar.

Se der erro ao criar o `.venv`:
- Debian/Ubuntu: `sudo apt install python3-venv`.
- Sem root, com o [uv](https://docs.astral.sh/uv/): `uv venv --seed .venv && make install`.

Outros alvos: `make test` roda os testes, e `make clean` remove o `.venv` e o binário.

## Interface de terminal

```bash
cd meu-projeto
revisor-tui
```

| Tela | Teclas |
|---|---|
| Menu | `1`–`5` ou `↑↓`+`enter` · `e` muda o esforço · `q` sai |
| Commits do histórico | `espaço` marca · `a` marca os não revisados até o cursor · `c` limpa · `v` abre a revisão anterior do commit · `enter` revisa |
| Revisão em andamento | progresso ao vivo (ferramentas usadas, custo) · `esc` cancela |
| Resultados | `↑↓` achado · `tab`/`←→` próxima revisão · `f` filtra por severidade · `o`/`enter` abre o arquivo na linha (`$VISUAL`/`$EDITOR`) · `pgup/pgdn` rola o detalhe |
| Histórico | `enter` abre · `r` revisa o commit de novo |

Na lista de commits, os já revisados aparecem com a contagem de achados (●).

Opções: `--repo`, `--esforco`, `--orcamento`, `--modelo`, `--diretrizes`, `--python`.
Variáveis de ambiente: `REVISOR_PYTHON`, `REVISOR_ESFORCO`, `REVISOR_ORCAMENTO`,
`REVISOR_MODELO`.

## CLI (sem interface)

```bash
revisor revisar                          # mudanças não commitadas
revisor revisar --staged                 # só o que está em stage
revisor revisar --base main              # branch atual vs main
revisor revisar --commit a1b2c3d         # um commit (repita --commit para vários)
revisor revisar --ultimos 5              # últimos 5 commits, um por vez
revisor revisar --intervalo v1.2..HEAD --pular-revisados
revisor historico                        # revisões anteriores
revisor historico --caminho              # onde o histórico deste repo fica
```

| Opção | Efeito |
|---|---|
| `--esforco low…max` | Profundidade da análise (padrão `high`) |
| `--orcamento 2.0` | Teto de custo em US$ por revisão |
| `--falhar-em alta` | Sai com código 1 se houver achado `alta` ou pior |
| `--json` / `--saida rel.md` | Saída em JSON / relatório markdown em arquivo |
| `--eventos` | Eventos JSONL no stdout (protocolo usado pela interface Go) |
| `--diretrizes ARQ` | Diretrizes do time (padrão: `.revisor/diretrizes.md`, `REVIEW.md`) |

Códigos de saída: `0` ok · `1` achado na severidade de `--falhar-em` · `2` erro.

Hook de pre-push:

```bash
# .git/hooks/pre-push
revisor revisar --base origin/main --falhar-em alta --esforco medium
```

## Histórico

O histórico fica **fora do repositório**:

```
~/.local/share/revisor/historico/<repo>-<sha do primeiro commit>/<data>-<sha>.json
```

O diretório base segue `$REVISOR_HOME` ou `$XDG_DATA_HOME/revisor`. O identificador usa o
primeiro commit do repositório, então clones diferentes do mesmo projeto compartilham o
histórico.

## Arquitetura

```
cmd/revisor-tui/  interface Go: executa `python -m revisor revisar --eventos` e lê o JSONL
revisor/
  git_utils.py  alvos: working tree, stage, branch, commits
  prompts.py    system prompt (bug > segurança > qualidade > padrões) + prompt do alvo
  agent.py      query() do Agent SDK com ferramentas só de leitura e saída via JSON Schema
  schema.py     formato dos achados
  history.py    histórico em ~/.local/share/revisor
  report.py     markdown / JSON
  cli.py        comandos `revisar` e `historico`
```

**Eventos JSONL** (`--eventos`): `alvos` → para cada alvo: `inicio`, `ferramenta`/`texto`
(ao vivo), `resultado` (registro completo) → `fim` com o código de saída.

**Segurança do agente.** O agente roda com as ferramentas `Read`, `Grep`, `Glob` e `Bash`,
e `permission_mode="dontAsk"`. Um hook `PreToolUse` recusa qualquer Bash que não seja git
somente leitura (`diff`, `show`, `log`, `blame`…). Também são recusados pipes, `;`, `$()`,
redirecionamentos e opções como `--output`/`--ext-diff`.

**Diffs grandes.** Até 60 mil caracteres, o diff vai inteiro no prompt. Acima disso, o
agente lê arquivo por arquivo, e o resumo indica se algum ficou de fora.

## Diretrizes do time

Copie `exemplos/diretrizes.md` para `.revisor/diretrizes.md` (ou `REVIEW.md`) no
repositório revisado e ajuste as regras. Os achados que violam essas regras saem na
categoria `padroes`.
