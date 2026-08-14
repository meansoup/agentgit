package terminal

import (
	"io"
	"os"
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

func TestStatusLineShowsCtrlGCommitShortcut(t *testing.T) {
	s := &session{width: 100}

	status := s.statusLine()

	for _, want := range []string{"agentgit:terminal", "Ctrl-G commits", "Esc returns"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q: %q", want, status)
		}
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
