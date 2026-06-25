package store

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	AgentName     string
	Model         string
	Message       string
	RepoRoot      string
	SessionID     string
	TurnID        string
	BaselinePaths map[string]bool
	BaselineHead  string
}

type LinkedRequest struct {
	RequestID int64
	AgentName string
	Model     string
	Message   string
}

type RequestSummary struct {
	ID         int64
	AgentName  string
	Model      string
	Message    string
	StartedAt  string
	CommitRefs []string
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

func CreateRequest(provider, agentName, model, message, repoRoot, sessionID, turnID, baselineHead string, baseline map[string]bool) (int64, error) {
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
		`INSERT INTO agent_requests(provider, agent_name, model, message, repo_root, session_id, turn_id, baseline_status_json, baseline_head)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provider,
		agentName,
		model,
		message,
		repoRoot,
		sessionID,
		turnID,
		string(raw),
		baselineHead,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func CreateOrUpdateRequest(provider, agentName, model, message, repoRoot, sessionID, turnID, baselineHead string, baseline map[string]bool) (int64, error) {
	if existing, ok, err := FindRequest(repoRoot, agentName, sessionID, turnID); err != nil {
		return 0, err
	} else if ok {
		return existing.ID, nil
	}
	return CreateRequest(provider, agentName, model, message, repoRoot, sessionID, turnID, baselineHead, baseline)
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
		`SELECT id, COALESCE(agent_name, provider), model, message, repo_root, COALESCE(session_id, ''), COALESCE(turn_id, ''), baseline_status_json, COALESCE(baseline_head, '')
		 FROM agent_requests WHERE id = ?`,
		id,
	).Scan(&req.ID, &req.AgentName, &req.Model, &req.Message, &req.RepoRoot, &req.SessionID, &req.TurnID, &baselineRaw, &req.BaselineHead)
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

func FindRequest(repoRoot, agentName, sessionID, turnID string) (Request, bool, error) {
	if _, err := Init(); err != nil {
		return Request{}, false, err
	}
	db, err := Open()
	if err != nil {
		return Request{}, false, err
	}
	defer db.Close()
	var id int64
	err = db.QueryRow(
		`SELECT id
		 FROM agent_requests
		 WHERE repo_root = ? AND COALESCE(agent_name, provider) = ? AND session_id = ? AND turn_id = ? AND finished_at IS NULL
		 ORDER BY id DESC
		 LIMIT 1`,
		repoRoot,
		agentName,
		sessionID,
		turnID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		// Try without finished_at IS NULL just in case, but for Gemini it's important to find the right turn.
		// Actually, for Codex it might be fine to find finished ones if turn_id is unique.
		err = db.QueryRow(
			`SELECT id
			 FROM agent_requests
			 WHERE repo_root = ? AND COALESCE(agent_name, provider) = ? AND session_id = ? AND turn_id = ?
			 ORDER BY id DESC
			 LIMIT 1`,
			repoRoot,
			agentName,
			sessionID,
			turnID,
		).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, false, nil
	}
	if err != nil {
		return Request{}, false, err
	}
	req, err := GetRequest(id)
	return req, err == nil, err
}

func FindActiveRequestBySession(repoRoot, sessionID string) (Request, bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Request{}, false, nil
	}
	if _, err := Init(); err != nil {
		return Request{}, false, err
	}
	db, err := Open()
	if err != nil {
		return Request{}, false, err
	}
	defer db.Close()
	var id int64
	err = db.QueryRow(
		`SELECT id
		 FROM agent_requests
		 WHERE repo_root = ? AND session_id = ? AND finished_at IS NULL
		 ORDER BY id DESC
		 LIMIT 1`,
		repoRoot,
		sessionID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, false, nil
	}
	if err != nil {
		return Request{}, false, err
	}
	req, err := GetRequest(id)
	return req, err == nil, err
}

func FindSingleActiveRequest(repoRoot string) (Request, bool, error) {
	if _, err := Init(); err != nil {
		return Request{}, false, err
	}
	db, err := Open()
	if err != nil {
		return Request{}, false, err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT id
		 FROM agent_requests
		 WHERE repo_root = ? AND finished_at IS NULL
		 ORDER BY id DESC
		 LIMIT 2`,
		repoRoot,
	)
	if err != nil {
		return Request{}, false, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return Request{}, false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return Request{}, false, err
	}
	if len(ids) != 1 {
		return Request{}, false, nil
	}
	req, err := GetRequest(ids[0])
	return req, err == nil, err
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

func DeleteCommitLinks(repoRoot string, commitHashes []string) error {
	if len(commitHashes) == 0 {
		return nil
	}
	if _, err := Init(); err != nil {
		return err
	}
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, hash := range commitHashes {
		if _, err := tx.Exec(
			`DELETE FROM request_commits WHERE repo_root = ? AND commit_hash = ?`,
			repoRoot,
			hash,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func MoveCommitLinks(repoRoot string, fromHashes []string, toHash string) error {
	if len(fromHashes) == 0 {
		return nil
	}
	if strings.TrimSpace(toHash) == "" {
		return errors.New("target commit hash is empty")
	}
	if _, err := Init(); err != nil {
		return err
	}
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, hash := range fromHashes {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO request_commits(request_id, commit_hash, repo_root)
			 SELECT request_id, ?, repo_root
			 FROM request_commits
			 WHERE repo_root = ? AND commit_hash = ?`,
			toHash,
			repoRoot,
			hash,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	for _, hash := range fromHashes {
		if _, err := tx.Exec(
			`DELETE FROM request_commits WHERE repo_root = ? AND commit_hash = ?`,
			repoRoot,
			hash,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
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
		`SELECT rc.commit_hash, ar.id, COALESCE(ar.agent_name, ar.provider), ar.model, ar.message
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
		if err := rows.Scan(&commitHash, &req.RequestID, &req.AgentName, &req.Model, &req.Message); err != nil {
			return nil, err
		}
		result[commitHash] = append(result[commitHash], req)
	}
	return result, rows.Err()
}

func RequestsByRepo(repoRoot string) ([]RequestSummary, error) {
	if _, err := Init(); err != nil {
		return nil, err
	}
	db, err := Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT ar.id,
		        COALESCE(ar.agent_name, ar.provider),
		        ar.model,
		        ar.message,
		        ar.started_at,
		        rc.commit_hash
		 FROM agent_requests ar
		 JOIN request_commits rc
		   ON rc.request_id = ar.id AND rc.repo_root = ar.repo_root
		 WHERE ar.repo_root = ?
		 ORDER BY ar.started_at DESC, ar.id DESC, rc.linked_at ASC, rc.commit_hash ASC`,
		repoRoot,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RequestSummary
	indexByID := map[int64]int{}
	for rows.Next() {
		var (
			id        int64
			agentName string
			model     string
			message   string
			startedAt string
			commitRef string
		)
		if err := rows.Scan(&id, &agentName, &model, &message, &startedAt, &commitRef); err != nil {
			return nil, err
		}
		index, ok := indexByID[id]
		if !ok {
			index = len(result)
			indexByID[id] = index
			result = append(result, RequestSummary{
				ID:        id,
				AgentName: agentName,
				Model:     model,
				Message:   message,
				StartedAt: startedAt,
			})
		}
		result[index].CommitRefs = append(result[index].CommitRefs, commitRef)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
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
