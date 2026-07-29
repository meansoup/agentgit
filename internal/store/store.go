package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const Schema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS agent_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider TEXT NOT NULL,
  agent_name TEXT,
  model TEXT NOT NULL,
  message TEXT NOT NULL,
  repo_root TEXT NOT NULL,
  session_id TEXT,
  turn_id TEXT,
  baseline_status_json TEXT NOT NULL DEFAULT '[]',
  baseline_head TEXT,
  started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_requests_repo_started
ON agent_requests(repo_root, started_at DESC);
`

func DefaultDBPath() string {
	if override := os.Getenv("AGENTGIT_DB"); override != "" {
		abs, err := filepath.Abs(expandHome(override))
		if err == nil {
			return abs
		}
		return expandHome(override)
	}
	return filepath.Join(homeDir(), ".local", "share", "agentgit", "agentgit.sqlite3")
}

func Open() (*sql.DB, error) {
	path := DefaultDBPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func Init() (string, error) {
	db, err := Open()
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err = db.Exec(Schema); err != nil {
		return "", err
	}
	for _, stmt := range []string{
		`ALTER TABLE agent_requests ADD COLUMN agent_name TEXT`,
		`ALTER TABLE agent_requests ADD COLUMN session_id TEXT`,
		`ALTER TABLE agent_requests ADD COLUMN turn_id TEXT`,
		`ALTER TABLE agent_requests ADD COLUMN baseline_head TEXT`,
	} {
		if _, alterErr := db.Exec(stmt); alterErr != nil && !isDuplicateColumnError(alterErr) {
			return "", alterErr
		}
	}
	_, err = db.Exec(`
		UPDATE agent_requests
		SET agent_name = provider
		WHERE agent_name IS NULL AND provider IS NOT NULL
	`)
	if err != nil {
		return "", err
	}
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_agent_requests_agent_turn
		ON agent_requests(agent_name, session_id, turn_id)
	`)
	return DefaultDBPath(), err
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func expandHome(path string) string {
	if path == "~" {
		return homeDir()
	}
	if len(path) > 2 && path[:2] == "~/" {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}
