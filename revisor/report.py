"""Formatação dos resultados (markdown para terminal/arquivo e JSON)."""

from __future__ import annotations

import json
from collections import Counter
from typing import Any

from .agent import ReviewResult
from .schema import SEVERITIES

ICON = {"critica": "🔴", "alta": "🟠", "media": "🟡", "baixa": "🔵", "info": "⚪"}


def sort_findings(achados: list[dict[str, Any]]) -> list[dict[str, Any]]:
    order = {s: i for i, s in enumerate(SEVERITIES)}
    return sorted(achados, key=lambda a: (order.get(a.get("severidade"), 99), a.get("arquivo", ""), a.get("linha", 0)))


def worst_severity(results: list[ReviewResult]) -> str | None:
    found = {a.get("severidade") for r in results for a in r.achados}
    return next((s for s in SEVERITIES if s in found), None)


def to_markdown(result: ReviewResult) -> str:
    out = [f"## Revisão — {result.target.label}"]
    if result.error:
        out.append(f"> ⚠️ A revisão não terminou: {result.error}")
    if result.resumo:
        out.append(result.resumo)

    achados = sort_findings(result.achados)
    if not achados and not result.error:
        out.append("✅ Nenhum problema encontrado.")
    elif achados:
        cont = Counter(a["severidade"] for a in achados)
        out.append("**Achados:** " + ", ".join(f"{ICON[s]} {cont[s]} {s}" for s in SEVERITIES if cont[s]))
        for i, a in enumerate(achados, 1):
            out.append(
                f"### {i}. {ICON.get(a['severidade'], '')} [{a['severidade']}/{a['categoria']}] {a['titulo']}\n"
                f"`{a['arquivo']}:{a['linha']}` · confiança {a['confianca']}\n\n"
                f"{a['descricao']}"
            )
            if a.get("cenario"):
                out.append(f"**Cenário:** {a['cenario']}")
            if a.get("sugestao"):
                out.append(f"**Sugestão:** {a['sugestao']}")

    rodape = [f"{result.num_turns} turnos"]
    if result.cost_usd is not None:
        rodape.append(f"US$ {result.cost_usd:.4f}")
    out.append(f"<sub>{' · '.join(rodape)}</sub>")
    return "\n\n".join(out)


def to_json(results: list[ReviewResult]) -> str:
    return json.dumps(
        [
            {
                "alvo": r.target.label,
                "commit": r.target.commit_sha,
                "resumo": r.resumo,
                "achados": sort_findings(r.achados),
                "erro": r.error,
                "custo_usd": r.cost_usd,
            }
            for r in results
        ],
        ensure_ascii=False,
        indent=2,
    )
