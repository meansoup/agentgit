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

// GeminiLog represents a Gemini log entry
type GeminiLog struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// ProjectMapping represents the Gemini projects.json mapping
type ProjectMapping struct {
	Projects map[string]string `json:"projects"`
}

// LoadGeminiRequests loads user requests from Gemini logs
func LoadGeminiRequests(gitRoot string) ([]model.LinkedRequest, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Load project mapping
	projectsFile := filepath.Join(homedir, ".gemini", "projects.json")
	projectID, err := getProjectID(projectsFile, gitRoot)
	if err != nil {
		return nil, nil // No project mapping
	}

	// Read logs for this project
	logsPath := filepath.Join(homedir, ".gemini", "tmp", projectID, "logs.json")
	if _, err := os.Stat(logsPath); os.IsNotExist(err) {
		return nil, nil // No Gemini logs yet
	}

	data, err := os.ReadFile(logsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Gemini logs: %w", err)
	}

	var logs []GeminiLog
	if err := json.Unmarshal(data, &logs); err != nil {
		return nil, fmt.Errorf("failed to parse Gemini logs: %w", err)
	}

	var requests []model.LinkedRequest
	for _, log := range logs {
		if log.Type != "user" {
			continue
		}

		timestamp, err := parseTimestamp(log.Timestamp)
		if err != nil {
			timestamp = time.Now()
		}

		text := truncateText(log.Message, 100)
		// Skip requests with no meaningful text
		if text == "" {
			continue
		}

		request := model.LinkedRequest{
			ID:        log.SessionID + "_" + log.Timestamp,
			Provider:  "gemini",
			SessionID: log.SessionID,
			Text:      text,
			Timestamp: timestamp,
		}

		requests = append(requests, request)
	}

	return requests, nil
}

func getProjectID(projectsFile, gitRoot string) (string, error) {
	data, err := os.ReadFile(projectsFile)
	if err != nil {
		return "", fmt.Errorf("failed to read projects.json: %w", err)
	}

	var mapping ProjectMapping
	if err := json.Unmarshal(data, &mapping); err != nil {
		return "", fmt.Errorf("failed to parse projects.json: %w", err)
	}

	// Normalize gitRoot by removing trailing slash
	gitRoot = filepath.Clean(gitRoot)

	// Try exact match first
	for path, id := range mapping.Projects {
		if filepath.Clean(path) == gitRoot {
			return id, nil
		}
	}

	// Then try prefix match
	for path, id := range mapping.Projects {
		cleanPath := filepath.Clean(path)
		if strings.HasPrefix(gitRoot, cleanPath) || strings.HasPrefix(cleanPath, gitRoot) {
			return id, nil
		}
	}

	return "", fmt.Errorf("no project mapping found for %s", gitRoot)
}
