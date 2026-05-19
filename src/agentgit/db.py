from __future__ import annotations

import json
import os
import sqlite3
from dataclasses import dataclass
from importlib import resources
from pathlib import Path


@dataclass(frozen=True)
class Request:
    id: int
    provider: str
    model: str
    message: str
    repo_root: str
    baseline_paths: set[str]


@dataclass(frozen=True)
class LinkedRequest:
    request_id: int
    provider: str
    model: str
    message: str


def default_db_path() -> Path:
    override = os.environ.get("AGENTGIT_DB")
    if override:
        return Path(override).expanduser().resolve()
    return Path.home() / ".local" / "share" / "agentgit" / "agentgit.sqlite3"


def connect(db_path: Path | None = None) -> sqlite3.Connection:
    path = db_path or default_db_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA foreign_keys = ON")
    return conn


def init_db(db_path: Path | None = None) -> Path:
    path = db_path or default_db_path()
    with connect(path) as conn:
        schema = resources.files("agentgit").joinpath("schema.sql").read_text(encoding="utf-8")
        conn.executescript(schema)
    return path


def create_request(provider: str, model: str, message: str, repo_root: Path, baseline_paths: set[str]) -> int:
    init_db()
    with connect() as conn:
        cur = conn.execute(
            """
            INSERT INTO agent_requests(provider, model, message, repo_root, baseline_status_json)
            VALUES (?, ?, ?, ?, ?)
            """,
            (provider, model, message, str(repo_root), json.dumps(sorted(baseline_paths))),
        )
        return int(cur.lastrowid)


def get_request(request_id: int) -> Request:
    init_db()
    with connect() as conn:
        row = conn.execute(
            """
            SELECT id, provider, model, message, repo_root, baseline_status_json
            FROM agent_requests
            WHERE id = ?
            """,
            (request_id,),
        ).fetchone()
    if row is None:
        raise KeyError(f"Request {request_id} does not exist.")
    return Request(
        id=int(row["id"]),
        provider=str(row["provider"]),
        model=str(row["model"]),
        message=str(row["message"]),
        repo_root=str(row["repo_root"]),
        baseline_paths=set(json.loads(row["baseline_status_json"])),
    )


def finish_request(request_id: int) -> None:
    init_db()
    with connect() as conn:
        conn.execute(
            """
            UPDATE agent_requests
            SET finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
            WHERE id = ?
            """,
            (request_id,),
        )


def link_commit(request_id: int, commit_hash: str, repo_root: Path) -> None:
    init_db()
    with connect() as conn:
        conn.execute(
            """
            INSERT OR IGNORE INTO request_commits(request_id, commit_hash, repo_root)
            VALUES (?, ?, ?)
            """,
            (request_id, commit_hash, str(repo_root)),
        )


def requests_by_commit(repo_root: Path) -> dict[str, list[LinkedRequest]]:
    init_db()
    with connect() as conn:
        rows = conn.execute(
            """
            SELECT
              rc.commit_hash,
              ar.id AS request_id,
              ar.provider,
              ar.model,
              ar.message
            FROM request_commits rc
            JOIN agent_requests ar ON ar.id = rc.request_id
            WHERE rc.repo_root = ?
            ORDER BY rc.linked_at ASC, ar.id ASC
            """,
            (str(repo_root),),
        ).fetchall()
    result: dict[str, list[LinkedRequest]] = {}
    for row in rows:
        result.setdefault(str(row["commit_hash"]), []).append(
            LinkedRequest(
                request_id=int(row["request_id"]),
                provider=str(row["provider"]),
                model=str(row["model"]),
                message=str(row["message"]),
            )
        )
    return result
