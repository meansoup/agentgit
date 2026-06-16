package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func TestHeaderShowsRepoContext(t *testing.T) {
	m := model{
		root:   "/Users/example/develop/git/agentgit",
		branch: "main",
		head:   "69e67e5",
		commits: []git.Commit{
			{Hash: git.UncommittedHash, ShortHash: "uncommitted", Subject: "Uncommitted files (2)"},
			{Hash: "abc", ShortHash: "abc", Subject: "change"},
		},
		fileCache: map[string][]string{
			git.UncommittedHash: {"a.go", "b.go"},
		},
		mode:  modeCommits,
		width: 160,
	}

	header := ansi.Strip(m.viewHeader())

	for _, want := range []string{"view:commits", "base ", "branch main", "head 69e67e5", "commits 1", "dirty 2"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header missing %q:\n%s", want, header)
		}
	}
}

func TestQuestionMarkOpensHelpDialog(t *testing.T) {
	m := model{
		root:   "/repo",
		branch: "main",
		head:   "abc",
		commits: []git.Commit{
			{Hash: "abc", ShortHash: "abc", Subject: "change"},
		},
		mode:   modeCommits,
		width:  100,
		height: 30,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	got := updated.(model)

	if !got.helpOpen {
		t.Fatal("helpOpen = false, want true")
	}
	view := ansi.Strip(got.View())
	for _, want := range []string{"Help", "view: commits", "enter/right", "Open files"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help view missing %q:\n%s", want, view)
		}
	}
}

func TestHelpDialogClosesWithoutQuitting(t *testing.T) {
	m := model{
		mode:     modeDiff,
		helpOpen: true,
		width:    100,
		height:   30,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(model)

	if got.helpOpen {
		t.Fatal("helpOpen = true, want false")
	}
	if cmd != nil {
		t.Fatal("esc while help is open returned a quit command")
	}
}

func TestQDoesNotQuit(t *testing.T) {
	m := model{mode: modeCommits}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("q returned a quit command")
	}
	if got.mode != modeCommits {
		t.Fatalf("mode = %v, want modeCommits", got.mode)
	}
}

func TestCtrlCQuits(t *testing.T) {
	m := model{mode: modeCommits}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if cmd == nil {
		t.Fatal("ctrl+c did not return a quit command")
	}
}

func TestFooterIncludesHelpShortcut(t *testing.T) {
	m := model{
		mode:  modeFiles,
		width: 120,
	}

	footer := ansi.Strip(m.viewFooter())

	if !strings.Contains(footer, "?") || !strings.Contains(footer, "help") {
		t.Fatalf("footer missing help shortcut:\n%s", footer)
	}
}

func TestContextLineCompactsLongPath(t *testing.T) {
	m := model{
		root:   "/very/long/path/that/will/not/fit/inside/the/context/window/agentgit",
		branch: "main",
		head:   "abcdef123456",
	}

	line := m.contextLine(48)

	if width := lipgloss.Width(line); width > 120 {
		t.Fatalf("contextLine width = %d, unexpectedly wide: %q", width, line)
	}
	if !strings.Contains(line, "...") || !strings.Contains(line, "agentgit") {
		t.Fatalf("contextLine did not compact path usefully: %q", line)
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

func TestLoadDirectoryEntriesGroupsChangedFiles(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: "c1", ShortHash: "c1", Subject: "newest"},
			{Hash: "c2", ShortHash: "c2", Subject: "older"},
		},
		fileCache: map[string][]string{
			"c1": {"internal/tui/tui.go", "README.md"},
			"c2": {"internal/hooks/hooks.go"},
		},
	}

	m.loadDirectoryEntries()

	entries := map[string]directoryEntry{}
	for _, entry := range m.dirEntries {
		entries[entry.Path] = entry
	}
	internal := entries["internal"]
	if !internal.IsDir {
		t.Fatalf("internal entry IsDir = false, want true")
	}
	if internal.FileCount != 2 {
		t.Fatalf("internal FileCount = %d, want 2", internal.FileCount)
	}
	if got := internal.CommitIndexes; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("internal CommitIndexes = %v, want [0 1]", got)
	}
	readme := entries["README.md"]
	if readme.IsDir {
		t.Fatalf("README.md entry IsDir = true, want false")
	}
	if got := readme.CommitIndexes; len(got) != 1 || got[0] != 0 {
		t.Fatalf("README.md CommitIndexes = %v, want [0]", got)
	}
}

func TestDirectoryViewExpandsOnlyCurrentFilePath(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: "c1", ShortHash: "c1", Subject: "newest"},
		},
		fileCache: map[string][]string{
			"c1": {
				"internal/tui/tui.go",
				"internal/hooks/hooks.go",
				"README.md",
				"docs/PRD.md",
			},
		},
		files:   []string{"internal/tui/tui.go"},
		fileIdx: 0,
	}

	m.loadDirectoryEntries()
	m.expandCurrentDirectoryPath()

	var paths []string
	for _, entry := range m.visibleDirectoryEntries() {
		paths = append(paths, entry.Path)
	}
	got := strings.Join(paths, "\n")
	for _, want := range []string{"README.md", "docs", "internal", "internal/tui", "internal/tui/tui.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("visible directories missing %q:\n%s", want, got)
		}
	}
	for _, hidden := range []string{"docs/PRD.md", "internal/hooks/hooks.go"} {
		if strings.Contains(got, hidden) {
			t.Fatalf("visible directories included folded path %q:\n%s", hidden, got)
		}
	}
}

func TestEnterDirectoryEntryTogglesFolderExpansion(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: "c1", ShortHash: "c1", Subject: "newest"},
		},
		fileCache: map[string][]string{
			"c1": {"docs/PRD.md", "README.md"},
		},
		mode: modeDirectories,
	}
	m.loadDirectoryEntries()
	for i, entry := range m.visibleDirectoryEntries() {
		if entry.Path == "docs" {
			m.dirIdx = i
			break
		}
	}

	m.enter(false)

	if !m.expanded["docs"] {
		t.Fatal("docs was not expanded")
	}
	var expanded []string
	for _, entry := range m.visibleDirectoryEntries() {
		expanded = append(expanded, entry.Path)
	}
	if !strings.Contains(strings.Join(expanded, "\n"), "docs/PRD.md") {
		t.Fatalf("expanded entries missing docs/PRD.md: %v", expanded)
	}

	m.enter(false)

	if m.expanded["docs"] {
		t.Fatal("docs remained expanded after second enter")
	}
	var collapsed []string
	for _, entry := range m.visibleDirectoryEntries() {
		collapsed = append(collapsed, entry.Path)
	}
	if strings.Contains(strings.Join(collapsed, "\n"), "docs/PRD.md") {
		t.Fatalf("collapsed entries still include docs/PRD.md: %v", collapsed)
	}
}

func TestTabTogglesBetweenCommitAndDirectoryViews(t *testing.T) {
	m := model{
		commits: []git.Commit{{Hash: "c1", ShortHash: "c1", Subject: "change"}},
		fileCache: map[string][]string{
			"c1": {"internal/tui/tui.go"},
		},
		mode: modeCommits,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(model)
	if got.mode != modeDirectories {
		t.Fatalf("mode after first tab = %v, want modeDirectories", got.mode)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
	got = updated.(model)
	if got.mode != modeCommits {
		t.Fatalf("mode after second tab = %v, want modeCommits", got.mode)
	}
}

func TestEnterDirectoryEntryOpensLatestMatchingCommitFiles(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: "c1", ShortHash: "c1", Subject: "newest"},
			{Hash: "c2", ShortHash: "c2", Subject: "older"},
		},
		fileCache: map[string][]string{
			"c1": {"internal/tui/tui.go", "README.md"},
			"c2": {"internal/hooks/hooks.go"},
		},
		mode: modeDirectories,
	}
	m.loadDirectoryEntries()
	m.expanded = map[string]bool{"internal": true, "internal/tui": true}
	for i, entry := range m.visibleDirectoryEntries() {
		if entry.Path == "internal/tui/tui.go" {
			m.dirIdx = i
			break
		}
	}

	m.enter(false)

	if m.mode != modeFiles {
		t.Fatalf("mode = %v, want modeFiles", m.mode)
	}
	if m.commitIdx != 0 {
		t.Fatalf("commitIdx = %d, want 0", m.commitIdx)
	}
	if len(m.files) != 1 || m.files[0] != "internal/tui/tui.go" {
		t.Fatalf("files = %v, want latest internal files only", m.files)
	}
}

func TestSelectedLatestRangeAllowsContiguousHeadRangeWithUncommittedEntry(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: git.UncommittedHash, ShortHash: "uncommitted", Subject: "dirty"},
			{Hash: "head", ShortHash: "head", Subject: "head"},
			{Hash: "older", ShortHash: "older", Subject: "older"},
			{Hash: "oldest", ShortHash: "oldest", Subject: "oldest"},
		},
		selected: map[string]bool{
			"head":  true,
			"older": true,
		},
	}

	got, err := m.selectedLatestRange(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Hash != "head" || got[1].Hash != "older" {
		t.Fatalf("selectedLatestRange = %+v, want head and older", got)
	}
}

func TestSelectedLatestRangeRejectsSelectionThatSkipsHead(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: "head", ShortHash: "head", Subject: "head"},
			{Hash: "older", ShortHash: "older", Subject: "older"},
		},
		selected: map[string]bool{
			"older": true,
		},
	}

	if _, err := m.selectedLatestRange(false); err == nil {
		t.Fatal("selectedLatestRange succeeded without selecting HEAD")
	}
}

func TestSelectedLatestRangeRejectsGaps(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: "head", ShortHash: "head", Subject: "head"},
			{Hash: "middle", ShortHash: "middle", Subject: "middle"},
			{Hash: "older", ShortHash: "older", Subject: "older"},
		},
		selected: map[string]bool{
			"head":  true,
			"older": true,
		},
	}

	if _, err := m.selectedLatestRange(false); err == nil {
		t.Fatal("selectedLatestRange succeeded with a gap")
	}
}

func TestToggleSelectedCommitAllowsUncommittedChanges(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: git.UncommittedHash, ShortHash: "uncommitted", Subject: "dirty"},
			{Hash: "head", ShortHash: "head", Subject: "head"},
		},
		selected:  map[string]bool{},
		mode:      modeSelect,
		commitIdx: 0,
	}

	m.toggleSelectedCommit()

	if !m.selected[git.UncommittedHash] {
		t.Fatal("uncommitted entry was not selected")
	}
}

func TestSelectedLatestRangeAllowsOnlyUncommittedForRemove(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: git.UncommittedHash, ShortHash: "uncommitted", Subject: "dirty"},
			{Hash: "head", ShortHash: "head", Subject: "head"},
		},
		selected: map[string]bool{
			git.UncommittedHash: true,
		},
	}

	got, err := m.selectedLatestRange(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("selectedLatestRange = %+v, want no commits for uncommitted-only selection", got)
	}
	if _, err := m.selectedLatestRange(false); err == nil {
		t.Fatal("selectedLatestRange allowed uncommitted selection for merge")
	}
}
