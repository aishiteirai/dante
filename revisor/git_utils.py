"""Coleta de alvos de revisão a partir do git (diff local, branch, commits do histórico)."""

from __future__ import annotations

import subprocess
from dataclasses import dataclass, field
from pathlib import Path


class GitError(RuntimeError):
    pass


def git(*args: str, cwd: Path) -> str:
    proc = subprocess.run(
        ["git", *args], cwd=cwd, capture_output=True, text=True, check=False
    )
    if proc.returncode != 0:
        raise GitError(f"git {' '.join(args)} falhou: {proc.stderr.strip()}")
    return proc.stdout


def repo_root(path: Path) -> Path:
    return Path(git("rev-parse", "--show-toplevel", cwd=path).strip())


@dataclass
class ReviewTarget:
    """Um conjunto de mudanças a revisar.

    `diff_cmd` é o comando git que reproduz o diff; o agente pode rodá-lo
    sozinho quando o diff é grande demais para ir inteiro no prompt.
    """

    kind: str  # working | staged | branch | commit
    label: str
    diff_cmd: list[str]
    commit_sha: str | None = None
    commit_message: str | None = None
    files: list[str] = field(default_factory=list)
    diff: str = ""
    stat: str = ""

    def load(self, root: Path) -> "ReviewTarget":
        self.diff = git(*self.diff_cmd, cwd=root)
        self.stat = git(*self.diff_cmd, "--stat", cwd=root)
        self.files = [
            f for f in git(*self.diff_cmd, "--name-only", cwd=root).splitlines() if f
        ]
        return self

    @property
    def empty(self) -> bool:
        return not self.diff.strip()


def working_target() -> ReviewTarget:
    return ReviewTarget("working", "mudanças não commitadas (working tree vs HEAD)", ["diff", "HEAD"])


def staged_target() -> ReviewTarget:
    return ReviewTarget("staged", "mudanças em stage", ["diff", "--cached"])


def branch_target(base: str, root: Path) -> ReviewTarget:
    merge_base = git("merge-base", base, "HEAD", cwd=root).strip()
    head = git("rev-parse", "--short", "HEAD", cwd=root).strip()
    return ReviewTarget(
        "branch",
        f"branch atual ({head}) vs {base}",
        ["diff", f"{merge_base}...HEAD"],
        commit_sha=git("rev-parse", "HEAD", cwd=root).strip(),
    )


def commit_target(rev: str, root: Path) -> ReviewTarget:
    sha = git("rev-parse", "--verify", f"{rev}^{{commit}}", cwd=root).strip()
    msg = git("log", "-1", "--format=%an <%ae>%n%ad%n%n%B", sha, cwd=root).strip()
    subject = git("log", "-1", "--format=%s", sha, cwd=root).strip()
    parents = git("rev-list", "--parents", "-n", "1", sha, cwd=root).split()[1:]
    # Commit raiz não tem pai: compara com a árvore vazia.
    base = parents[0] if parents else git("hash-object", "-t", "tree", "/dev/null", cwd=root).strip()
    return ReviewTarget(
        "commit",
        f"commit {sha[:10]}: {subject}",
        ["diff", base, sha],
        commit_sha=sha,
        commit_message=msg,
    )


def commits_in_range(rev_range: str, root: Path) -> list[str]:
    """SHAs de um intervalo (ex.: main..HEAD), do mais antigo ao mais novo."""
    out = git("rev-list", "--reverse", "--no-merges", rev_range, cwd=root)
    return [s for s in out.splitlines() if s]


def last_commits(n: int, root: Path) -> list[str]:
    out = git("rev-list", "--reverse", "--no-merges", f"--max-count={n}", "HEAD", cwd=root)
    return [s for s in out.splitlines() if s]
