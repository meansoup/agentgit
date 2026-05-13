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
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp interface{}     `json:"timestamp"` // Can be string or int64
	SessionID string          `json:"sessionId"`
	// Legacy or flat fields
	CWD     string `json:"cwd"`
	Message string `json:"message"`
}

type CodexEventPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type CodexResponsePayload struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []map[string]interface{}
}

type CodexSessionMetaPayload struct {
	CWD string `json:"cwd"`
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

				var rawText string
				var isUser bool
				// 1. Process based on Type
				switch entry.Type {
				case "session_meta":
					var p CodexSessionMetaPayload
					if err := json.Unmarshal(entry.Payload, &p); err == nil && p.CWD != "" {
						currentCWD = p.CWD
					}
				case "event_msg":
					var p CodexEventPayload
					if err := json.Unmarshal(entry.Payload, &p); err == nil {
						rawText = p.Message
						isUser = (p.Type == "user_message" || p.Type == "user")
					}
				case "response_item":
					var p CodexResponsePayload
					if err := json.Unmarshal(entry.Payload, &p); err == nil {
						rawText = extractCodexText(p.Content)
						isUser = (p.Role == "user" || p.Role == "human")
					}
				default:
					// Legacy or flat fields
					rawText = entry.Message
					isUser = (entry.Type == "user" || entry.Type == "user_message")
				}

				// 2. Update CWD from top-level if present (legacy or redundancy)
				if entry.CWD != "" {
					currentCWD = entry.CWD
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
					IsUser:    isUser,
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

func extractCodexText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					sb.WriteString(text)
				}
			}
		}
		return sb.String()
	}
	return ""
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
