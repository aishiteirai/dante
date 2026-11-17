"""Execução do agente revisor sobre o Claude Agent SDK."""

from __future__ import annotations

import re
import shlex
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable

from claude_agent_sdk import (
    AssistantMessage,
    ClaudeAgentOptions,
    HookMatcher,
    ResultMessage,
    TextBlock,
    ToolUseBlock,
    query,
)

from .git_utils import ReviewTarget
from .prompts import SYSTEM_PROMPT, build_prompt
from .schema import FINDINGS_SCHEMA

# Subcomandos git que o agente pode rodar (todos somente leitura).
GIT_READONLY = ("diff", "show", "log", "blame", "status", "ls-files", "grep", "rev-parse")
# Qualquer metacaractere de shell faz o comando ser recusado.
SHELL_META = re.compile(r"[;&|`<>$\n]|\$\(")


@dataclass
class ReviewConfig:
    model: str = "claude-opus-5-5"
    effort: str = "high"
    max_turns: int = 60
    max_budget_usd: float | None = None
    max_inline_diff_chars: int = 60_000
    verbose: bool = False


@dataclass
class ReviewResult:
    target: ReviewTarget
    resumo: str
    achados: list[dict[str, Any]]
    cost_usd: float | None = None
    num_turns: int = 0
    session_id: str | None = None
    error: str | None = None
    meta: dict[str, Any] = field(default_factory=dict)


def is_safe_git_command(command: str) -> bool:
    if SHELL_META.search(command):
        return False
    try:
        argv = shlex.split(command)
    except ValueError:
        return False
    if len(argv) < 2 or argv[0] != "git":
        return False
    # Aceita `git --no-pager <sub>`; outras opções globais (ex.: `-C dir`, `-c k=v`) são recusadas.
    sub = argv[1]
    if sub == "--no-pager" and len(argv) > 2:
        sub = argv[2]
    if sub not in GIT_READONLY:
        return False
    # Opções que escrevem em disco ou executam programas externos.
    banned = ("--output", "--ext-diff", "--textconv", "-o")
    return not any(a == b or a.startswith(b + "=") for a in argv for b in banned)


async def _guard_bash(input_data: dict, tool_use_id: str | None, context: Any) -> dict:
    """Hook PreToolUse: só deixa passar comandos git somente leitura."""
    command = (input_data.get("tool_input") or {}).get("command", "")
    if is_safe_git_command(command):
        return {}
    return {
        "hookSpecificOutput": {
            "hookEventName": "PreToolUse",
            "permissionDecision": "deny",
            "permissionDecisionReason": (
                "O revisor só pode rodar git somente leitura "
                f"({', '.join(GIT_READONLY)}) sem pipes ou redirecionamentos."
            ),
        }
    }


def build_options(root: Path, cfg: ReviewConfig) -> ClaudeAgentOptions:
    return ClaudeAgentOptions(
        cwd=str(root),
        model=cfg.model,
        effort=cfg.effort,
        thinking={"type": "adaptive"},
        system_prompt=SYSTEM_PROMPT,
        # Só ferramentas de leitura existem para o agente.
        tools=["Read", "Grep", "Glob", "Bash"],
        allowed_tools=["Read", "Grep", "Glob", "Bash"],
        disallowed_tools=["Write", "Edit", "NotebookEdit", "WebFetch", "WebSearch"],
        # dontAsk: nada fica esperando aprovação; o hook abaixo filtra o Bash.
        permission_mode="dontAsk",
        hooks={"PreToolUse": [HookMatcher(matcher="Bash", hooks=[_guard_bash])]},
        # Não carrega settings/CLAUDE.md do usuário: revisões reproduzíveis.
        setting_sources=[],
        output_format={"type": "json_schema", "schema": FINDINGS_SCHEMA},
        max_turns=cfg.max_turns,
        max_budget_usd=cfg.max_budget_usd,
    )


EventCallback = Callable[[dict[str, Any]], None]


def _log(cfg: ReviewConfig, msg: str) -> None:
    if cfg.verbose:
        print(msg, file=sys.stderr, flush=True)


async def run_review(
    target: ReviewTarget,
    root: Path,
    cfg: ReviewConfig,
    guidelines: str | None = None,
    extra_instructions: str | None = None,
    on_event: EventCallback | None = None,
) -> ReviewResult:
    prompt = build_prompt(target, guidelines, cfg.max_inline_diff_chars, extra_instructions)
    final: ResultMessage | None = None

    async for msg in query(prompt=prompt, options=build_options(root, cfg)):
        if isinstance(msg, AssistantMessage):
            for block in msg.content:
                if isinstance(block, ToolUseBlock):
                    arg = block.input.get("command") or block.input.get("file_path") or block.input.get("pattern") or ""
                    _log(cfg, f"  · {block.name} {arg}")
                    if on_event and block.name != "StructuredOutput":
                        on_event({"tipo": "ferramenta", "nome": block.name, "arg": str(arg)})
                elif isinstance(block, TextBlock) and block.text.strip():
                    _log(cfg, f"  » {block.text.strip()[:200]}")
                    if on_event:
                        on_event({"tipo": "texto", "texto": block.text.strip()})
        elif isinstance(msg, ResultMessage):
            final = msg

    if final is None:
        return ReviewResult(target, "", [], error="O agente terminou sem resultado.")

    common = dict(
        cost_usd=final.total_cost_usd,
        num_turns=final.num_turns,
        session_id=final.session_id,
        meta={"model": cfg.model, "effort": cfg.effort, "terminal_reason": final.terminal_reason},
    )
    data = final.structured_output
    if final.is_error or not isinstance(data, dict):
        detail = "; ".join(final.errors or []) or final.subtype or "saída estruturada ausente"
        return ReviewResult(target, final.result or "", [], error=detail, **common)

    return ReviewResult(target, data.get("resumo", ""), data.get("achados", []), **common)
