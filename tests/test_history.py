import subprocess
from pathlib import Path

from revisor import history
from revisor.agent import ReviewResult
from revisor.git_utils import commit_target


def test_historico_fora_do_repo(tmp_path: Path, monkeypatch):
    repo = tmp_path / "meu-repo"
    repo.mkdir()
    for cmd in (["git", "init", "-q"], ["git", "config", "user.email", "t@example.com"],
                ["git", "config", "user.name", "T"]):
        subprocess.run(cmd, cwd=repo, check=True)
    (repo / "a.py").write_text("x = 1\n")
    subprocess.run(["git", "add", "."], cwd=repo, check=True)
    subprocess.run(["git", "commit", "-qm", "c"], cwd=repo, check=True)

    home = tmp_path / "dados"
    monkeypatch.setenv("DANTE_HOME", str(home))
    t = commit_target("HEAD", repo).load(repo)
    achado = {"severidade": "alta", "arquivo": "a.py", "linha": 1}
    path, rec = history.save(ReviewResult(t, "ok", [achado]), repo)

    assert home in path.parents and repo not in path.parents
    assert history.history_dir(repo).name.startswith("meu-repo-")
    assert [r["resumo"] for r in history.load_all(repo)] == ["ok"]
    assert history.reviewed_commits(repo) == {t.commit_sha}
    assert not (repo / ".revisor").exists()
