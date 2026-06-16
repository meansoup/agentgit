package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/store"
)

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
