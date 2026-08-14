package terminal

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCtrlGOpensCommitView(t *testing.T) {
	s := &session{actionCh: make(chan actionRequest)}
	opened := make(chan struct{})
	go func() {
		request := <-s.actionCh
		if request.action != actionCommitView {
			t.Errorf("action = %v, want actionCommitView", request.action)
		}
		close(request.done)
		close(opened)
	}()

	s.handleInput([]byte{ctrlG})

	<-opened
	if s.status != "opening commits..." {
		t.Fatalf("status = %q, want opening commits...", s.status)
	}
}

func TestResolveCommandDefaultsToShell(t *testing.T) {
	t.Setenv("AGENTGIT_AGENT", "")
	t.Setenv("SHELL", "/bin/test-shell")

	command, err := resolveCommand(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(command, " "), "/bin/test-shell"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestResolveCommandUsesConfiguredAgent(t *testing.T) {
	t.Setenv("AGENTGIT_AGENT", "claude --dangerously-skip-permissions")
	t.Setenv("SHELL", "/bin/test-shell")

	command, err := resolveCommand(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(command, " "), "/bin/test-shell -lc claude --dangerously-skip-permissions"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestCtrlGDoesNotForwardToAgent(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readEnd.Close()
	s := &session{
		ptmx:     writeEnd,
		actionCh: make(chan actionRequest),
	}
	go func() {
		request := <-s.actionCh
		close(request.done)
	}()

	s.handleInput([]byte("a"))
	s.handleInput([]byte{ctrlG})
	s.handleInput([]byte("b"))
	writeEnd.Close()

	data, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "ab" {
		t.Fatalf("agent input = %q, want ab", got)
	}
}

func TestStatusLineShowsGitState(t *testing.T) {
	s := &session{
		width: 100,
		gitState: gitStatusState{
			Branch:      "main",
			HasUpstream: true,
			Ahead:       2,
			Behind:      1,
			DirtyFiles:  3,
			Additions:   12,
			Deletions:   4,
		},
	}

	status := s.statusLine()

	for _, want := range []string{"agentgit", "main", "server ↑2 ↓1", "dirty 3 files +12 -4", "Ctrl-G commits"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q: %q", want, status)
		}
	}
}

func TestLoadGitStatusIncludesAheadAndDirtyStats(t *testing.T) {
	remote := newTerminalTestRepo(t)
	runGitCommand(t, remote, "config", "receive.denyCurrentBranch", "updateInstead")
	root := t.TempDir()
	runGitCommand(t, root, "clone", remote, ".")
	runGitCommand(t, root, "config", "user.email", "agentgit@example.test")
	runGitCommand(t, root, "config", "user.name", "agentgit")
	writeTerminalTestFile(t, root, "tracked.txt", "base\n")
	runGitCommand(t, root, "add", "tracked.txt")
	runGitCommand(t, root, "commit", "-m", "initial")
	runGitCommand(t, root, "push", "-u", "origin", "HEAD")
	writeTerminalTestFile(t, root, "tracked.txt", "base\nlocal\n")
	runGitCommand(t, root, "commit", "-am", "local")
	writeTerminalTestFile(t, root, "tracked.txt", "base\nlocal\nworktree\n")
	writeTerminalTestFile(t, root, "new.txt", "one\ntwo\n")

	state := loadGitStatus(root)

	if state.Err != "" {
		t.Fatalf("Err = %q, want empty", state.Err)
	}
	if state.Branch == "" {
		t.Fatal("Branch is empty")
	}
	if !state.HasUpstream || state.Ahead != 1 || state.Behind != 0 {
		t.Fatalf("upstream = %v ahead=%d behind=%d, want upstream ahead=1 behind=0", state.HasUpstream, state.Ahead, state.Behind)
	}
	if state.DirtyFiles != 2 {
		t.Fatalf("DirtyFiles = %d, want 2", state.DirtyFiles)
	}
	if state.Additions != 3 || state.Deletions != 0 {
		t.Fatalf("lines = +%d -%d, want +3 -0", state.Additions, state.Deletions)
	}
}

func TestObservePTYOutputTracksAltScreen(t *testing.T) {
	s := &session{}

	s.observePTYOutputLocked([]byte("before\x1b[?1049hafter"))
	if !s.agentAlt {
		t.Fatal("agentAlt = false, want true")
	}

	s.observePTYOutputLocked([]byte("\x1b[?1049l"))
	if s.agentAlt {
		t.Fatal("agentAlt = true, want false")
	}
}

func TestObservePTYOutputTracksSplitAltScreenSequence(t *testing.T) {
	s := &session{}

	s.observePTYOutputLocked([]byte("before\x1b[?10"))
	s.observePTYOutputLocked([]byte("49hafter"))

	if !s.agentAlt {
		t.Fatal("agentAlt = false, want true")
	}
}

func newTerminalTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitCommand(t, root, "init", "-b", "main")
	runGitCommand(t, root, "config", "user.email", "agentgit@example.test")
	runGitCommand(t, root, "config", "user.name", "agentgit")
	return root
}

func writeTerminalTestFile(t *testing.T, root string, path string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGitCommand(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}
