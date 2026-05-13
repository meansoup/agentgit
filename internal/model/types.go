package model

import "time"

// ChangeSet represents a commit or the current working tree
type ChangeSet struct {
	ID         string
	Type       string // "uncommitted" | "commit"
	Title      string
	CommitHash string
	Author     string
	Timestamp  time.Time
	Summary    string
	FileCount  int
	Requests   []LinkedRequest
}

// LinkedRequest represents a user request from Claude, Gemini or Codex
type LinkedRequest struct {
	ID        string
	Provider  string // "claude" | "gemini" | "codex"
	IsUser    bool
	SessionID string
	Text      string
	Timestamp time.Time
}

// ChangedFile represents a file changed in a commit or working tree
type ChangedFile struct {
	Path      string
	OldPath   string
	Status    string // added|modified|deleted|renamed|copied|unknown
	Additions int
	Deletions int
}

// FileDiff represents the diff for a file
type FileDiff struct {
	Path     string
	Patch    string
	IsBinary bool
	TooLarge bool
}

// Commit represents a git commit
type Commit struct {
	Hash      string
	Author    string
	Timestamp time.Time
	Title     string
	Body      string
}
