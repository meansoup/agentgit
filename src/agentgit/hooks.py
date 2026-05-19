from __future__ import annotations

import os
from pathlib import Path

from . import db
from .git import git_dir, repo_root, run_git
from .state import get_active_request


def global_hooks_dir() -> Path:
    return Path.home() / ".config" / "agentgit" / "hooks"


def install_global_hooks() -> Path:
    hooks_dir = global_hooks_dir()
    hooks_dir.mkdir(parents=True, exist_ok=True)
    previous = run_git(["config", "--global", "--get", "core.hooksPath"], check=False).strip()
    previous_path = Path(previous).expanduser() if previous else None
    if previous_path is not None and not previous_path.is_absolute():
        previous_path = (Path.cwd() / previous_path).resolve()
    if previous_path == hooks_dir:
        previous_path = None
    hook = install_hook_file(hooks_dir / "post-commit", previous_path)
    run_git(["config", "--global", "core.hooksPath", str(hooks_dir)])
    return hook


def install_post_commit_hook(root: Path) -> Path:
    hooks_dir = git_dir(root) / "hooks"
    hooks_dir.mkdir(parents=True, exist_ok=True)
    return install_hook_file(hooks_dir / "post-commit")


def install_hook_file(hook: Path, previous_hooks_dir: Path | None = None) -> Path:
    backup = hook.with_name(f"{hook.name}.agentgit-backup")
    if hook.exists() and "agentgit hook post-commit" not in hook.read_text(encoding="utf-8", errors="ignore"):
        if not backup.exists():
            backup.write_bytes(hook.read_bytes())
        existing = hook.read_text(encoding="utf-8", errors="ignore")
        hook.write_text(
            existing.rstrip()
            + "\n\n# agentgit\n"
            + hook_snippet(previous_hooks_dir),
            encoding="utf-8",
        )
    else:
        hook.write_text("#!/bin/sh\n# Installed by agentgit.\n" + hook_snippet(previous_hooks_dir), encoding="utf-8")
    os.chmod(hook, 0o755)
    return hook


def hook_snippet(previous_hooks_dir: Path | None = None) -> str:
    source_root = Path(__file__).resolve().parents[2]
    source_src = source_root / "src"
    lines = [
        "if command -v agentgit >/dev/null 2>&1; then",
        "  agentgit hook post-commit",
    ]
    if source_src.exists():
        lines.extend(
            [
                "elif command -v python3 >/dev/null 2>&1; then",
                f"  PYTHONPATH='{source_src}' python3 -m agentgit.cli hook post-commit",
            ]
        )
    lines.append("fi")
    if previous_hooks_dir is not None:
        previous_hook = previous_hooks_dir / "post-commit"
        lines.extend(
            [
                "",
                f"if [ -x '{previous_hook}' ]; then",
                f"  '{previous_hook}' \"$@\"",
                "fi",
            ]
        )
    lines.append("")
    return "\n".join(lines)


def handle_post_commit() -> str | None:
    root = repo_root()
    request_id = get_active_request(root)
    if request_id is None:
        return None
    commit_hash = run_git(["rev-parse", "HEAD"], cwd=root).strip()
    db.link_commit(request_id, commit_hash, root)
    return commit_hash
