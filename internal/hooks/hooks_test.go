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

func TestHandleClaudeAutoCommitsAndLinksRequest(t *testing.T) {
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
	} else if len(status) != 0 {
		t.Fatalf("status after Claude auto commit = %+v, want clean", status)
	}
	if subject := strings.TrimSpace(git.RunAllowError(root, "log", "-1", "--pretty=%s")); subject != "agentgit: Add Claude support" {
		t.Fatalf("latest commit subject = %q", subject)
	}
	hash, err := git.Head(root)
	if err != nil {
		t.Fatal(err)
	}
	links, err := store.RequestsByCommit(root)
	if err != nil {
		t.Fatal(err)
	}
	got := links[hash]
	if len(got) != 1 || got[0].AgentName != "claude" || got[0].Model != "claude" {
		t.Fatalf("Claude request links = %+v", got)
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

func TestHandleCodexStopAutoCommitsRequestChanges(t *testing.T) {
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
	if len(status) != 0 {
		t.Fatalf("status after auto commit = %+v, want clean", status)
	}
	subject := strings.TrimSpace(git.RunAllowError(root, "log", "-1", "--pretty=%s"))
	if subject != "agentgit: Add feature" {
		t.Fatalf("latest commit subject = %q, want auto commit subject", subject)
	}
	hash, err := git.Head(root)
	if err != nil {
		t.Fatal(err)
	}
	links, err := store.RequestsByCommit(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := links[hash]; len(got) != 1 || got[0].RequestID != reqID {
		t.Fatalf("auto commit links = %+v, want request %d", got, reqID)
	}
	if _, ok, err := store.FindActiveRequestBySession(root, "session"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("request remained active after auto commit")
	}
}

func TestHandleCodexStopSkipsAutoCommitWhenBaselineWasDirty(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")
	root := newHookTestRepo(t)
	writeHookTestFile(t, root, "base.txt", "base\n")
	runHookTestGit(t, root, "add", "base.txt")
	runHookTestGit(t, root, "commit", "-m", "base")
	if _, err := store.CreateRequest("codex", "codex", "gpt", "Change dirty file", root, "session", "turn", "head", map[string]bool{"dirty.txt": true}); err != nil {
		t.Fatal(err)
	}
	writeHookTestFile(t, root, "dirty.txt", "dirty\n")

	if err := handleCodexStop(codexHookInput{SessionID: "session", TurnID: "turn"}, root); err != nil {
		t.Fatal(err)
	}

	status, err := git.StatusPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if !status["dirty.txt"] {
		t.Fatalf("dirty baseline file was committed unexpectedly; status = %+v", status)
	}
	count := strings.TrimSpace(git.RunAllowError(root, "rev-list", "--count", "HEAD"))
	if count != "1" {
		t.Fatalf("commit count = %q, want original commit only", count)
	}
	hash, err := git.Head(root)
	if err != nil {
		t.Fatal(err)
	}
	links, err := store.RequestsByCommit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(links[hash]) != 0 {
		t.Fatalf("baseline-dirty request was linked to a commit: %+v", links[hash])
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
