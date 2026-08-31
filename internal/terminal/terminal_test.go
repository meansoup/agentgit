package terminal

import (
	"strings"
	"testing"
)

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

func TestResolveCommandPreservesExplicitCommand(t *testing.T) {
	command, err := resolveCommand([]string{"codex", "--full-auto"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(command, " "), "codex --full-auto"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}
