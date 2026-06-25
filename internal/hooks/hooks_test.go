package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/store"
)

func TestInstallClaudeSettingsHooksPreservesSettingsAndIsIdempotent(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	existing := `{
  "permissions": {"allow": ["Bash(git status)"]},
  "hooks": {
    "Stop": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "/usr/local/bin/existing-hook"}]
      }
    ]
  }
}
`
	if err := os.WriteFile(settingsPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := "/tmp/agentgit-claude-hook"
	if err := installClaudeSettingsHooks(settingsPath, runner); err != nil {
		t.Fatal(err)
	}
	if err := installClaudeSettingsHooks(settingsPath, runner); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings["permissions"]; !ok {
		t.Fatal("existing permissions were removed")
	}
	hooks := settings["hooks"].(map[string]interface{})
	for _, eventName := range []string{"UserPromptSubmit", "Stop"} {
		if got := countHookCommands(hooks[eventName], runner); got != 1 {
			t.Fatalf("%s agentgit hook count = %d, want 1", eventName, got)
		}
	}
	if got := countHookCommands(hooks["Stop"], "/usr/local/bin/existing-hook"); got != 1 {
		t.Fatalf("existing Stop hook count = %d, want 1", got)
	}
}

func TestHandleClaudeStopFinishesRequestWithoutCreatingCommit(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")
	root := newHookTestRepo(t)
	writeHookTestFile(t, root, "base.txt", "base\n")
	runHookTestGit(t, root, "add", "base.txt")
	runHookTestGit(t, root, "commit", "-m", "base")

	submit := claudeHookInput{
		SessionID:     "claude-session",
		CWD:           root,
		HookEventName: "UserPromptSubmit",
		Prompt:        "Add Claude support",
	}
	if err := HandleClaude(jsonInput(t, submit)); err != nil {
		t.Fatal(err)
	}
	writeHookTestFile(t, root, "claude.txt", "supported\n")
	stop := claudeHookInput{
		SessionID:     "claude-session",
		CWD:           root,
		HookEventName: "Stop",
	}
	if err := HandleClaude(jsonInput(t, stop)); err != nil {
		t.Fatal(err)
	}

	if status, err := git.StatusPaths(root); err != nil {
		t.Fatal(err)
	} else if !status["claude.txt"] {
		t.Fatalf("status after Claude stop = %+v, want claude.txt to remain uncommitted", status)
	}
	links, err := store.RequestsByCommit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("Claude stop created commit links unexpectedly: %+v", links)
	}
	if _, ok, err := store.FindActiveRequestBySession(root, "claude-session"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("Claude request remained active after Stop")
	}
}

func TestHandlePostCommitLinksCommitBySessionEnv(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")
	root := newHookTestRepo(t)
	firstID, err := store.CreateRequest("codex", "codex", "gpt", "one", root, "session-1", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := store.CreateRequest("gemini", "gemini", "gemini", "two", root, "session-2", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	writeHookTestFile(t, root, "file.txt", "content\n")
	runHookTestGit(t, root, "add", "file.txt")
	runHookTestGit(t, root, "commit", "-m", "change")
	hash, err := git.Head(root)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTGIT_SESSION_ID", "session-2")
	if err := HandlePostCommit(root); err != nil {
		t.Fatal(err)
	}

	links, err := store.RequestsByCommit(root)
	if err != nil {
		t.Fatal(err)
	}
	got := links[hash]
	if len(got) != 1 {
		t.Fatalf("linked requests = %d, want 1", len(got))
	}
	if got[0].RequestID != secondID {
		t.Fatalf("linked request id = %d, want %d", got[0].RequestID, secondID)
	}
	if got[0].RequestID == firstID {
		t.Fatalf("linked wrong request id %d", firstID)
	}
}

func TestHandlePostCommitWithoutSessionSkipsAmbiguousActiveRequests(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")
	t.Setenv("AGENTGIT_SESSION_ID", "")
	root := newHookTestRepo(t)
	if _, err := store.CreateRequest("codex", "codex", "gpt", "one", root, "session-1", "turn", "head", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRequest("gemini", "gemini", "gemini", "two", root, "session-2", "turn", "head", nil); err != nil {
		t.Fatal(err)
	}
	writeHookTestFile(t, root, "file.txt", "content\n")
	runHookTestGit(t, root, "add", "file.txt")
	runHookTestGit(t, root, "commit", "-m", "change")
	hash, err := git.Head(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := HandlePostCommit(root); err != nil {
		t.Fatal(err)
	}

	links, err := store.RequestsByCommit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(links[hash]) != 0 {
		t.Fatalf("ambiguous post-commit linked requests: %+v", links[hash])
	}
}

func TestHandleCodexStopFinishesRequestWithoutCreatingCommit(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")
	root := newHookTestRepo(t)
	writeHookTestFile(t, root, "base.txt", "base\n")
	runHookTestGit(t, root, "add", "base.txt")
	runHookTestGit(t, root, "commit", "-m", "base")
	reqID, err := store.CreateRequest("codex", "codex", "gpt", "Add feature\n\nmore context", root, "session", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	writeHookTestFile(t, root, "feature.txt", "feature\n")

	if err := handleCodexStop(codexHookInput{SessionID: "session", TurnID: "turn"}, root); err != nil {
		t.Fatal(err)
	}

	status, err := git.StatusPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if !status["feature.txt"] {
		t.Fatalf("status after stop = %+v, want feature.txt to remain uncommitted", status)
	}
	links, err := store.RequestsByCommit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("stop created commit links unexpectedly: %+v", links)
	}
	if _, ok, err := store.FindActiveRequestBySession(root, "session"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("request remained active after stop")
	}
	if reqID <= 0 {
		t.Fatalf("request id = %d, want positive", reqID)
	}
}

func newHookTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runHookTestGit(t, root, "init")
	runHookTestGit(t, root, "config", "user.email", "agentgit@example.test")
	runHookTestGit(t, root, "config", "user.name", "agentgit")
	repoRoot, err := git.RepoRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	return repoRoot
}

func writeHookTestFile(t *testing.T, root string, path string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runHookTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func jsonInput(t *testing.T, value interface{}) *bytes.Reader {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(raw)
}

func countHookCommands(rawGroups interface{}, command string) int {
	groups, _ := rawGroups.([]interface{})
	count := 0
	for _, rawGroup := range groups {
		group, _ := rawGroup.(map[string]interface{})
		handlers, _ := group["hooks"].([]interface{})
		for _, rawHandler := range handlers {
			handler, _ := rawHandler.(map[string]interface{})
			if handler["command"] == command {
				count++
			}
		}
	}
	return count
}
