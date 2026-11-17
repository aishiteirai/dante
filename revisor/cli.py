"""CLI: `revisor revisar ...` e `revisor historico ...`."""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

from . import history
from .agent import ReviewConfig, ReviewResult, run_review
from .git_utils import (
    GitError,
    ReviewTarget,
    branch_target,
    commit_target,
    commits_in_range,
    last_commits,
    repo_root,
    staged_target,
    working_target,
)
from .report import ICON, sort_findings, to_json, to_markdown, worst_severity
from .schema import SEVERITIES

GUIDELINE_FILES = [".revisor/diretrizes.md", "REVIEW.md", ".github/REVIEW.md"]


def find_guidelines(root: Path, explicit: str | None) -> str | None:
    if explicit:
        return Path(explicit).read_text(encoding="utf-8")
    for name in GUIDELINE_FILES:
        p = root / name
        if p.is_file():
            return p.read_text(encoding="utf-8")
    return None


def collect_targets(args: argparse.Namespace, root: Path) -> list[ReviewTarget]:
    if args.commit:
        shas = args.commit
    elif args.intervalo:
        shas = commits_in_range(args.intervalo, root)
    elif args.ultimos:
        shas = last_commits(args.ultimos, root)
    else:
        shas = []

    if shas:
        if args.pular_revisados:
            done = history.reviewed_commits(root)
            pulados = [s for s in shas if s in done]
            shas = [s for s in shas if s not in done]
            if pulados:
                print(f"Pulando {len(pulados)} commit(s) já revisado(s).", file=sys.stderr)
        return [commit_target(s, root) for s in shas]

    if args.base:
        return [branch_target(args.base, root)]
    if args.staged:
        return [staged_target()]
    return [working_target()]


async def cmd_revisar(args: argparse.Namespace) -> int:
    root = repo_root(Path(args.repo).resolve())
    cfg = ReviewConfig(
        model=args.modelo,
        effort=args.esforco,
        max_turns=args.max_turnos,
        max_budget_usd=args.orcamento,
        verbose=args.verbose,
    )
    guidelines = find_guidelines(root, args.diretrizes)
    targets = [t.load(root) for t in collect_targets(args, root)]
    targets = [t for t in targets if not t.empty]
    emit = (lambda ev: print(json.dumps(ev, ensure_ascii=False), flush=True)) if args.eventos else None
    if emit:
        emit({"tipo": "alvos", "repo": str(root), "alvos": [
            {"descricao": t.label, "commit": t.commit_sha, "arquivos": t.files} for t in targets
        ]})
    if not targets:
        if emit:
            emit({"tipo": "fim", "codigo": 0})
        else:
            print("Nada para revisar.", file=sys.stderr)
        return 0

    results: list[ReviewResult] = []
    for i, t in enumerate(targets, 1):
        if emit:
            emit({"tipo": "inicio", "indice": i, "total": len(targets), "descricao": t.label})
        else:
            print(f"[{i}/{len(targets)}] Revisando {t.label} ({len(t.files)} arquivo(s))...", file=sys.stderr)
        r = await run_review(t, root, cfg, guidelines, args.instrucoes, on_event=emit)
        results.append(r)
        if args.sem_historico:
            record = history.record_from(r, datetime.now(timezone.utc))
        else:
            path, record = history.save(r, root)
            if not emit:
                print(f"  salvo em {path}", file=sys.stderr)
        if emit:
            record["achados"] = sort_findings(record["achados"])
            emit({"tipo": "resultado", "indice": i, "revisao": record})
        elif not args.json:
            print(to_markdown(r) + "\n")

    code = exit_code(results, args.falhar_em)
    if emit:
        emit({"tipo": "fim", "codigo": code})
        return code
    if args.json:
        print(to_json(results))
    if args.saida:
        Path(args.saida).write_text("\n\n---\n\n".join(to_markdown(r) for r in results), encoding="utf-8")

    return code


def exit_code(results: list[ReviewResult], falhar_em: str | None) -> int:
    if any(r.error for r in results):
        return 2
    worst = worst_severity(results)
    if falhar_em and worst and SEVERITIES.index(worst) <= SEVERITIES.index(falhar_em):
        return 1
    return 0


def cmd_historico(args: argparse.Namespace) -> int:
    root = repo_root(Path(args.repo).resolve())
    if args.caminho:
        print(history.history_dir(root))
        return 0
    records = history.load_all(root)
    if args.commit:
        records = [r for r in records if (r["alvo"].get("commit") or "").startswith(args.commit)]
    records = records[-args.limite:]
    if args.json:
        print(json.dumps(records, ensure_ascii=False))
        return 0
    if not records:
        print("Nenhuma revisão no histórico.")
        return 0
    for r in records:
        cont = Counter(a["severidade"] for a in r["achados"])
        sev = " ".join(f"{ICON[s]}{cont[s]}" for s in SEVERITIES if cont[s]) or "✅"
        if r.get("erro"):
            sev = "⚠️ erro"
        custo = f"US$ {r['custo_usd']:.3f}" if r.get("custo_usd") is not None else ""
        print(f"{r['data'][:19]}  {sev:<14} {r['alvo']['descricao'][:70]:<70} {custo}  {Path(r['_arquivo']).name}")
    return 0


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="revisor", description="Revisor de código com Claude Agent SDK.")
    p.add_argument("--repo", default=".", help="Caminho do repositório (padrão: diretório atual).")
    sub = p.add_subparsers(dest="cmd", required=True)

    r = sub.add_parser("revisar", help="Revisa mudanças locais, uma branch ou commits do histórico.")
    alvo = r.add_mutually_exclusive_group()
    alvo.add_argument("--staged", action="store_true", help="Só o que está em stage.")
    alvo.add_argument("--base", metavar="REF", help="Branch atual vs REF (ex.: main).")
    alvo.add_argument("--commit", metavar="SHA", action="append", help="Um commit (pode repetir).")
    alvo.add_argument("--intervalo", metavar="A..B", help="Cada commit do intervalo, um por vez.")
    alvo.add_argument("--ultimos", type=int, metavar="N", help="Os últimos N commits, um por vez.")
    r.add_argument("--pular-revisados", action="store_true", help="Pula commits que já estão no histórico.")
    r.add_argument("--diretrizes", metavar="ARQ", help=f"Arquivo de diretrizes (padrão: {', '.join(GUIDELINE_FILES)}).")
    r.add_argument("--instrucoes", help="Instruções extras para esta revisão.")
    r.add_argument("--modelo", default="claude-opus-5-5")
    r.add_argument("--esforco", default="high", choices=["low", "medium", "high", "xhigh", "max"])
    r.add_argument("--max-turnos", type=int, default=60)
    r.add_argument("--orcamento", type=float, metavar="USD", help="Teto de custo por revisão.")
    r.add_argument("--falhar-em", choices=SEVERITIES, help="Sai com código 1 se houver achado com essa severidade ou pior.")
    r.add_argument("--json", action="store_true", help="Imprime JSON em vez de markdown.")
    r.add_argument("--saida", metavar="ARQ", help="Também grava o relatório markdown nesse arquivo.")
    r.add_argument("--sem-historico", action="store_true", help="Não grava no histórico.")
    r.add_argument("--eventos", action="store_true", help="Emite eventos JSONL no stdout (usado pela interface Go).")
    r.add_argument("-v", "--verbose", action="store_true", help="Mostra as ferramentas que o agente usa.")

    h = sub.add_parser("historico", help="Lista revisões anteriores.")
    h.add_argument("--commit", metavar="SHA", help="Filtra por commit (prefixo).")
    h.add_argument("--limite", type=int, default=30)
    h.add_argument("--json", action="store_true", help="Registros completos em JSON.")
    h.add_argument("--caminho", action="store_true", help="Mostra onde o histórico deste repo fica.")

    args = p.parse_args(argv)
    try:
        if args.cmd == "revisar":
            return asyncio.run(cmd_revisar(args))
        return cmd_historico(args)
    except GitError as e:
        print(f"Erro: {e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
