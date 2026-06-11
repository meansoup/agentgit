package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/store"
)

func TestStyleDiffLineStripsNestedANSIWhenRenderingBackground(t *testing.T) {
	line := "+\x1b[31madded\x1b[0m"

	rendered := styleDiffLine(line, 10)

	if strings.Contains(rendered, "\x1b[31m") {
		t.Fatalf("rendered line retained nested syntax ANSI: %q", rendered)
	}
	if got, want := ansi.Strip(rendered), "+added    "; got != want {
		t.Fatalf("ansi-stripped rendered line = %q, want %q", got, want)
	}
}

func TestSplitDiffStripsNestedANSIFromChangedCells(t *testing.T) {
	lines := []string{
		"-\x1b[31mold\x1b[0m",
		"+\x1b[32mnew\x1b[0m",
	}

	got := splitDiff(lines, 23)
	if len(got) != 1 {
		t.Fatalf("splitDiff returned %d lines, want 1", len(got))
	}
	if strings.Contains(got[0], "\x1b[31m") || strings.Contains(got[0], "\x1b[32m") {
		t.Fatalf("split line retained nested syntax ANSI: %q", got[0])
	}
	want := "old        │ new       "
	if stripped := ansi.Strip(got[0]); stripped != want {
		t.Fatalf("ansi-stripped split line = %q, want %q", stripped, want)
	}
	if width := ansi.StringWidth(ansi.Strip(got[0])); width != 23 {
		t.Fatalf("split line width = %d, want 23", width)
	}
}

func TestSplitDiffFitsNarrowWidths(t *testing.T) {
	lines := []string{
		"-old value",
		"+new value",
	}

	for _, width := range []int{3, 4, 8, 16} {
		got := splitDiff(lines, width)
		if len(got) != 1 {
			t.Fatalf("splitDiff(%d) returned %d lines, want 1", width, len(got))
		}
		if visible := ansi.StringWidth(ansi.Strip(got[0])); visible > width {
			t.Fatalf("splitDiff(%d) visible width = %d, want <= %d: %q", width, visible, width, got[0])
		}
	}
}

func TestRequestPreviewMessageUsesFirstNonEmptyLine(t *testing.T) {
	message := "\n\tOpen @lib/main.go\n\npackage main\nfunc main() {}\n"

	got := requestPreviewMessage(message)
	if want := "Open @lib/main.go"; got != want {
		t.Fatalf("requestPreviewMessage() = %q, want %q", got, want)
	}
}

func TestRequestSummaryLineIsSingleLine(t *testing.T) {
	requests := []store.LinkedRequest{
		{
			AgentName: "gemini",
			Model:     "gemini-2.5-pro",
			Message:   "Review @internal/tui/tui.go\n\nfull file content\nmore content",
		},
		{
			AgentName: "codex",
			Model:     "gpt-5",
			Message:   "second request",
		},
	}

	got := ansi.Strip(requestSummaryLine(requests))
	if strings.Contains(got, "\n") {
		t.Fatalf("requestSummaryLine contained newline: %q", got)
	}
	if strings.Contains(got, "full file content") || strings.Contains(got, "more content") {
		t.Fatalf("requestSummaryLine leaked multiline request content: %q", got)
	}
	if !strings.Contains(got, "Review @internal/tui/tui.go") || !strings.Contains(got, "(+1)") {
		t.Fatalf("requestSummaryLine missing expected preview details: %q", got)
	}
}

func TestMoveInFilesDoesNotLoadDiff(t *testing.T) {
	m := model{
		root: "/definitely/not/a/repo",
		commits: []git.Commit{
			{Hash: "abc", ShortHash: "abc", Subject: "test"},
		},
		files: []string{"a.go", "b.go"},
		mode:  modeFiles,
	}

	m.move(1)

	if m.err != nil {
		t.Fatalf("move in file list loaded diff and set error: %v", m.err)
	}
	if m.fileIdx != 1 {
		t.Fatalf("fileIdx = %d, want 1", m.fileIdx)
	}
	if m.diffLines != nil {
		t.Fatalf("move in file list loaded diff lines: %q", m.diffLines)
	}
}

func TestEnterCommitDoesNotPreloadDiff(t *testing.T) {
	m := model{
		root: "/definitely/not/a/repo",
		commits: []git.Commit{
			{Hash: "abc", ShortHash: "abc", Subject: "test"},
		},
		fileCache: map[string][]string{
			"abc": {"a.go"},
		},
		mode: modeCommits,
	}

	m.enter(false)

	if m.err != nil {
		t.Fatalf("enter commit loaded diff and set error: %v", m.err)
	}
	if m.mode != modeFiles {
		t.Fatalf("mode = %v, want modeFiles", m.mode)
	}
	if m.diffLines != nil {
		t.Fatalf("enter commit loaded diff lines: %q", m.diffLines)
	}
}
