import subprocess
from pathlib import Path

import pytest

from revisor.agent import is_safe_git_command
from revisor.git_utils import (
    branch_target, commit_target, commits_in_range, last_commits, staged_target, working_target,
)
from revisor.prompts import build_prompt


def sh(*args, cwd):
    subprocess.run(args, cwd=cwd, check=True, capture_output=True)


@pytest.fixture
def repo(tmp_path: Path) -> Path:
    sh("git", "init", "-q", "-b", "main", cwd=tmp_path)
    sh("git", "config", "user.email", "t@example.com", cwd=tmp_path)
    sh("git", "config", "user.name", "Teste", cwd=tmp_path)
    for i in range(3):
        (tmp_path / "a.py").write_text(f"x = {i}\n")
        sh("git", "add", ".", cwd=tmp_path)
        sh("git", "commit", "-qm", f"commit {i}", cwd=tmp_path)
    return tmp_path


def test_commit_raiz_e_normal(repo):
    shas = last_commits(10, repo)
    assert len(shas) == 3
    raiz = commit_target(shas[0], repo).load(repo)
    assert "+x = 0" in raiz.diff and raiz.files == ["a.py"]
    t = commit_target(shas[2], repo).load(repo)
    assert "-x = 1" in t.diff and "+x = 2" in t.diff
    assert "commit 2" in t.label and "commit 2" in t.commit_message


def test_intervalo_e_ultimos(repo):
    assert len(commits_in_range("HEAD~2..HEAD", repo)) == 2
    assert last_commits(2, repo) == commits_in_range("HEAD~2..HEAD", repo)


def test_working_staged_branch(repo):
    (repo / "a.py").write_text("x = 99\n")
    assert "+x = 99" in working_target().load(repo).diff
    assert staged_target().load(repo).empty
    sh("git", "checkout", "-qb", "feat", cwd=repo)
    sh("git", "commit", "-qam", "feat", cwd=repo)
    assert "+x = 99" in branch_target("main", repo).load(repo).diff


def test_prompt_diff_grande_nao_vai_inline(repo):
    t = commit_target("HEAD", repo).load(repo)
    assert "```diff" in build_prompt(t, None, 10_000)
    p = build_prompt(t, "- regra X", 5)
    assert "```diff" not in p and "git diff" in p and "regra X" in p


@pytest.mark.parametrize("cmd,ok", [
    ("git diff HEAD~1 -- a.py", True),
    ("git log --oneline -n 20", True),
    ("git --no-pager blame -L 10,20 a.py", True),
    ("git show abc123:src/x.py", True),
    ("git commit -am x", False),
    ("git push", False),
    ("git diff | sh", False),
    ("git log; rm -rf /", False),
    ("git diff --output=/tmp/x", False),
    ("git -c core.pager=evil log", False),
    ("git log $(whoami)", False),
    ("rm -rf .", False),
])
def test_guarda_bash(cmd, ok):
    assert is_safe_git_command(cmd) is ok
