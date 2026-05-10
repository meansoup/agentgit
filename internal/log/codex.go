package log

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/minkuik/agentgit/internal/model"
)

// CodexLog represents a Codex log entry with potential nested structures
type CodexLog struct {
	EventMsg     *CodexEventMsg     `json:"event_msg"`
	ResponseItem *CodexResponseItem `json:"response_item"`
	SessionMeta  *CodexSessionMeta  `json:"session_meta"`
	Timestamp    interface{}        `json:"timestamp"` // Can be string or int64
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

	// Ubuntu/Linux 등에서 Codex 로그가 존재할 수 있는 다양한 경로 후보
	possibleDirs := []string{
		filepath.Join(homedir, ".codex", "sessions"),
		filepath.Join(homedir, ".codex", "logs"),
		filepath.Join(homedir, ".agent", "codex", "sessions"),
	}

	// Normalize gitRoot for comparison
	evalRoot, err := filepath.EvalSymlinks(gitRoot)
	if err != nil {
		evalRoot = gitRoot
	}
	evalRoot = filepath.Clean(evalRoot)

	var requests []model.LinkedRequest

	for _, codexDir := range possibleDirs {
		if _, err := os.Stat(codexDir); os.IsNotExist(err) {
			continue
		}

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
			sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")

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
				if entry.EventMsg != nil && (entry.EventMsg.Payload.Type == "user_message" || entry.EventMsg.Payload.Type == "user") {
					rawText = entry.EventMsg.Payload.Message
				} else if entry.ResponseItem != nil && (entry.ResponseItem.Payload.Role == "user" || entry.ResponseItem.Payload.Role == "human") {
					rawText = entry.ResponseItem.Payload.Content
				} else if entry.Type == "user" || entry.Type == "user_message" {
					rawText = entry.Message
				}

				if rawText == "" {
					continue
				}

				// 3. Filter by CWD
				if !flexiblePathMatches(currentCWD, evalRoot) {
					continue
				}

				timestamp := parseFlexibleTimestamp(entry.Timestamp)
				if timestamp.IsZero() {
					timestamp = time.Now()
				}

				text := truncateText(rawText, 100)
				if text == "" {
					continue
				}

				currentSessionID := entry.SessionID
				if currentSessionID == "" {
					currentSessionID = sessionID
				}

				request := model.LinkedRequest{
					ID:        fmt.Sprintf("%s_%d", currentSessionID, timestamp.UnixNano()),
					Provider:  "codex",
					SessionID: currentSessionID,
					Text:      text,
					Timestamp: timestamp,
				}

				requests = append(requests, request)
			}
			return nil
		})
	}

	return requests, nil
}

func flexiblePathMatches(logPath, gitRoot string) bool {
	if logPath == "" {
		return true // If unknown, include it to be safe
	}
	
	cleanLog := filepath.Clean(logPath)
	// Try symlink evaluation for log path as well
	evalLog, err := filepath.EvalSymlinks(cleanLog)
	if err != nil {
		evalLog = cleanLog
	}

	// Check if one is a prefix of another (repo root or subdirs)
	return strings.HasPrefix(strings.ToLower(gitRoot), strings.ToLower(evalLog)) || 
	       strings.HasPrefix(strings.ToLower(evalLog), strings.ToLower(gitRoot))
}

func parseFlexibleTimestamp(ts interface{}) time.Time {
	if ts == nil {
		return time.Time{}
	}

	switch v := ts.(type) {
	case string:
		// Try ISO formats
		if t, err := parseTimestamp(v); err == nil {
			return t
		}
		// Try if it's a numeric string
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parseUnix(i)
		}
	case float64:
		return parseUnix(int64(v))
	case int64:
		return parseUnix(v)
	}

	return time.Time{}
}

func parseUnix(v int64) time.Time {
	if v > 1000000000000 { // Milliseconds
		return time.Unix(v/1000, (v%1000)*1000000).UTC()
	}
	return time.Unix(v, 0).UTC()
}
