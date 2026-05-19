from __future__ import annotations

import argparse
import sys
from pathlib import Path

from . import db, hooks, tui
from .git import GitError, commit_all, repo_root, status_paths
from .state import clear_active_request, get_active_request, set_active_request


PROVIDERS = ("codex", "gemini", "claude")


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    try:
        return int(args.func(args) or 0)
    except (GitError, KeyError, ValueError) as exc:
        print(f"agentgit: {exc}", file=sys.stderr)
        return 1


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="agentgit")
    sub = parser.add_subparsers(required=True)

    setup = sub.add_parser("setup", help="install hooks and initialize the local database")
    setup.set_defaults(func=cmd_setup)

    setup_global = sub.add_parser("setup-global", help="install global hooks once for this PC")
    setup_global.set_defaults(func=cmd_setup_global)

    log = sub.add_parser("log", help="browse commits with linked AI requests")
    log.add_argument("--limit", type=int, default=500)
    log.set_defaults(func=cmd_log)

    hook = sub.add_parser("hook", help="internal hook entrypoints")
    hook_sub = hook.add_subparsers(required=True)
    post_commit = hook_sub.add_parser("post-commit")
    post_commit.set_defaults(func=cmd_hook_post_commit)

    request = sub.add_parser("request", help="generic request commands")
    add_request_subcommands(request, provider=None)

    for provider in PROVIDERS:
        provider_parser = sub.add_parser(provider, help=f"{provider} request commands")
        add_request_subcommands(provider_parser, provider=provider)

    return parser


def add_request_subcommands(parser: argparse.ArgumentParser, provider: str | None) -> None:
    sub = parser.add_subparsers(required=True)
    start = sub.add_parser("start", help="record an agent request and snapshot current dirty files")
    if provider is None:
        start.add_argument("--provider", required=True, choices=PROVIDERS)
    start.add_argument("--model", required=True)
    start.add_argument("--message", required=True)
    start.add_argument(
        "--include-current",
        action="store_true",
        help="treat existing dirty files as part of this request",
    )
    start.set_defaults(func=cmd_start, provider=provider)

    commit = sub.add_parser("commit", help="commit only files changed after request start")
    commit.add_argument("-m", "--message", required=True)
    commit.set_defaults(func=cmd_commit, provider=provider)

    finish = sub.add_parser("finish", help="finish the active request")
    finish.set_defaults(func=cmd_finish, provider=provider)


def cmd_setup(_args: argparse.Namespace) -> int:
    root = repo_root()
    db_path = db.init_db()
    hook = hooks.install_post_commit_hook(root)
    print(f"initialized db: {db_path}")
    print(f"installed hook: {hook}")
    return 0


def cmd_setup_global(_args: argparse.Namespace) -> int:
    db_path = db.init_db()
    hook = hooks.install_global_hooks()
    print(f"initialized db: {db_path}")
    print(f"installed global hook: {hook}")
    print(f"configured: git config --global core.hooksPath {hook.parent}")
    return 0


def cmd_log(args: argparse.Namespace) -> int:
    root = repo_root()
    db.init_db()
    tui.run(root, args.limit)
    return 0


def cmd_hook_post_commit(_args: argparse.Namespace) -> int:
    hooks.handle_post_commit()
    return 0


def cmd_start(args: argparse.Namespace) -> int:
    root = repo_root()
    provider = args.provider or getattr(args, "provider", None)
    if provider is None:
        raise ValueError("provider is required")
    baseline = set() if args.include_current else status_paths(root)
    request_id = db.create_request(provider, args.model, args.message, root, baseline)
    set_active_request(root, request_id)
    print(f"started {provider} request #{request_id}")
    return 0


def cmd_commit(args: argparse.Namespace) -> int:
    root = repo_root()
    request_id = get_active_request(root)
    if request_id is None:
        raise ValueError("no active agentgit request")
    request = db.get_request(request_id)
    if Path(request.repo_root).resolve() != root:
        raise ValueError(f"active request #{request_id} belongs to {request.repo_root}")
    current_paths = status_paths(root)
    owned_paths = current_paths - request.baseline_paths
    commit_hash = commit_all(root, owned_paths, args.message)
    db.link_commit(request_id, commit_hash, root)
    print(f"committed {commit_hash[:8]} for {request.provider} request #{request.id}")
    return 0


def cmd_finish(_args: argparse.Namespace) -> int:
    root = repo_root()
    request_id = get_active_request(root)
    if request_id is None:
        raise ValueError("no active agentgit request")
    db.finish_request(request_id)
    clear_active_request(root)
    print(f"finished request #{request_id}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
