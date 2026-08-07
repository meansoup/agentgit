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

func TestUnpushedCommitsReturnsCommitsAheadOfUpstream(t *testing.T) {
	remote := newTestRepo(t)
	runGit(t, remote, "config", "receive.denyCurrentBranch", "updateInstead")
	root := t.TempDir()
	runGit(t, root, "clone", remote, ".")
	runGit(t, root, "config", "user.email", "agentgit@example.test")
	runGit(t, root, "config", "user.name", "agentgit")
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	runGit(t, root, "push", "-u", "origin", "HEAD")
	writeFile(t, root, "tracked.txt", "changed\n")
	runGit(t, root, "commit", "-am", "local")
	head, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}

	unpushed, err := UnpushedCommits(root)
	if err != nil {
		t.Fatal(err)
	}

	if !unpushed[head] {
		t.Fatalf("unpushed commits = %+v, missing HEAD %s", unpushed, head)
	}
}

func TestUnpushedCommitsWithoutUpstreamReturnsEmptyMap(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")

	unpushed, err := UnpushedCommits(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(unpushed) != 0 {
		t.Fatalf("unpushed commits = %+v, want empty without upstream", unpushed)
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

func TestChangedFileStatusesMarksDeletedCommitFiles(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "deleted.txt", "base\n")
	writeFile(t, root, "modified.txt", "base\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "modified.txt", "changed\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "change")

	head, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := ChangedFileStatuses(root, head)
	if err != nil {
		t.Fatal(err)
	}

	if got := statuses["deleted.txt"]; got != "deleted" {
		t.Fatalf("deleted status = %q, want deleted", got)
	}
	if got := statuses["modified.txt"]; got != "updated" {
		t.Fatalf("modified status = %q, want updated", got)
	}
}

func TestChangedFileStatusesMarksCreatedCommitFiles(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "base.txt", "base\n")
	runGit(t, root, "add", "base.txt")
	runGit(t, root, "commit", "-m", "initial")
	writeFile(t, root, "created.txt", "new\n")
	runGit(t, root, "add", "created.txt")
	runGit(t, root, "commit", "-m", "add file")

	head, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := ChangedFileStatuses(root, head)
	if err != nil {
		t.Fatal(err)
	}

	if got := statuses["created.txt"]; got != "created" {
		t.Fatalf("created status = %q, want created", got)
	}
}

func TestChangedFileChangesDetectsRenamedCommitFiles(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "old.txt", "base\n")
	runGit(t, root, "add", "old.txt")
	runGit(t, root, "commit", "-m", "initial")
	runGit(t, root, "mv", "old.txt", "new.txt")
	runGit(t, root, "commit", "-m", "rename")

	head, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := ChangedFileChanges(root, head)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("ChangedFileChanges returned %+v, want one rename", changes)
	}
	if got := changes[0]; got.Path != "new.txt" || got.OldPath != "old.txt" || got.Status != "renamed" {
		t.Fatalf("rename change = %+v, want old.txt -> new.txt renamed", got)
	}
	statuses, err := ChangedFileStatuses(root, head)
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses["new.txt"]; got != "renamed" {
		t.Fatalf("renamed status = %q, want renamed", got)
	}
}

func TestUnifiedDiffForRenamedCommitFileShowsRenameOnly(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "old.txt", "one\ntwo\n")
	runGit(t, root, "add", "old.txt")
	runGit(t, root, "commit", "-m", "initial")
	runGit(t, root, "mv", "old.txt", "new.txt")
	runGit(t, root, "commit", "-m", "rename")

	head, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := UnifiedDiff(root, head, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "\n")

	for _, want := range []string{"rename from old.txt", "rename to new.txt"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rename diff missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"+one", "+two", "-one", "-two", "--- /dev/null"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("rename-only diff contains content marker %q:\n%s", notWant, got)
		}
	}
}

func TestChangedFileStatusesMarksDeletedUncommittedFiles(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "deleted.txt", "base\n")
	runGit(t, root, "add", "deleted.txt")
	runGit(t, root, "commit", "-m", "initial")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}

	statuses, err := ChangedFileStatuses(root, UncommittedHash)
	if err != nil {
		t.Fatal(err)
	}

	if got := statuses["deleted.txt"]; got != "deleted" {
		t.Fatalf("deleted status = %q, want deleted", got)
	}
}

func TestChangedFileChangesUsesNewPathForUncommittedRename(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "old.txt", "base\n")
	runGit(t, root, "add", "old.txt")
	runGit(t, root, "commit", "-m", "initial")
	runGit(t, root, "mv", "old.txt", "new.txt")

	changes, err := ChangedFileChanges(root, UncommittedHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("ChangedFileChanges returned %+v, want one rename", changes)
	}
	if got := changes[0]; got.Path != "new.txt" || got.OldPath != "old.txt" || got.Status != "renamed" {
		t.Fatalf("uncommitted rename change = %+v, want old.txt -> new.txt renamed", got)
	}
	files, err := ChangedFiles(root, UncommittedHash)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(files, "\n")
	if !strings.Contains(got, "new.txt") || strings.Contains(got, "old.txt") {
		t.Fatalf("ChangedFiles(%q) = %q, want only new path", UncommittedHash, got)
	}
}

func TestUnifiedDiffForUncommittedRenameShowsRenameOnly(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "old.txt", "one\ntwo\n")
	runGit(t, root, "add", "old.txt")
	runGit(t, root, "commit", "-m", "initial")
	runGit(t, root, "mv", "old.txt", "new.txt")

	lines, err := UnifiedDiff(root, UncommittedHash, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "\n")

	for _, want := range []string{"rename from old.txt", "rename to new.txt"} {
		if !strings.Contains(got, want) {
			t.Fatalf("uncommitted rename diff missing %q:\n%s", want, got)
		}
	}
	for _, notWant := range []string{"+one", "+two", "-one", "-two", "--- /dev/null"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("uncommitted rename-only diff contains content marker %q:\n%s", notWant, got)
		}
	}
}

func TestChangedFileStatusesMarksCreatedUncommittedFiles(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "created.txt", "new\n")

	statuses, err := ChangedFileStatuses(root, UncommittedHash)
	if err != nil {
		t.Fatal(err)
	}

	if got := statuses["created.txt"]; got != "created" {
		t.Fatalf("created status = %q, want created", got)
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

func TestResetHardDropsLatestCommitChanges(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	writeFile(t, root, "tracked.txt", "latest\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "latest")
	latest, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	base, err := Parent(root, latest)
	if err != nil {
		t.Fatal(err)
	}

	if err := ResetHard(root, base); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "base\n" {
		t.Fatalf("tracked.txt = %q, want base content", raw)
	}
	head, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	if head != base {
		t.Fatalf("HEAD = %s, want %s", head, base)
	}
}

func TestDiscardUncommittedDropsTrackedAndUntrackedChanges(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	writeFile(t, root, "tracked.txt", "changed\n")
	writeFile(t, root, "new.txt", "new\n")

	if err := DiscardUncommitted(root); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "tracked.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "base\n" {
		t.Fatalf("tracked.txt = %q, want base content", raw)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt still exists or stat failed unexpectedly: %v", err)
	}
	clean, err := IsWorkingTreeClean(root)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatal("working tree is not clean after DiscardUncommitted")
	}
}

func TestSquashSinceCombinesLatestCommits(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	writeFile(t, root, "one.txt", "one\n")
	runGit(t, root, "add", "one.txt")
	runGit(t, root, "commit", "-m", "one")
	oldestSelected, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "two.txt", "two\n")
	runGit(t, root, "add", "two.txt")
	runGit(t, root, "commit", "-m", "two")
	base, err := Parent(root, oldestSelected)
	if err != nil {
		t.Fatal(err)
	}

	newHash, err := SquashSince(root, base, []string{"one", "Squashed 2 commits"})
	if err != nil {
		t.Fatal(err)
	}

	head, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	if head != newHash {
		t.Fatalf("HEAD = %s, want new squash hash %s", head, newHash)
	}
	log, err := Run(root, "log", "--pretty=%s")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimSpace(log), "\n"); len(got) != 2 || got[0] != "one" || got[1] != "initial" {
		t.Fatalf("log subjects = %q, want squashed commit plus initial", log)
	}
	for _, path := range []string{"one.txt", "two.txt"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("%s missing after squash: %v", path, err)
		}
	}
}

func TestDeleteMergedBranchesDeletesMergedNonProtectedBranches(t *testing.T) {
	root := newTestRepo(t)
	writeFile(t, root, "tracked.txt", "base\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	defaultBranch := Branch(root)
	runGit(t, root, "checkout", "-b", "feature/merged")
	writeFile(t, root, "feature.txt", "feature\n")
	runGit(t, root, "add", "feature.txt")
	runGit(t, root, "commit", "-m", "feature")
	runGit(t, root, "checkout", defaultBranch)
	runGit(t, root, "merge", "--no-ff", "feature/merged", "-m", "merge feature")
	runGit(t, root, "checkout", "-b", "feature/unmerged")
	writeFile(t, root, "unmerged.txt", "unmerged\n")
	runGit(t, root, "add", "unmerged.txt")
	runGit(t, root, "commit", "-m", "unmerged")
	runGit(t, root, "checkout", defaultBranch)

	deleted, _, err := DeleteMergedBranches(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(deleted, "\n"); got != "feature/merged" {
		t.Fatalf("deleted branches = %q, want feature/merged", got)
	}
	branches, err := Run(root, "branch", "--format=%(refname:short)")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(branches, "feature/merged") {
		t.Fatalf("merged branch still exists:\n%s", branches)
	}
	for _, want := range []string{defaultBranch, "feature/unmerged"} {
		if !strings.Contains(branches, want) {
			t.Fatalf("branch list missing %q:\n%s", want, branches)
		}
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
