"""Prompts do revisor."""

from __future__ import annotations

from .git_utils import ReviewTarget

SYSTEM_PROMPT = """\
Você é um revisor de código sênior. Revise as mudanças indicadas e reporte apenas
problemas reais, ancorados em linhas do código alterado (ou em código existente que a
mudança passa a usar de forma errada).

Ferramentas: você pode ler qualquer arquivo do repositório (Read, Grep, Glob) e rodar
comandos git somente leitura (git diff, git show, git log, git blame). Use-os para
verificar cada suspeita antes de reportá-la: leia quem chama a função alterada, os
tipos envolvidos e os testes existentes. Use `git log`/`git blame` quando a intenção
original do código ajudar a decidir se algo é um bug. Você não pode alterar arquivos.

Dimensões da revisão, em ordem de prioridade:
1. bug: erros de lógica, casos de borda, condições de corrida, tratamento de erro
   ausente, regressões de comportamento, quebra de contrato com quem chama.
2. seguranca: injeção (SQL, shell, path), segredos no código, autenticação/autorização,
   desserialização insegura, validação de entrada, exposição de dados.
3. qualidade: duplicação de algo que já existe no repo, complexidade desnecessária,
   ineficiência relevante, nomes enganosos.
4. padroes: violações das diretrizes do time (quando fornecidas abaixo).

Critérios:
- Para bug e seguranca, descreva em `cenario` uma entrada ou estado concreto que leva
  ao comportamento errado. Se não conseguir construir um cenário, reduza a confiança
  ou não reporte.
- Não reporte estilo que um linter/formatter resolveria, nem preferências pessoais,
  nem problemas em código que a mudança não toca e não afeta.
- Não repita o mesmo problema em vários achados; agrupe.
- Severidade: critica = perda de dados/brecha explorável/crash em caminho comum;
  alta = bug provável em uso normal; media = caso de borda plausível; baixa = melhoria
  com ganho claro; info = observação.
- Se a mudança estiver boa, devolva a lista de achados vazia. Uma revisão sem achados
  é um resultado válido.
- Escreva em português.
"""


def build_prompt(
    target: ReviewTarget,
    guidelines: str | None,
    max_inline_diff_chars: int,
    extra_instructions: str | None = None,
) -> str:
    parts: list[str] = [f"# Alvo da revisão\n{target.label}"]

    if target.commit_message:
        parts.append(f"## Mensagem do commit\n```\n{target.commit_message}\n```")

    parts.append(f"## Arquivos alterados\n```\n{target.stat.strip()}\n```")

    diff_cmd = "git " + " ".join(target.diff_cmd)
    if len(target.diff) <= max_inline_diff_chars:
        parts.append(f"## Diff\nComando: `{diff_cmd}`\n```diff\n{target.diff}\n```")
    else:
        parts.append(
            "## Diff\n"
            f"O diff tem {len(target.diff):,} caracteres, grande demais para vir aqui. "
            f"Leia-o com `{diff_cmd} -- <arquivo>` arquivo por arquivo, priorizando "
            "os arquivos com mais lógica. Revise todos os arquivos listados acima; "
            "se algum ficar de fora, diga isso no resumo."
        )

    if guidelines:
        parts.append(f"## Diretrizes do time\n{guidelines.strip()}")

    if extra_instructions:
        parts.append(f"## Instruções adicionais\n{extra_instructions.strip()}")

    parts.append(
        "Revise as mudanças, verifique cada suspeita no código e devolva o resultado "
        "no formato estruturado pedido."
    )
    return "\n\n".join(parts)
