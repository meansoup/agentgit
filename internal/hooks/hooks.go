package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/store"
)

const AgentgitHookCommand = "agentgit hook codex"
const AgentgitGeminiHookCommand = "agentgit hook gemini"
const (
	agentgitCodexHookStart  = "# BEGIN agentgit codex hooks"
	agentgitCodexHookEnd    = "# END agentgit codex hooks"
	agentgitPostCommitStart = "# BEGIN agentgit post-commit hook"
	agentgitPostCommitEnd   = "# END agentgit post-commit hook"
)

func InstallCodex() (string, error) {
	if _, err := store.Init(); err != nil {
		return "", err
	}
	if err := cleanupLegacyGitHook(); err != nil {
		return "", err
	}
	if err := cleanupLegacyLocalGitHook("."); err != nil {
		return "", err
	}
	runner, err := installCodexHookRunner()
	if err != nil {
		return "", err
	}
	codexDir, err := codexHomeDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return "", err
	}
	if err := cleanupLegacyCodexHooksJSON(codexDir); err != nil {
		return "", err
	}
	configPath := filepath.Join(codexDir, "config.toml")
	if err := installCodexConfigHooks(configPath, runner); err != nil {
		return "", err
	}
	return configPath, nil
}

func InstallGemini() (string, error) {
	if _, err := store.Init(); err != nil {
		return "", err
	}
	runner, err := installGeminiHookRunner()
	if err != nil {
		return "", err
	}
	geminiDir, err := geminiHomeDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		return "", err
	}
	settingsPath := filepath.Join(geminiDir, "settings.json")
	if err := installGeminiSettingsHooks(settingsPath, runner); err != nil {
		return "", err
	}
	return settingsPath, nil
}

func codexHomeDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("CODEX_HOME")); override != "" {
		return filepath.Abs(expandHome(override))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func geminiHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini"), nil
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

func cleanupLegacyLocalGitHook(cwd string) error {
	root, err := git.RepoRoot(cwd)
	if err != nil {
		return nil
	}
	hook := filepath.Join(root, ".git", "hooks", "post-commit")
	raw, err := os.ReadFile(hook)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(raw)
	if !strings.Contains(content, "# Installed by agentgit.") {
		return nil
	}
	if !strings.Contains(content, "agentgit hook post-commit") {
		return nil
	}
	return os.Remove(hook)
}

func cleanupLegacyCodexHooksJSON(codexDir string) error {
	hooksPath := filepath.Join(codexDir, "hooks.json")
	raw, err := os.ReadFile(hooksPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(raw), AgentgitHookCommand) {
		return nil
	}
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse %s: %w", hooksPath, err)
	}
	var hooks map[string][]codexHookGroup
	if hooksRaw, ok := root["hooks"]; ok {
		if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
			return fmt.Errorf("parse %s hooks: %w", hooksPath, err)
		}
	}
	for event, groups := range hooks {
		hooks[event] = removeAgentgitHookHandlers(groups)
		if len(hooks[event]) == 0 {
			delete(hooks, event)
		}
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		hooksRaw, err := json.Marshal(hooks)
		if err != nil {
			return err
		}
		root["hooks"] = hooksRaw
	}
	if len(root) == 0 {
		return os.Remove(hooksPath)
	}
	updated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	return os.WriteFile(hooksPath, updated, 0o600)
}

func removeAgentgitHookHandlers(groups []codexHookGroup) []codexHookGroup {
	kept := groups[:0]
	for _, group := range groups {
		handlers := group.Hooks[:0]
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

func installCodexHookRunner() (string, error) {
	bin, err := resolveAgentgitExecutable()
	if err != nil {
		return "", err
	}
	dataDir := filepath.Dir(store.DefaultDBPath())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", err
	}
	runner := filepath.Join(dataDir, "agentgit-codex-hook")
	body := fmt.Sprintf(`#!/bin/sh
# Installed by agentgit. Used by Codex lifecycle hooks.
PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
export PATH
exec %s hook codex
`, shellQuote(bin))
	if err := os.WriteFile(runner, []byte(body), 0o755); err != nil {
		return "", err
	}
	return runner, nil
}

func installGeminiHookRunner() (string, error) {
	bin, err := resolveAgentgitExecutable()
	if err != nil {
		return "", err
	}
	dataDir := filepath.Dir(store.DefaultDBPath())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", err
	}
	runner := filepath.Join(dataDir, "agentgit-gemini-hook")
	body := fmt.Sprintf(`#!/bin/sh
# Installed by agentgit. Used by Gemini lifecycle hooks.
PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
export PATH
exec %s hook gemini
`, shellQuote(bin))
	if err := os.WriteFile(runner, []byte(body), 0o755); err != nil {
		return "", err
	}
	return runner, nil
}

func EnsurePostCommitHook(root string) error {
	bin, err := resolveAgentgitExecutable()
	if err != nil {
		return err
	}
	hookPath, err := git.GitPath(root, "hooks/post-commit")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		return err
	}
	var existing string
	if raw, err := os.ReadFile(hookPath); err == nil {
		existing = string(raw)
	} else if !os.IsNotExist(err) {
		return err
	}
	cleaned := removeBlock(existing, agentgitPostCommitStart, agentgitPostCommitEnd)
	block := fmt.Sprintf(`%s
PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
export PATH
%s hook post-commit
%s
`, agentgitPostCommitStart, shellQuote(bin), agentgitPostCommitEnd)
	if strings.TrimSpace(cleaned) != "" && !strings.HasSuffix(cleaned, "\n") {
		cleaned += "\n"
	}
	if strings.TrimSpace(cleaned) != "" {
		cleaned += "\n"
	}
	content := cleaned + block
	if !strings.HasPrefix(content, "#!") {
		content = "#!/bin/sh\n" + content
	}
	return os.WriteFile(hookPath, []byte(content), 0o755)
}

func resolveAgentgitExecutable() (string, error) {
	if override := strings.TrimSpace(os.Getenv("AGENTGIT_HOOK_BIN")); override != "" {
		return filepath.Abs(expandHome(override))
	}
	if exe, err := os.Executable(); err == nil {
		if real, realErr := filepath.EvalSymlinks(exe); realErr == nil {
			exe = real
		}
		if isUsableHookExecutable(exe) {
			return exe, nil
		}
	}
	if local, err := resolveLocalCheckoutLauncher(); err == nil {
		return local, nil
	}
	if path, err := exec.LookPath("agentgit"); err == nil {
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return path, nil
		}
		if real, realErr := filepath.EvalSymlinks(abs); realErr == nil {
			abs = real
		}
		return abs, nil
	}
	return "", fmt.Errorf("agentgit executable was not found; install agentgit on PATH or set AGENTGIT_HOOK_BIN")
}

func resolveLocalCheckoutLauncher() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := git.RepoRoot(cwd)
	if err != nil {
		return "", err
	}
	launcher := filepath.Join(root, "bin", "agentgit")
	mainGo := filepath.Join(root, "cmd", "agentgit", "main.go")
	if _, err := os.Stat(launcher); err != nil {
		return "", err
	}
	if _, err := os.Stat(mainGo); err != nil {
		return "", err
	}
	return launcher, nil
}

func isUsableHookExecutable(path string) bool {
	if path == "" {
		return false
	}
	tempDir := os.TempDir()
	if rel, err := filepath.Rel(tempDir, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return false
	}
	return !strings.Contains(path, string(filepath.Separator)+"go-build")
}

func installCodexConfigHooks(configPath, runner string) error {
	var existing string
	if raw, err := os.ReadFile(configPath); err == nil {
		existing = string(raw)
	} else if !os.IsNotExist(err) {
		return err
	}
	cleaned := removeAgentgitCodexHookBlock(existing)
	block := codexConfigHookBlock(runner)
	if strings.TrimSpace(cleaned) != "" && !strings.HasSuffix(cleaned, "\n") {
		cleaned += "\n"
	}
	if strings.TrimSpace(cleaned) != "" {
		cleaned += "\n"
	}
	cleaned += block
	return os.WriteFile(configPath, []byte(cleaned), 0o600)
}

func installGeminiSettingsHooks(settingsPath, runner string) error {
	var settings map[string]interface{}
	if raw, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(raw, &settings); err != nil {
			// If invalid JSON, start fresh or return error?
			// Better return error if it's corrupted but not empty.
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if settings == nil {
		settings = make(map[string]interface{})
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	addGeminiHook := func(eventName string) {
		groups, _ := hooks[eventName].([]interface{})

		// Find existing group with matcher "*" and our hook
		var targetGroup map[string]interface{}
		for _, g := range groups {
			group, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			if group["matcher"] == "*" {
				targetGroup = group
				break
			}
		}

		if targetGroup == nil {
			targetGroup = map[string]interface{}{
				"matcher": "*",
				"hooks":   []interface{}{},
			}
			groups = append(groups, targetGroup)
		}

		hookList, _ := targetGroup["hooks"].([]interface{})

		// Remove existing agentgit hooks
		newHookList := []interface{}{}
		for _, h := range hookList {
			hook, ok := h.(map[string]interface{})
			if !ok {
				newHookList = append(newHookList, h)
				continue
			}
			if name, _ := hook["name"].(string); name != "agentgit" {
				newHookList = append(newHookList, h)
			}
		}

		newHookList = append(newHookList, map[string]interface{}{
			"name":    "agentgit",
			"type":    "command",
			"command": runner,
		})
		targetGroup["hooks"] = newHookList
		hooks[eventName] = groups
	}

	addGeminiHook("BeforeAgent")
	addGeminiHook("AfterAgent")

	updated, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(updated, '\n'), 0o600)
}

func removeAgentgitCodexHookBlock(s string) string {
	return removeBlock(s, agentgitCodexHookStart, agentgitCodexHookEnd)
}

func removeBlock(s, startMarker, endMarker string) string {
	start := strings.Index(s, startMarker)
	if start == -1 {
		return s
	}
	end := strings.Index(s[start:], endMarker)
	if end == -1 {
		return s
	}
	end += start + len(endMarker)
	for end < len(s) && (s[end] == '\n' || s[end] == '\r') {
		end++
	}
	return strings.TrimRight(s[:start], "\r\n") + s[end:]
}

func codexConfigHookBlock(runner string) string {
	command := shellQuote(runner)
	return fmt.Sprintf(`%s

[[hooks.UserPromptSubmit]]
command = %q
timeout = 30
statusMessage = "Recording agent request"

[[hooks.Stop]]
command = %q
timeout = 120
statusMessage = "Committing agent request changes"

%s
`, agentgitCodexHookStart, command, command, agentgitCodexHookEnd)
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
	case "UserPromptSubmit", "user_prompt_submit":
		return handleCodexUserPromptSubmit(input, root)
	case "Stop", "stop":
		return handleCodexStop(input, root)
	default:
		return nil
	}
}

func HandleGemini(r io.Reader) error {
	var input geminiHookInput
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
	var errOut error
	switch input.HookEventName {
	case "BeforeAgent":
		errOut = handleGeminiBeforeAgent(input, root)
	case "AfterAgent":
		errOut = handleGeminiAfterAgent(input, root)
	}
	// Gemini hooks MUST return a JSON object on stdout
	fmt.Println("{}")
	return errOut
}

func handleGeminiBeforeAgent(input geminiHookInput, root string) error {
	if err := EnsurePostCommitHook(root); err != nil {
		return err
	}
	baseline, err := git.StatusPaths(root)
	if err != nil {
		return err
	}
	head, _ := git.Head(root)
	// For Gemini, we don't have an explicit TurnID in the hook input yet.
	// We'll use SessionID as TurnID or generate one if needed.
	// Using SessionID as TurnID for now as they are often 1:1 in turns?
	// Actually, we should probably use a turn counter or something if possible.
	// But let's see if we can use transcript path or something to distinguish turns.
	// For now, let's just use SessionID and accept it might overwrite if multiple turns happen
	// without AfterAgent finishing? No, CreateOrUpdateRequest will just find it.

	// Better: Use SessionID and if we can't find TurnID, we use a hash of the prompt?
	turnID := input.TurnID
	if turnID == "" {
		turnID = "current" // Placeholder
	}

	model := input.Model
	if model == "" {
		model = input.ModelID
	}
	if model == "" {
		model = input.Engine
	}
	if model == "" {
		model = "gemini"
	}

	_, err = store.CreateOrUpdateRequest("gemini", "gemini", model, input.Prompt, root, input.SessionID, turnID, head, baseline)
	return err
}

func handleGeminiAfterAgent(input geminiHookInput, root string) error {
	turnID := input.TurnID
	if turnID == "" {
		turnID = "current"
	}
	req, ok, err := store.FindRequest(root, "gemini", input.SessionID, turnID)
	if err != nil || !ok {
		return err
	}
	return store.FinishRequest(req.ID)
}

type geminiHookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Model          string `json:"model"`
	ModelID        string `json:"model_id"`
	Engine         string `json:"engine"`
	Prompt         string `json:"prompt"`
	TurnID         string `json:"turn_id"` // Hope it exists or we use "current"
}

func handleCodexUserPromptSubmit(input codexHookInput, root string) error {
	if err := EnsurePostCommitHook(root); err != nil {
		return err
	}
	baseline, err := git.StatusPaths(root)
	if err != nil {
		return err
	}
	head, _ := git.Head(root)
	_, err = store.CreateOrUpdateRequest("codex", "codex", input.Model, input.Prompt, root, input.SessionID, input.TurnID, head, baseline)
	return err
}

func handleCodexStop(input codexHookInput, root string) error {
	req, ok, err := store.FindRequest(root, "codex", input.SessionID, input.TurnID)
	if err != nil || !ok {
		return err
	}
	return store.FinishRequest(req.ID)
}

func HandlePostCommit(cwd string) error {
	root, err := git.RepoRoot(cwd)
	if err != nil {
		return nil
	}
	hash, err := git.Head(root)
	if err != nil {
		return err
	}
	req, ok, err := activeRequestForCommit(root, os.Getenv("AGENTGIT_SESSION_ID"))
	if err != nil || !ok {
		return err
	}
	return store.LinkCommit(req.ID, hash, root)
}

func activeRequestForCommit(root, sessionID string) (store.Request, bool, error) {
	if strings.TrimSpace(sessionID) != "" {
		return store.FindActiveRequestBySession(root, sessionID)
	}
	return store.FindSingleActiveRequest(root)
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

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
