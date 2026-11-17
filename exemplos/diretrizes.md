# Diretrizes de revisão do time

Copie para `.revisor/diretrizes.md` (ou `REVIEW.md`) no seu repositório e ajuste.

- Toda consulta SQL usa parâmetros; nunca concatenação de strings.
- Funções públicas novas têm type hints e docstring.
- Erros de I/O e de rede são tratados explicitamente; nada de `except Exception: pass`.
- Logs nunca incluem tokens, senhas, CPF ou e-mail.
- Mudança de comportamento vem acompanhada de teste.
- Datas sempre com timezone (UTC no banco).
