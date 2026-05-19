package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const Schema = `
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
`

type Request struct {
	ID            int64
	Provider      string
	Model         string
	Message       string
	RepoRoot      string
	BaselinePaths map[string]bool
}

type LinkedRequest struct {
	RequestID int64
	Provider  string
	Model     string
	Message   string
}

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
	_, err = db.Exec(Schema)
	return DefaultDBPath(), err
}

func CreateRequest(provider, model, message, repoRoot string, baseline map[string]bool) (int64, error) {
	if _, err := Init(); err != nil {
		return 0, err
	}
	db, err := Open()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	paths := make([]string, 0, len(baseline))
	for path := range baseline {
		paths = append(paths, path)
	}
	raw, err := json.Marshal(paths)
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(
		`INSERT INTO agent_requests(provider, model, message, repo_root, baseline_status_json)
		 VALUES (?, ?, ?, ?, ?)`,
		provider,
		model,
		message,
		repoRoot,
		string(raw),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetRequest(id int64) (Request, error) {
	if _, err := Init(); err != nil {
		return Request{}, err
	}
	db, err := Open()
	if err != nil {
		return Request{}, err
	}
	defer db.Close()
	var req Request
	var baselineRaw string
	err = db.QueryRow(
		`SELECT id, provider, model, message, repo_root, baseline_status_json
		 FROM agent_requests WHERE id = ?`,
		id,
	).Scan(&req.ID, &req.Provider, &req.Model, &req.Message, &req.RepoRoot, &baselineRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, errors.New("request does not exist")
	}
	if err != nil {
		return Request{}, err
	}
	var paths []string
	if err := json.Unmarshal([]byte(baselineRaw), &paths); err != nil {
		return Request{}, err
	}
	req.BaselinePaths = map[string]bool{}
	for _, path := range paths {
		req.BaselinePaths[path] = true
	}
	return req, nil
}

func FinishRequest(id int64) error {
	if _, err := Init(); err != nil {
		return err
	}
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(
		`UPDATE agent_requests
		 SET finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		 WHERE id = ?`,
		id,
	)
	return err
}

func LinkCommit(requestID int64, commitHash, repoRoot string) error {
	if _, err := Init(); err != nil {
		return err
	}
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT OR IGNORE INTO request_commits(request_id, commit_hash, repo_root)
		 VALUES (?, ?, ?)`,
		requestID,
		commitHash,
		repoRoot,
	)
	return err
}

func RequestsByCommit(repoRoot string) (map[string][]LinkedRequest, error) {
	if _, err := Init(); err != nil {
		return nil, err
	}
	db, err := Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT rc.commit_hash, ar.id, ar.provider, ar.model, ar.message
		 FROM request_commits rc
		 JOIN agent_requests ar ON ar.id = rc.request_id
		 WHERE rc.repo_root = ?
		 ORDER BY rc.linked_at ASC, ar.id ASC`,
		repoRoot,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]LinkedRequest{}
	for rows.Next() {
		var commitHash string
		var req LinkedRequest
		if err := rows.Scan(&commitHash, &req.RequestID, &req.Provider, &req.Model, &req.Message); err != nil {
			return nil, err
		}
		result[commitHash] = append(result[commitHash], req)
	}
	return result, rows.Err()
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
