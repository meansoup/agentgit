from __future__ import annotations

from pathlib import Path

from .git import git_dir


def state_dir(repo_root: Path) -> Path:
    path = git_dir(repo_root) / "agentgit"
    path.mkdir(parents=True, exist_ok=True)
    return path


def active_request_file(repo_root: Path) -> Path:
    return state_dir(repo_root) / "active-request"


def set_active_request(repo_root: Path, request_id: int) -> None:
    active_request_file(repo_root).write_text(f"{request_id}\n", encoding="utf-8")


def get_active_request(repo_root: Path) -> int | None:
    path = active_request_file(repo_root)
    if not path.exists():
        return None
    raw = path.read_text(encoding="utf-8").strip()
    return int(raw) if raw else None


def clear_active_request(repo_root: Path) -> None:
    path = active_request_file(repo_root)
    if path.exists():
        path.unlink()
