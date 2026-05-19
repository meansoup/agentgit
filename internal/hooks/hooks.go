package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/store"
)

const AgentgitHookCommand = "agentgit hook codex"

func InstallCodex() (string, error) {
	if _, err := store.Init(); err != nil {
		return "", err
	}
	if err := cleanupLegacyGitHook(); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return "", err
	}
	hooksPath := filepath.Join(codexDir, "hooks.json")
	root := map[string]json.RawMessage{}
	cfg := codexHooksConfig{}
	if raw, err := os.ReadFile(hooksPath); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &root); err != nil {
			return "", fmt.Errorf("parse %s: %w", hooksPath, err)
		}
		if hooksRaw, ok := root["hooks"]; ok {
			if err := json.Unmarshal(hooksRaw, &cfg.Hooks); err != nil {
				return "", fmt.Errorf("parse %s hooks: %w", hooksPath, err)
			}
		}
	}
	if cfg.Hooks == nil {
		cfg.Hooks = map[string][]codexHookGroup{}
	}
	cfg.Hooks["UserPromptSubmit"] = appendWithoutAgentgit(cfg.Hooks["UserPromptSubmit"])
	cfg.Hooks["UserPromptSubmit"] = append(cfg.Hooks["UserPromptSubmit"], codexHookGroup{
		Hooks: []codexHookHandler{{
			Type:          "command",
			Command:       AgentgitHookCommand,
			Timeout:       30,
			StatusMessage: "Recording agent request",
		}},
	})
	cfg.Hooks["Stop"] = appendWithoutAgentgit(cfg.Hooks["Stop"])
	cfg.Hooks["Stop"] = append(cfg.Hooks["Stop"], codexHookGroup{
		Hooks: []codexHookHandler{{
			Type:          "command",
			Command:       AgentgitHookCommand,
			Timeout:       120,
			StatusMessage: "Linking request to commit",
		}},
	})
	hooksRaw, err := json.Marshal(cfg.Hooks)
	if err != nil {
		return "", err
	}
	root["hooks"] = hooksRaw
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(hooksPath, raw, 0o644); err != nil {
		return "", err
	}
	return hooksPath, nil
}

func cleanupLegacyGitHook() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	legacyDir := filepath.Join(home, ".config", "agentgit", "hooks")
	current := strings.TrimSpace(git.RunAllowError("", "config", "--global", "--get", "core.hooksPath"))
	if current == "" {
		return nil
	}
	if current != legacyDir {
		return nil
	}
	hook := filepath.Join(legacyDir, "post-commit")
	raw, err := os.ReadFile(hook)
	if err != nil {
		return nil
	}
	if !strings.Contains(string(raw), "agentgit hook post-commit") {
		return nil
	}
	_, err = git.Run("", "config", "--global", "--unset", "core.hooksPath")
	return err
}

func HandleCodex(r io.Reader) error {
	var input codexHookInput
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return err
	}
	if input.CWD == "" {
		return nil
	}
	root, err := git.RepoRoot(input.CWD)
	if err != nil {
		return nil
	}
	switch input.HookEventName {
	case "UserPromptSubmit":
		return handleCodexUserPromptSubmit(input, root)
	case "Stop":
		return handleCodexStop(input, root)
	default:
		return nil
	}
}

func handleCodexUserPromptSubmit(input codexHookInput, root string) error {
	baseline, err := git.StatusPaths(root)
	if err != nil {
		return err
	}
	head, _ := git.Head(root)
	_, err = store.CreateOrUpdateRequest("codex", input.Model, input.Prompt, root, input.SessionID, input.TurnID, head, baseline)
	return err
}

func handleCodexStop(input codexHookInput, root string) error {
	req, ok, err := store.FindRequest("codex", input.SessionID, input.TurnID)
	if err != nil || !ok {
		return err
	}
	current, err := git.StatusPaths(root)
	if err != nil {
		return err
	}
	owned := map[string]bool{}
	for path := range current {
		if !req.BaselinePaths[path] {
			owned[path] = true
		}
	}
	if len(owned) > 0 {
		commitHash, err := git.CommitPaths(root, owned, commitMessage(req))
		if err != nil {
			return err
		}
		if err := store.LinkCommit(req.ID, commitHash, root); err != nil {
			return err
		}
		return store.FinishRequest(req.ID)
	}
	hashes, err := git.CommitsAfter(root, req.BaselineHead)
	if err != nil {
		return err
	}
	for _, hash := range hashes {
		if err := store.LinkCommit(req.ID, hash, root); err != nil {
			return err
		}
	}
	return store.FinishRequest(req.ID)
}

type codexHooksConfig struct {
	Hooks map[string][]codexHookGroup `json:"hooks"`
}

type codexHookGroup struct {
	Matcher string             `json:"matcher,omitempty"`
	Hooks   []codexHookHandler `json:"hooks"`
}

type codexHookHandler struct {
	Type          string `json:"type"`
	Command       string `json:"command"`
	Timeout       int    `json:"timeout,omitempty"`
	StatusMessage string `json:"statusMessage,omitempty"`
}

type codexHookInput struct {
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	CWD              string `json:"cwd"`
	HookEventName    string `json:"hook_event_name"`
	Model            string `json:"model"`
	TurnID           string `json:"turn_id"`
	Prompt           string `json:"prompt"`
	LastAssistantMsg string `json:"last_assistant_message"`
}

func appendWithoutAgentgit(groups []codexHookGroup) []codexHookGroup {
	var kept []codexHookGroup
	for _, group := range groups {
		var handlers []codexHookHandler
		for _, handler := range group.Hooks {
			if strings.TrimSpace(handler.Command) != AgentgitHookCommand {
				handlers = append(handlers, handler)
			}
		}
		if len(handlers) > 0 {
			group.Hooks = handlers
			kept = append(kept, group)
		}
	}
	return kept
}

func commitMessage(req store.Request) string {
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "agent request"
	}
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) > 80 {
		message = message[:80]
	}
	return fmt.Sprintf("agentgit(%s): %s", req.AgentName, message)
}
