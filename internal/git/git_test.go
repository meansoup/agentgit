package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitsWithUncommittedAddsWorkingTreeEntryFirst(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	writeFile(t, root, "tracked.txt", "changed\n")
	writeFile(t, root, "new.txt", "new\n")

	commits, err := CommitsWithUncommitted(root, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("CommitsWithUncommitted returned %d commits, want 2", len(commits))
	}
	if commits[0].Hash != UncommittedHash {
		t.Fatalf("first commit hash = %q, want %q", commits[0].Hash, UncommittedHash)
	}
	if !strings.Contains(commits[0].Subject, "Uncommitted files") {
		t.Fatalf("first commit subject = %q, want uncommitted label", commits[0].Subject)
	}
	if commits[1].Subject != "initial" {
		t.Fatalf("second commit subject = %q, want initial", commits[1].Subject)
	}
}

func TestUncommittedFilesIncludesTrackedAndUntrackedFiles(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	writeFile(t, root, "tracked.txt", "changed\n")
	writeFile(t, root, "new.txt", "new\n")

	files, err := ChangedFiles(root, UncommittedHash)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(files, "\n")
	for _, want := range []string{"new.txt", "tracked.txt"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ChangedFiles(%q) = %q, missing %q", UncommittedHash, got, want)
		}
	}
}

func TestUncommittedDiffForUntrackedFile(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	writeFile(t, root, "new.txt", "first\nsecond\n")

	lines, err := UnifiedDiff(root, UncommittedHash, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{"--- /dev/null", "+++ b/new.txt", "+first", "+second"} {
		if !strings.Contains(got, want) {
			t.Fatalf("UnifiedDiff(%q, new.txt) missing %q:\n%s", UncommittedHash, want, got)
		}
	}
}

func TestBranchReturnsCurrentBranch(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	runGit(t, root, "checkout", "-b", "feature/context")

	if got, want := Branch(root), "feature/context"; got != want {
		t.Fatalf("Branch() = %q, want %q", got, want)
	}
}

func newTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "agentgit@example.test")
	runGit(t, root, "config", "user.name", "agentgit")
	return root
}

func writeFile(t *testing.T, root string, path string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
