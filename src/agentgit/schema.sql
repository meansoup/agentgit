PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS agent_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  message TEXT NOT NULL,
  repo_root TEXT NOT NULL,
  baseline_status_json TEXT NOT NULL DEFAULT '[]',
  started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  finished_at TEXT
);

CREATE TABLE IF NOT EXISTS request_commits (
  request_id INTEGER NOT NULL REFERENCES agent_requests(id) ON DELETE CASCADE,
  commit_hash TEXT NOT NULL,
  repo_root TEXT NOT NULL,
  linked_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (request_id, commit_hash)
);

CREATE INDEX IF NOT EXISTS idx_agent_requests_repo_started
ON agent_requests(repo_root, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_request_commits_repo_commit
ON request_commits(repo_root, commit_hash);
