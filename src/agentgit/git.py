from __future__ import annotations

import subprocess
from dataclasses import dataclass
from pathlib import Path


class GitError(RuntimeError):
    pass


@dataclass(frozen=True)
class Commit:
    hash: str
    short_hash: str
    date: str
    subject: str


def run_git(args: list[str], cwd: Path | None = None, check: bool = True) -> str:
    proc = subprocess.run(
        ["git", *args],
        cwd=cwd,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if check and proc.returncode != 0:
        raise GitError(proc.stderr.strip() or proc.stdout.strip())
    return proc.stdout


def repo_root(cwd: Path | None = None) -> Path:
    return Path(run_git(["rev-parse", "--show-toplevel"], cwd=cwd).strip()).resolve()


def git_dir(root: Path) -> Path:
    path = run_git(["rev-parse", "--git-dir"], cwd=root).strip()
    resolved = Path(path)
    if not resolved.is_absolute():
        resolved = root / resolved
    return resolved.resolve()


def status_paths(root: Path) -> set[str]:
    raw = run_git(["status", "--porcelain=v1", "-z"], cwd=root)
    paths: set[str] = set()
    entries = [entry for entry in raw.split("\0") if entry]
    idx = 0
    while idx < len(entries):
        entry = entries[idx]
        status = entry[:2]
        path = entry[3:]
        if status.startswith("R") or status.startswith("C"):
            idx += 1
            if idx < len(entries):
                path = entries[idx]
        if path:
            paths.add(path)
        idx += 1
    return paths


def commits(root: Path, limit: int = 500) -> list[Commit]:
    fmt = "%H%x1f%h%x1f%ad%x1f%s"
    raw = run_git(
        ["log", f"--max-count={limit}", "--date=format:%m-%d %H:%M", f"--pretty=format:{fmt}"],
        cwd=root,
    )
    result: list[Commit] = []
    for line in raw.splitlines():
        parts = line.split("\x1f", 3)
        if len(parts) == 4:
            result.append(Commit(hash=parts[0], short_hash=parts[1], date=parts[2], subject=parts[3]))
    return result


def changed_files(root: Path, commit_hash: str) -> list[str]:
    raw = run_git(["show", "--pretty=format:", "--name-only", commit_hash], cwd=root)
    return [line for line in raw.splitlines() if line.strip()]


def unified_diff(root: Path, commit_hash: str, path: str) -> list[str]:
    raw = run_git(["show", "--format=", "--no-ext-diff", commit_hash, "--", path], cwd=root)
    return raw.splitlines()


def commit_all(root: Path, paths: set[str], message: str) -> str:
    if not paths:
        raise GitError("No request-owned file changes to commit.")
    run_git(["add", "--", *sorted(paths)], cwd=root)
    run_git(["commit", "-m", message], cwd=root)
    return run_git(["rev-parse", "HEAD"], cwd=root).strip()
