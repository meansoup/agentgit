package log

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minkuik/agentgit/internal/model"
)

// CodexLog represents a Codex log entry
type CodexLog struct {
	Type      string    `json:"type"`
	Timestamp string    `json:"timestamp"`
	SessionID string    `json:"sessionId"`
	CWD       string    `json:"cwd"`
	Message   string    `json:"message"`
}

// LoadCodexRequests loads user requests from Codex logs
func LoadCodexRequests(gitRoot string) ([]model.LinkedRequest, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	codexDir := filepath.Join(homedir, ".codex", "sessions")

	if _, err := os.Stat(codexDir); os.IsNotExist(err) {
		return nil, nil // No Codex logs yet
	}

	var requests []model.LinkedRequest

	// Recursively read all JSONL files in the directory
	err = filepath.Walk(codexDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var entry CodexLog
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}

			// Filter: only user messages in matching cwd
			if entry.Type != "user" {
				continue
			}

			if !pathMatches(entry.CWD, gitRoot) {
				continue
			}

			timestamp, err := parseTimestamp(entry.Timestamp)
			if err != nil {
				timestamp = time.Now()
			}

			text := truncateText(entry.Message, 100)
			if text == "" {
				continue
			}

			request := model.LinkedRequest{
				ID:        entry.SessionID + "_" + entry.Timestamp,
				Provider:  "codex",
				SessionID: entry.SessionID,
				Text:      text,
				Timestamp: timestamp,
			}

			requests = append(requests, request)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk Codex logs: %w", err)
	}

	return requests, nil
}
