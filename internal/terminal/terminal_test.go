package terminal

import (
	"strings"
	"testing"
)

func TestCtrlGHOpensHelp(t *testing.T) {
	s := &session{}

	s.handleInput([]byte{ctrlG, 'h'})

	if !s.help {
		t.Fatal("help = false, want true")
	}
	if s.prefix {
		t.Fatal("prefix = true, want false")
	}
	if s.status != "help" {
		t.Fatalf("status = %q, want help", s.status)
	}
}

func TestCtrlGQuestionMarkStillOpensHelpWhenSentAsASCII(t *testing.T) {
	s := &session{}

	s.handleInput([]byte{ctrlG, '?'})

	if !s.help {
		t.Fatal("help = false, want true")
	}
	if s.prefix {
		t.Fatal("prefix = true, want false")
	}
}

func TestCtrlGEscCancelsPrefix(t *testing.T) {
	s := &session{}

	s.handleInput([]byte{ctrlG, esc})

	if s.prefix {
		t.Fatal("prefix = true, want false")
	}
	if s.status != "prefix canceled" {
		t.Fatalf("status = %q, want prefix canceled", s.status)
	}
}

func TestPrefixStatusShowsCommandMode(t *testing.T) {
	s := &session{width: 160}

	s.handleInput([]byte{ctrlG})
	status := s.statusLine()

	for _, want := range []string{"agentgit:prefix", "c commits", "h help", "Esc cancel"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q: %q", want, status)
		}
	}
}

func TestStatusLinePrioritizesCommands(t *testing.T) {
	s := &session{
		width:   80,
		root:    "/repo",
		command: []string{"a-very-long-agent-command", "with", "many", "arguments"},
	}

	s.handleInput([]byte{ctrlG})
	status := s.statusLine()

	for _, want := range []string{"c commits", "h help", "q quit", "Esc cancel"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q: %q", want, status)
		}
	}
}
