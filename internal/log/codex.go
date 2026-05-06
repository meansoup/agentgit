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

// CodexLog represents a Codex log entry with potential nested structures
type CodexLog struct {
	EventMsg     *CodexEventMsg     `json:"event_msg"`
	ResponseItem *CodexResponseItem `json:"response_item"`
	SessionMeta  *CodexSessionMeta  `json:"session_meta"`
	Timestamp    string             `json:"timestamp"`
	SessionID    string             `json:"sessionId"`
	// Legacy or flat fields
	Type    string `json:"type"`
	CWD     string `json:"cwd"`
	Message string `json:"message"`
}

type CodexEventMsg struct {
	Payload struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"payload"`
}

type CodexResponseItem struct {
	Payload struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"payload"`
}

type CodexSessionMeta struct {
	Payload struct {
		CWD string `json:"cwd"`
	} `json:"payload"`
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

		// Each file is usually one session, track CWD within the file
		var currentCWD string

		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var entry CodexLog
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}

			// 1. Update CWD if meta info is present
			if entry.SessionMeta != nil && entry.SessionMeta.Payload.CWD != "" {
				currentCWD = entry.SessionMeta.Payload.CWD
			} else if entry.CWD != "" {
				currentCWD = entry.CWD
			}

			// 2. Extract user message text
			var rawText string
			if entry.EventMsg != nil && entry.EventMsg.Payload.Type == "user_message" {
				rawText = entry.EventMsg.Payload.Message
			} else if entry.ResponseItem != nil && entry.ResponseItem.Payload.Role == "user" {
				rawText = entry.ResponseItem.Payload.Content
			} else if entry.Type == "user" {
				rawText = entry.Message
			}

			if rawText == "" {
				continue
			}

			// 3. Filter by CWD
			if !pathMatches(currentCWD, gitRoot) {
				continue
			}

			timestamp, err := parseTimestamp(entry.Timestamp)
			if err != nil {
				timestamp = time.Now()
			}

			text := truncateText(rawText, 100)
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
