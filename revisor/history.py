"""Histórico de revisões: cada revisão vira um JSON em .revisor/historico/."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .agent import ReviewResult

HISTORY_DIR = Path(".revisor") / "historico"


def save(result: ReviewResult, root: Path) -> Path:
    d = root / HISTORY_DIR
    d.mkdir(parents=True, exist_ok=True)
    now = datetime.now(timezone.utc)
    t = result.target
    ref = (t.commit_sha or "")[:10] or t.kind
    path = d / f"{now:%Y%m%dT%H%M%S}-{ref}.json"
    record = {
        "data": now.isoformat(),
        "alvo": {
            "tipo": t.kind,
            "descricao": t.label,
            "commit": t.commit_sha,
            "comando_diff": "git " + " ".join(t.diff_cmd),
            "arquivos": t.files,
        },
        "resumo": result.resumo,
        "achados": result.achados,
        "erro": result.error,
        "custo_usd": result.cost_usd,
        "turnos": result.num_turns,
        "sessao": result.session_id,
        "meta": result.meta,
    }
    path.write_text(json.dumps(record, ensure_ascii=False, indent=2), encoding="utf-8")
    return path


def load_all(root: Path) -> list[dict[str, Any]]:
    d = root / HISTORY_DIR
    if not d.exists():
        return []
    records = []
    for p in sorted(d.glob("*.json")):
        try:
            rec = json.loads(p.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            continue
        rec["_arquivo"] = str(p.relative_to(root))
        records.append(rec)
    return records


def reviewed_commits(root: Path) -> set[str]:
    """SHAs que já têm revisão concluída sem erro (para pular em --pular-revisados)."""
    return {
        r["alvo"]["commit"]
        for r in load_all(root)
        if r["alvo"].get("tipo") == "commit" and r["alvo"].get("commit") and not r.get("erro")
    }

