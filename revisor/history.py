"""Histórico de revisões, guardado fora do repositório revisado.

Local: $DANTE_HOME/historico/<repo>-<id>/  (padrão: $XDG_DATA_HOME/dante ou
~/.local/share/dante; $REVISOR_HOME também é aceito). O <id> é o SHA do primeiro commit do repo, que é o mesmo em
qualquer clone; repositórios sem commits usam um hash do caminho.
"""

from __future__ import annotations

import hashlib
import json
import os
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .agent import ReviewResult
from .git_utils import GitError, git


def data_home() -> Path:
    for var in ("DANTE_HOME", "REVISOR_HOME"):
        if os.environ.get(var):
            return Path(os.environ[var]).expanduser()
    xdg = os.environ.get("XDG_DATA_HOME") or str(Path.home() / ".local" / "share")
    return Path(xdg) / "dante"


def repo_id(root: Path) -> str:
    try:
        roots = git("rev-list", "--max-parents=0", "HEAD", cwd=root).split()
    except GitError:
        roots = []
    ident = sorted(roots)[0][:12] if roots else hashlib.sha1(str(root).encode()).hexdigest()[:12]
    return f"{root.name}-{ident}"


def history_dir(root: Path) -> Path:
    return data_home() / "historico" / repo_id(root)


def record_from(result: ReviewResult, now: datetime) -> dict[str, Any]:
    t = result.target
    return {
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


def save(result: ReviewResult, root: Path) -> tuple[Path, dict[str, Any]]:
    d = history_dir(root)
    d.mkdir(parents=True, exist_ok=True)
    (d / "repo.json").write_text(
        json.dumps({"caminho": str(root)}, ensure_ascii=False), encoding="utf-8"
    )
    now = datetime.now(timezone.utc)
    ref = (result.target.commit_sha or "")[:10] or result.target.kind
    path = d / f"{now:%Y%m%dT%H%M%S%f}-{ref}.json"
    record = record_from(result, now)
    path.write_text(json.dumps(record, ensure_ascii=False, indent=2), encoding="utf-8")
    record["_arquivo"] = str(path)
    return path, record


def load_all(root: Path) -> list[dict[str, Any]]:
    d = history_dir(root)
    if not d.exists():
        return []
    records = []
    for p in sorted(d.glob("*.json")):
        if p.name == "repo.json":
            continue
        try:
            rec = json.loads(p.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            continue
        rec["_arquivo"] = str(p)
        records.append(rec)
    return records


def reviewed_commits(root: Path) -> set[str]:
    """SHAs que já têm revisão concluída sem erro (para --pular-revisados)."""
    return {
        r["alvo"]["commit"]
        for r in load_all(root)
        if r["alvo"].get("tipo") == "commit" and r["alvo"].get("commit") and not r.get("erro")
    }
