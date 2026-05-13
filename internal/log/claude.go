package log

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/minkuik/agentgit/internal/model"
)

// ClaudeLog represents a Claude log entry
type ClaudeLog struct {
	Type      string    `json:"type"`
	Timestamp string    `json:"timestamp"`
	SessionID string    `json:"sessionId"`
	CWD       string    `json:"cwd"`
	Message   Message   `json:"message"`
}

// Message represents the message part of a Claude log
type Message struct {
	Content interface{} `json:"content"`
}

// GetText returns the text content of the message
func (m Message) GetText() string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, part := range v {
			if m, ok := part.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// LoadClaudeRequests loads user requests from Claude logs
func LoadClaudeRequests(gitRoot string) ([]model.LinkedRequest, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Convert git root path to Claude log path format
	pathKey := convertPathToKey(gitRoot)
	claudeDir := filepath.Join(homedir, ".claude", "projects", pathKey)

	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return nil, nil // No Claude logs yet
	}

	var requests []model.LinkedRequest

	// Read all JSONL files in the directory
	files, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read Claude logs directory: %w", err)
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}

		filePath := filepath.Join(claudeDir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var entry ClaudeLog
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}

			if !pathMatches(entry.CWD, gitRoot) {
				continue
			}

			timestamp, err := parseTimestamp(entry.Timestamp)
			if err != nil {
				timestamp = time.Now()
			}

			text := truncateText(entry.Message.GetText(), 100)
			// Skip requests with no meaningful text
			if text == "" {
				continue
			}

			request := model.LinkedRequest{
				ID:        entry.SessionID + "_" + entry.Timestamp, // Make ID more unique
				Provider:  "claude",
				IsUser:    entry.Type == "user",
				SessionID: entry.SessionID,
				Text:      text,
				Timestamp: timestamp,
			}

			requests = append(requests, request)
		}
	}

	return requests, nil
}

func convertPathToKey(path string) string {
	// Convert /Users/minkuik/develop/git/agentgit to -Users-minkuik-develop-git-agentgit
	key := strings.ReplaceAll(path, string(os.PathSeparator), "-")
	// If the original path was absolute (started with /), the key already starts with -.
	return key
}

func pathMatches(logPath, gitRoot string) bool {
	if logPath == "" {
		return true
	}
	return strings.HasPrefix(gitRoot, logPath) || strings.HasPrefix(logPath, gitRoot)
}

func parseTimestamp(ts string) (time.Time, error) {
	// Try various timestamp formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", ts)
}

func truncateText(text string, maxLen int) string {
	// Take only the first meaningful line
	lines := strings.Split(text, "\n")
	firstLine := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Skip system prompts and noise
		if isNoisyLine(trimmed) {
			continue
		}

		firstLine = trimmed
		break
	}

	// If no meaningful line found, use the first line even if it might be noisy
	if firstLine == "" && len(lines) > 0 {
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				firstLine = strings.TrimSpace(line)
				break
			}
		}
	}

	if firstLine == "" {
		return ""
	}

	// Remove XML/markdown tags like <tag>content</tag> but keep content
	firstLine = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(firstLine, "")

	runes := []rune(firstLine)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return string(runes)
}

func isNoisyLine(line string) bool {
	trimmed := strings.TrimSpace(line)

	// Don't skip skill commands anymore, they are often the primary request
	// if strings.HasPrefix(trimmed, "/") { ... }

	// Skip system prompts and noise patterns
	noisyPatterns := []string{
		"Implement the following plan",
		"<command",
	}

	for _, pattern := range noisyPatterns {
		if strings.Contains(line, pattern) {
			return true
		}
	}

	// Skip only exact shell command matches (single word only, no args)
	// This is very conservative to avoid filtering user requests
	if len(trimmed) < 15 && !strings.Contains(trimmed, " ") {
		exactShellCommands := []string{
			"pwd", "clear", "ls", "cd", "exit", "quit",
			"cat", "echo", "git",
		}
		for _, cmd := range exactShellCommands {
			if strings.ToLower(trimmed) == cmd {
				return true
			}
		}
	}

	return false
}
