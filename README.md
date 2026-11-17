# revisor-codigo

Harness de revisão de código construído sobre o **Claude Agent SDK** (Python).
O agente lê o diff e, em seguida, explora o repositório: lê os arquivos, faz buscas e roda
`git log`/`git blame`. Com isso ele confirma cada suspeita antes de reportar e devolve os
achados em JSON estruturado.

## Instalação

```bash
pip install -e '.[dev]'
```

Credenciais: as mesmas do Claude Code (`ANTHROPIC_API_KEY` ou login do `claude`).

## Uso

```bash
revisor revisar                         # mudanças não commitadas (working tree vs HEAD)
revisor revisar --staged                # só o que está em stage
revisor revisar --base main             # branch atual vs main
revisor revisar --commit a1b2c3d        # um commit do histórico
revisor revisar --ultimos 5             # os últimos 5 commits, um por vez
revisor revisar --intervalo v1.2..HEAD  # cada commit do intervalo
revisor revisar --ultimos 20 --pular-revisados   # só os que ainda não estão no histórico
revisor historico                       # lista revisões anteriores
revisor historico --commit a1b2c3d      # revisões de um commit
```

Opções úteis:

| Opção | Efeito |
|---|---|
| `--esforco low…max` | Profundidade da análise (padrão `high`) |
| `--orcamento 2.0` | Teto de custo em US$ por revisão |
| `--falhar-em alta` | Sai com código 1 se houver achado `alta` ou pior (bom para hooks e CI) |
| `--json` / `--saida rel.md` | Saída em JSON / grava relatório markdown |
| `--diretrizes ARQ` | Diretrizes do time (padrão: `.revisor/diretrizes.md`, `REVIEW.md`) |
| `--instrucoes "..."` | Instruções extras só para essa execução |
| `-v` | Mostra cada ferramenta que o agente usa |

Códigos de saída: `0` ok · `1` achado na severidade de `--falhar-em` · `2` erro (git ou agente).

## Como funciona

```
git_utils.py  →  monta o alvo (diff, stat, arquivos, mensagem do commit)
prompts.py    →  system prompt (bug > segurança > qualidade > padrões) + prompt do alvo
agent.py      →  query() do Agent SDK, ferramentas só de leitura, saída via JSON Schema
schema.py     →  formato dos achados (arquivo, linha, severidade, categoria, cenário…)
history.py    →  grava cada revisão em .revisor/historico/<data>-<sha>.json
report.py     →  markdown / JSON
cli.py        →  comandos `revisar` e `historico`
```

**Segurança do agente.** O agente roda com `tools=["Read","Grep","Glob","Bash"]` e
`permission_mode="dontAsk"`. Um hook `PreToolUse` recusa qualquer Bash que não seja git
somente leitura (`diff`, `show`, `log`, `blame`…). Também são recusados pipes, `;`, `$()`,
redirecionamentos e opções como `--output`/`--ext-diff`. O agente não consegue alterar
arquivos.

**Diffs grandes.** Até 60 mil caracteres, o diff vai inteiro no prompt. Acima disso, vai só
a lista de arquivos e o comando git. O agente então lê arquivo por arquivo, e o resumo
indica se algum arquivo ficou de fora.

**Histórico.** Você decide se `.revisor/historico/` entra no repositório (fica compartilhado
com o time) ou vai para o `.gitignore`.

## Diretrizes do time

Copie `exemplos/diretrizes.md` para `.revisor/diretrizes.md` no repositório revisado e
ajuste as regras. Os achados que violam essas regras saem na categoria `padroes`.

## Uso como hook de pre-push

```bash
# .git/hooks/pre-push
revisor revisar --base origin/main --falhar-em alta --esforco medium
```

## Testes

```bash
pytest -q
```
