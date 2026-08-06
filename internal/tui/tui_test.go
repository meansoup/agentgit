package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/transcript"
)

func TestStyleDiffLineStripsNestedANSIWhenRenderingBackground(t *testing.T) {
	line := "+\x1b[31madded\x1b[0m"

	rendered := styleDiffLine(line, 10)

	if strings.Contains(rendered, "\x1b[31m") {
		t.Fatalf("rendered line retained nested syntax ANSI: %q", rendered)
	}
	if got, want := ansi.Strip(rendered), "added     "; got != want {
		t.Fatalf("ansi-stripped rendered line = %q, want %q", got, want)
	}
}

func TestRenderVisibleDiffLineHidesChangeMarkers(t *testing.T) {
	for _, tc := range []struct {
		line string
		want string
	}{
		{line: "-old", want: "old       "},
		{line: "+new", want: "new       "},
		{line: "--- a/file.go", want: "--- a/f..."},
		{line: "+++ b/file.go", want: "+++ b/f..."},
	} {
		got := ansi.Strip(renderVisibleDiffLine(tc.line, 10, false))
		if got != tc.want {
			t.Fatalf("renderVisibleDiffLine(%q) = %q, want %q", tc.line, got, tc.want)
		}
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

func TestLineNumberShortcutTogglesInDiffAndFullFile(t *testing.T) {
	for _, mode := range []mode{modeDiff, modeFullFile} {
		m := model{mode: mode}

		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		got := updated.(model)
		if !got.lineNums {
			t.Fatalf("mode %v lineNums = false after l", mode)
		}

		updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		if updated.(model).lineNums {
			t.Fatalf("mode %v lineNums = true after second l", mode)
		}
	}
}

func TestWrapShortcutTogglesAndUsesUnifiedDiff(t *testing.T) {
	for _, mode := range []mode{modeCommits, modeRequest, modeDiff, modeFullFile} {
		m := model{mode: mode, diffMode: diffSplit, scroll: 5}

		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
		got := updated.(model)

		if !got.wrapLines {
			t.Fatalf("mode %v wrapLines = false after w", mode)
		}
		if got.scroll != 0 {
			t.Fatalf("mode %v scroll = %d, want 0", mode, got.scroll)
		}
		if mode == modeDiff && got.diffMode != diffUnified {
			t.Fatalf("diff mode = %v, want unified while wrapping", got.diffMode)
		}
	}
}

func TestGitShortcutSequencesStartCommands(t *testing.T) {
	m := model{mode: modeCommits, root: t.TempDir()}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("g returned a command, want pending shortcut only")
	}
	if got.gitShortcut != "g" {
		t.Fatalf("gitShortcut = %q, want g", got.gitShortcut)
	}

	updated, cmd = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	got = updated.(model)
	if cmd == nil {
		t.Fatal("g p returned nil command, want git push command")
	}
	if got.gitShortcut != "" {
		t.Fatalf("gitShortcut = %q, want cleared", got.gitShortcut)
	}

	m = model{mode: modeCommits, root: t.TempDir()}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	updated, cmd = updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	got = updated.(model)
	if cmd != nil {
		t.Fatal("g b returned a command, want pending shortcut only")
	}
	if got.gitShortcut != "gb" {
		t.Fatalf("gitShortcut = %q, want gb", got.gitShortcut)
	}

	updated, cmd = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got = updated.(model)
	if cmd == nil {
		t.Fatal("g b d returned nil command, want delete merged branches command")
	}
	if got.gitShortcut != "" {
		t.Fatalf("gitShortcut = %q, want cleared", got.gitShortcut)
	}
}

func TestHardwrapLinePreservesCompleteContent(t *testing.T) {
	line := "abcdefghijklmnopqrstuvwxyz"

	got := hardwrapLine(line, 8)

	if strings.Join(got, "") != line {
		t.Fatalf("wrapped content = %q, want %q", strings.Join(got, ""), line)
	}
	for _, part := range got {
		if width := ansi.StringWidth(part); width > 8 {
			t.Fatalf("wrapped part width = %d, want <= 8: %q", width, part)
		}
		if strings.Contains(part, "...") {
			t.Fatalf("wrapped part contains truncation marker: %q", part)
		}
	}
}

func TestWrappedDiffPreservesLongCodeLine(t *testing.T) {
	m := model{
		commits:   []git.Commit{{Hash: "abc", ShortHash: "abc"}},
		files:     []string{"file.go"},
		diffLines: []string{"@@ -1 +1 @@", "+abcdefghijklmnopqrstuvwxyz"},
		mode:      modeDiff,
		wrapLines: true,
		width:     10,
	}

	got := ansi.Strip(m.viewDiff())
	joined := strings.ReplaceAll(got, "\n", "")

	if !strings.Contains(joined, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("wrapped diff lost code content:\n%s", got)
	}
	if strings.Contains(got, "...") {
		t.Fatalf("wrapped diff contains truncation marker:\n%s", got)
	}
}

func TestWrappedRequestPreservesLongLine(t *testing.T) {
	message := "abcdefghijklmnopqrstuvwxyz"
	m := model{
		requests:  []transcript.Request{{Agent: "codex", Model: "gpt", Message: message}},
		mode:      modeRequest,
		wrapLines: true,
		width:     10,
	}

	got := ansi.Strip(m.viewRequestFull())
	joined := strings.ReplaceAll(got, "\n", "")

	if !strings.Contains(joined, message) {
		t.Fatalf("wrapped request lost content:\n%s", got)
	}
	if strings.Contains(got, "...") {
		t.Fatalf("wrapped request contains truncation marker:\n%s", got)
	}
}

func TestWrappedFullFileScrollsByScreenRow(t *testing.T) {
	m := model{
		commits:   []git.Commit{{Hash: "abc", ShortHash: "abc"}},
		files:     []string{"file.go"},
		fullLines: []string{"abcdefghijkl"},
		mode:      modeFullFile,
		wrapLines: true,
		scroll:    1,
		width:     8,
	}

	got := ansi.Strip(m.viewFullFile())

	if !strings.Contains(got, "ijkl") {
		t.Fatalf("wrapped full file did not scroll into continuation row:\n%s", got)
	}
	if strings.Contains(got, "abcdefgh") {
		t.Fatalf("wrapped full file retained row before scroll:\n%s", got)
	}
}

func TestNumberUnifiedDiffLinesUsesOldAndNewLineNumbers(t *testing.T) {
	lines := []string{
		"@@ -10,2 +20,3 @@",
		" context",
		"-old",
		"+new",
		"+added",
	}

	got := numberUnifiedDiffLines(lines, 80, false)
	stripped := make([]string, len(got))
	for i, line := range got {
		stripped[i] = ansi.Strip(line)
	}

	for i, want := range []string{
		"@@ -10,2 +20,3 @@",
		"10 20 │  context",
		"11    │ old",
		"   21 │ new",
		"   22 │ added",
	} {
		if !strings.Contains(stripped[i], want) {
			t.Fatalf("numbered line %d = %q, want %q", i, stripped[i], want)
		}
	}
}

func TestSplitDiffLineNumbersTrackBothSides(t *testing.T) {
	lines := []string{
		"@@ -3,2 +8,2 @@",
		" same",
		"-old",
		"+new",
	}

	got := splitDiffWithLineNumbers(lines, 31, true)
	stripped := ansi.Strip(strings.Join(got, "\n"))
	for _, want := range []string{"3 same", "8 same", "4 old", "9 new"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("numbered split diff missing %q:\n%s", want, stripped)
		}
	}
}

func TestSplitDiffLineNumbersFitNarrowWidths(t *testing.T) {
	lines := []string{
		"@@ -100,1 +200,1 @@",
		"-old",
		"+new",
	}
	for _, width := range []int{3, 4, 8, 16} {
		for _, line := range splitDiffWithLineNumbers(lines, width, true) {
			if visible := ansi.StringWidth(ansi.Strip(line)); visible > width {
				t.Fatalf("numbered split width %d rendered %d columns: %q", width, visible, line)
			}
		}
	}
}

func TestViewFullFileNumbersScrolledLines(t *testing.T) {
	m := model{
		commits:   []git.Commit{{Hash: "abc", ShortHash: "abc"}},
		files:     []string{"file.go"},
		fullLines: []string{"one", "two", "three"},
		mode:      modeFullFile,
		lineNums:  true,
		scroll:    1,
		width:     80,
	}

	got := ansi.Strip(m.viewFullFile())
	for _, want := range []string{"2 │ two", "3 │ three"} {
		if !strings.Contains(got, want) {
			t.Fatalf("full file view missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "1 │ one") {
		t.Fatalf("full file view included scrolled line:\n%s", got)
	}
}

func TestRequestPreviewMessageUsesFirstNonEmptyLine(t *testing.T) {
	message := "\n\tOpen @lib/main.go\n\npackage main\nfunc main() {}\n"

	got := requestPreviewMessage(message)
	if want := "Open @lib/main.go"; got != want {
		t.Fatalf("requestPreviewMessage() = %q, want %q", got, want)
	}
}

func TestRequestListLineIsSingleLine(t *testing.T) {
	m := model{}
	req := transcript.Request{
		Agent:     "gemini",
		Model:     "gemini-2.5-pro",
		Message:   "Review @internal/tui/tui.go\n\nfull file content\nmore content",
		Timestamp: "2026-06-25T13:14:15Z",
	}

	got := ansi.Strip(m.requestListLine(req))
	if strings.Contains(got, "\n") {
		t.Fatalf("requestListLine contained newline: %q", got)
	}
	if strings.Contains(got, "full file content") || strings.Contains(got, "more content") {
		t.Fatalf("requestListLine leaked multiline request content: %q", got)
	}
	if !strings.Contains(got, "Review @internal/tui/tui.go") || !strings.Contains(got, "[gemini gemini-2.5-pro]") {
		t.Fatalf("requestListLine missing expected preview details: %q", got)
	}
}

func TestHighlightDiffPreservesLineCountOrderAndMetadata(t *testing.T) {
	lines := []string{
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,3 +1,4 @@",
		" package main",
		"-func old() {}",
		"+func new() {}",
		`\ No newline at end of file`,
	}

	got := highlightDiff("main.go", lines)

	if len(got) != len(lines) {
		t.Fatalf("highlightDiff len = %d, want %d", len(got), len(lines))
	}
	for _, index := range []int{0, 1, 2, 6} {
		if got[index] != lines[index] {
			t.Fatalf("metadata line %d changed:\n got %q\nwant %q", index, got[index], lines[index])
		}
	}
	for _, index := range []int{3, 4, 5} {
		stripped := ansi.Strip(got[index])
		if stripped != lines[index] {
			t.Fatalf("code line %d stripped = %q, want %q", index, stripped, lines[index])
		}
	}
}

func TestRenderedFileCachesAreBoundedLRU(t *testing.T) {
	m := model{}
	for i := 0; i < renderedFileCacheLimit+5; i++ {
		key := fmt.Sprintf("diff-%02d", i)
		m.storeDiffCache(key, []string{key})
		m.storeFullCache(key, []string{key})
	}

	if len(m.diffCache) != renderedFileCacheLimit || len(m.diffCacheKeys) != renderedFileCacheLimit {
		t.Fatalf("diff cache size = %d/%d, want %d", len(m.diffCache), len(m.diffCacheKeys), renderedFileCacheLimit)
	}
	if len(m.fullCache) != renderedFileCacheLimit || len(m.fullCacheKeys) != renderedFileCacheLimit {
		t.Fatalf("full cache size = %d/%d, want %d", len(m.fullCache), len(m.fullCacheKeys), renderedFileCacheLimit)
	}
	if _, ok := m.diffCache["diff-00"]; ok {
		t.Fatal("oldest diff cache entry was retained")
	}
	if _, ok := m.fullCache["diff-00"]; ok {
		t.Fatal("oldest full cache entry was retained")
	}
	if got := m.diffCacheKeys[len(m.diffCacheKeys)-1]; got != "diff-54" {
		t.Fatalf("newest diff cache key = %q, want diff-54", got)
	}
	if got := m.fullCacheKeys[len(m.fullCacheKeys)-1]; got != "diff-54" {
		t.Fatalf("newest full cache key = %q, want diff-54", got)
	}
}

func TestStatusBarShowsRepoContext(t *testing.T) {
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

	status := ansi.Strip(m.viewStatusBar())

	for _, want := range []string{"commits", "main", "69e67e5", "dirty 2", "Ctrl+P", "Alt+/"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q:\n%s", want, status)
		}
	}
	if got := lipgloss.Height(m.viewStatusBar()); got != 1 {
		t.Fatalf("status height = %d, want 1", got)
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

	got, err := m.selectedLatestRange()
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

	if _, err := m.selectedLatestRange(); err == nil {
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

	if _, err := m.selectedLatestRange(); err == nil {
		t.Fatal("selectedLatestRange succeeded with a gap")
	}
}

func TestToggleSelectedCommitIgnoresUncommittedChanges(t *testing.T) {
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

	if m.selected[git.UncommittedHash] {
		t.Fatal("uncommitted entry was selected")
	}
	if !strings.Contains(m.notice, "uncommitted") {
		t.Fatalf("notice = %q, want uncommitted warning", m.notice)
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

func TestHelpDialogIncludesSearchShortcuts(t *testing.T) {
	m := model{
		mode:   modeFullFile,
		files:  []string{"README.md"},
		width:  120,
		height: 34,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	view := ansi.Strip(updated.(model).View())

	for _, want := range []string{"ctrl+p", "Files", "alt+/", "Grep", "/", "Find", "ctrl+f"} {
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

func TestStatusBarIncludesPersistentCommands(t *testing.T) {
	m := model{
		mode:  modeFiles,
		width: 120,
	}

	status := ansi.Strip(m.viewStatusBar())

	for _, command := range []string{"Ctrl+P", "Alt+/", "?"} {
		if !strings.Contains(status, command) {
			t.Fatalf("status missing command %q:\n%s", command, status)
		}
	}
	for _, removed := range []string{"up/down", "enter/right"} {
		if strings.Contains(status, removed) {
			t.Fatalf("status retained verbose shortcut %q:\n%s", removed, status)
		}
	}
}

func TestStatusBarKeepsFixedDimensionsAcrossViews(t *testing.T) {
	for _, width := range []int{24, 72, 140} {
		for _, mode := range []mode{
			modeCommits,
			modeDirectories,
			modeFiles,
			modeDiff,
			modeFullFile,
			modeRequest,
		} {
			m := model{
				root:   "/Users/example/develop/git/agentgit",
				branch: "main",
				head:   "abcdef123456",
				commits: []git.Commit{
					{Hash: "abc", ShortHash: "abc", Subject: "a long selected item that changes by view"},
				},
				files: []string{"internal/tui/tui.go"},
				mode:  mode,
				width: width,
			}
			m.dirEntries = []directoryEntry{{Path: "internal", DisplayName: "internal", IsDir: true}}

			status := m.viewStatusBar()
			if got := lipgloss.Height(status); got != 1 {
				t.Fatalf("width %d mode %v status height = %d, want 1:\n%s", width, mode, got, ansi.Strip(status))
			}
			if got := ansi.StringWidth(ansi.Strip(status)); got != m.width {
				t.Fatalf("width %d mode %v status width = %d, want %d: %q", width, mode, got, m.width, ansi.Strip(status))
			}
		}
	}
}

func TestStatusBarIncludesViewTargetAndCommands(t *testing.T) {
	m := model{
		root:   "/Users/example/develop/git/agentgit",
		branch: "feature/header",
		head:   "abcdef123456",
		commits: []git.Commit{
			{Hash: "abc", ShortHash: "abc", Subject: "selected commit"},
		},
		mode:  modeCommits,
		width: 100,
	}

	status := ansi.Strip(m.viewStatusBar())

	for _, want := range []string{"commits", "feature/header", "Ctrl+P"} {
		if !strings.Contains(status, want) {
			t.Fatalf("status missing %q:\n%s", want, status)
		}
	}
}

func TestCtrlPOpensFuzzyFileSearch(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "internal/tui/tui.go", "package tui\n")
	writeTUITestFile(t, root, "internal/hooks/hooks.go", "package hooks\n")
	writeTUITestFile(t, root, "README.md", "readme\n")
	m := model{
		root:   root,
		mode:   modeCommits,
		width:  100,
		height: 24,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	got := updated.(model)

	if !got.searchOpen {
		t.Fatal("searchOpen = false after ctrl+p")
	}
	if len(got.searchResults) != 3 {
		t.Fatalf("initial search results = %d, want 3", len(got.searchResults))
	}
	status := ansi.Strip(got.viewStatusBar())
	for _, want := range []string{"search", "query type", "Enter"} {
		if !strings.Contains(status, want) {
			t.Fatalf("search status missing %q:\n%s", want, status)
		}
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("tui")})
	got = updated.(model)
	if got.searchText != "tui" {
		t.Fatalf("searchText = %q, want tui", got.searchText)
	}
	if len(got.searchResults) == 0 || got.searchResults[0].Path != "internal/tui/tui.go" {
		t.Fatalf("typed search results = %+v", got.searchResults)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got = updated.(model)
	if got.searchText != "tu" {
		t.Fatalf("searchText after Backspace = %q, want tu", got.searchText)
	}
}

func TestCtrlEOpensRecentFilesSearch(t *testing.T) {
	m := model{
		mode: modeCommits,
		recentFiles: []recentFile{
			{Path: "internal/tui/tui.go", Worktree: true},
			{Path: "README.md", CommitHash: "abcdef123456", Worktree: false},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	got := updated.(model)

	if !got.searchOpen {
		t.Fatal("searchOpen = false after ctrl+e")
	}
	if got.searchKind != searchKindRecentFiles {
		t.Fatalf("searchKind = %v, want recent files", got.searchKind)
	}
	if len(got.searchResults) != 2 {
		t.Fatalf("recent search results = %+v, want two entries", got.searchResults)
	}
	if got.searchResults[0].Path != "internal/tui/tui.go" {
		t.Fatalf("first recent result = %+v, want newest file first", got.searchResults[0])
	}
}

func TestCtrlEWithoutRecentFilesShowsNotice(t *testing.T) {
	m := model{mode: modeCommits}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	got := updated.(model)

	if got.searchOpen {
		t.Fatal("search opened without recent files")
	}
	if got.notice != "no recent files" {
		t.Fatalf("notice = %q, want no recent files", got.notice)
	}
}

func TestOpeningWorktreeFileAddsRecentFile(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "current.txt", "current content\n")
	m := model{
		root:         root,
		commits:      []git.Commit{{Hash: "abc", ShortHash: "abc"}},
		files:        []string{"current.txt"},
		mode:         modeFiles,
		fileReturn:   modeDirectories,
		worktreeFile: true,
	}

	m.enter(false)

	if m.mode != modeFullFile {
		t.Fatalf("mode = %v, want full file", m.mode)
	}
	if len(m.recentFiles) != 1 || m.recentFiles[0].Path != "current.txt" || !m.recentFiles[0].Worktree {
		t.Fatalf("recentFiles = %+v, want current worktree file", m.recentFiles)
	}
}

func TestRecentFileSearchOpensWorktreeFile(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "current.txt", "current content\n")
	m := model{
		root: root,
		mode: modeCommits,
		recentFiles: []recentFile{
			{Path: "current.txt", Worktree: true},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	got := updated.(model)
	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(model)

	if got.mode != modeFullFile || !got.worktreeFile {
		t.Fatalf("mode/worktree = %v/%v, want worktree full file", got.mode, got.worktreeFile)
	}
	if content := ansi.Strip(strings.Join(got.fullLines, "\n")); !strings.Contains(content, "current content") {
		t.Fatalf("fullLines = %q, want current content", content)
	}
}

func TestFuzzyFileSearchRanksConsecutiveBasenameMatches(t *testing.T) {
	files := []string{
		"docs/t-u-i.md",
		"internal/hooks/tiny_util.go",
		"internal/tui/tui.go",
	}

	results := fuzzyFileMatches(files, "tui")

	if len(results) != 3 {
		t.Fatalf("fuzzy results = %d, want 3", len(results))
	}
	if results[0].Path != "internal/tui/tui.go" {
		t.Fatalf("top fuzzy result = %q, want internal/tui/tui.go", results[0].Path)
	}
}

func TestFuzzyPathMatchUsesBestConnectedBasenameAlignment(t *testing.T) {
	positions, _, ok := fuzzyPathMatch("ab/src/abc.go", "abc")
	if !ok {
		t.Fatal("fuzzyPathMatch did not match abc")
	}

	got := strings.TrimSpace(fmt.Sprint(positions))
	if got != "[7 8 9]" {
		t.Fatalf("positions = %s, want connected basename match [7 8 9]", got)
	}
}

func TestFuzzyFileSearchRanksFilenameMatchesAboveDirectoryMatches(t *testing.T) {
	files := []string{
		"search/internal/runner.go",
		"cmd/search.go",
		"internal/runner_search.go",
	}

	results := fuzzyFileMatches(files, "search")

	if len(results) != 3 {
		t.Fatalf("fuzzy results = %d, want 3", len(results))
	}
	if results[0].Path != "cmd/search.go" {
		t.Fatalf("top fuzzy result = %q, want filename exact match", results[0].Path)
	}
	if results[2].Path != "search/internal/runner.go" {
		t.Fatalf("last fuzzy result = %q, want directory-only match last", results[2].Path)
	}
}

func TestSearchEnterRevealsFileInDirectoryView(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "internal/tui/tui.go", "package tui\n")
	writeTUITestFile(t, root, "internal/hooks/hooks.go", "package hooks\n")
	m := model{
		root: root,
		mode: modeCommits,
	}
	m.openSearch()
	m.searchText = "tuigo"
	m.updateSearchResults()
	if len(m.searchResults) == 0 || m.searchResults[0].Path != "internal/tui/tui.go" {
		t.Fatalf("unexpected fuzzy results: %+v", m.searchResults)
	}

	updated, _ := m.updateSearch(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	entries := got.visibleDirectoryEntries()

	if got.searchOpen {
		t.Fatal("search remained open after Enter")
	}
	if got.mode != modeDirectories {
		t.Fatalf("mode = %v, want modeDirectories", got.mode)
	}
	if !got.expanded["internal"] || !got.expanded["internal/tui"] {
		t.Fatalf("file ancestors were not expanded: %+v", got.expanded)
	}
	if len(entries) == 0 || entries[got.dirIdx].Path != "internal/tui/tui.go" {
		t.Fatalf("selected directory entry = %+v", entries)
	}
}

func TestSearchEscapeReturnsToPreviousView(t *testing.T) {
	m := model{
		mode:          modeDiff,
		searchOpen:    true,
		searchText:    "tui",
		searchFiles:   []string{"internal/tui/tui.go"},
		searchResults: fuzzyFileMatches([]string{"internal/tui/tui.go"}, "tui"),
	}

	updated, _ := m.updateSearch(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(model)

	if got.searchOpen {
		t.Fatal("search remained open after Esc")
	}
	if got.mode != modeDiff {
		t.Fatalf("mode = %v, want previous modeDiff", got.mode)
	}
}

func TestSlashSearchesWithinCurrentFileAndJumpsToMatch(t *testing.T) {
	m := model{
		mode:      modeFullFile,
		files:     []string{"internal/tui/tui.go"},
		fileIdx:   0,
		fullLines: []string{"package tui", "func first() {}", "const needle = true"},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	got := updated.(model)
	if !got.searchOpen {
		t.Fatal("searchOpen = false after /")
	}
	if got.searchKind != searchKindCurrentFile {
		t.Fatalf("searchKind = %v, want current file", got.searchKind)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("needle")})
	got = updated.(model)
	if len(got.searchResults) != 1 {
		t.Fatalf("search results = %+v, want one match", got.searchResults)
	}
	if got.searchResults[0].Line != 3 {
		t.Fatalf("match line = %d, want 3", got.searchResults[0].Line)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(model)
	if got.searchOpen {
		t.Fatal("search remained open after Enter")
	}
	if got.scroll != 2 {
		t.Fatalf("scroll = %d, want 2", got.scroll)
	}
}

func TestSearchOverlayKeepsCurrentFileVisible(t *testing.T) {
	m := model{
		mode:         modeFullFile,
		files:        []string{"README.md"},
		fileIdx:      0,
		fullLines:    []string{"package tui", "const needle = true", "after"},
		worktreeFile: true,
		width:        90,
		height:       24,
	}
	m.openCurrentFileSearch()
	m.searchText = "needle"
	m.updateSearchResults()

	view := ansi.Strip(m.View())

	for _, want := range []string{"/ needle", "matches 1", "README.md", "package tui", "const needle = true"} {
		if !strings.Contains(view, want) {
			t.Fatalf("search overlay view missing %q:\n%s", want, view)
		}
	}
}

func TestCurrentFileSearchExposesMatchPositionsForHighlighting(t *testing.T) {
	m := model{
		mode:         modeFullFile,
		files:        []string{"README.md"},
		fileIdx:      0,
		fullLines:    []string{"const needle = true"},
		width:        80,
		worktreeFile: true,
		searchOpen:   true,
		searchKind:   searchKindCurrentFile,
		searchText:   "needle",
		searchResults: []fileSearchResult{
			{Path: "README.md", Line: 1, Text: "const needle = true", Positions: contiguousPositions(6, 6)},
		},
	}

	positions := m.currentFileSearchPositions(1)
	line := m.highlightCurrentFileSearchLine(m.fullLines[0], 1)

	if len(positions) != 6 || positions[0] != 6 {
		t.Fatalf("positions = %+v, want needle positions", positions)
	}
	if ansi.Strip(line) != "const needle = true" {
		t.Fatalf("highlight changed visible text: %q", ansi.Strip(line))
	}
}

func TestSearchResultHighlightPositionsPreserveLeadingSpaces(t *testing.T) {
	text := trimSearchResultText("    needle value   ")
	positions := contiguousPositions(4, 6)

	if text != "    needle value" {
		t.Fatalf("text = %q, want leading spaces preserved", text)
	}
	if !positionsMatchQuery(text, positions, "needle") {
		t.Fatalf("positions %+v do not match %q", positions, text)
	}
}

func TestCurrentFileSearchResultShowsOnlyLineNumber(t *testing.T) {
	m := model{
		searchKind: searchKindCurrentFile,
	}
	result := fileSearchResult{
		Path: "internal/tui/tui.go",
		Line: 42,
		Text: "return needle value",
	}

	line := ansi.Strip(m.renderSearchResult(result))

	for _, want := range []string{"line 42", "return needle value"} {
		if !strings.Contains(line, want) {
			t.Fatalf("result missing %q: %q", want, line)
		}
	}
}

func TestAltSlashSearchesWorktreeContentAndOpensMatch(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "alpha.txt", "one\ntwo\n")
	writeTUITestFile(t, root, "src/beta.txt", "before\nneedle here\nafter\n")
	m := model{
		root: root,
		mode: modeCommits,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}, Alt: true})
	got := updated.(model)
	if !got.searchOpen {
		t.Fatal("searchOpen = false after alt+/")
	}
	if got.searchKind != searchKindWorktreeContent {
		t.Fatalf("searchKind = %v, want worktree content", got.searchKind)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("needle")})
	got = updated.(model)
	if len(got.searchResults) != 1 {
		t.Fatalf("search results = %+v, want one match", got.searchResults)
	}
	if got.searchResults[0].Path != "src/beta.txt" || got.searchResults[0].Line != 2 {
		t.Fatalf("match = %+v, want src/beta.txt:2", got.searchResults[0])
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = updated.(model)
	if got.searchOpen {
		t.Fatal("search remained open after Enter")
	}
	if got.mode != modeFullFile || !got.worktreeFile {
		t.Fatalf("mode/worktree = %v/%v, want full file worktree", got.mode, got.worktreeFile)
	}
	if len(got.files) != 1 || got.files[0] != "src/beta.txt" {
		t.Fatalf("opened files = %+v, want src/beta.txt", got.files)
	}
	if got.scroll != 1 {
		t.Fatalf("scroll = %d, want 1", got.scroll)
	}
}

func TestSearchAcceptsSpacesInQuery(t *testing.T) {
	m := model{searchOpen: true}

	updated, _ := m.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	got := updated.(model)
	updated, _ = got.updateSearch(tea.KeyMsg{Type: tea.KeySpace})
	got = updated.(model)
	updated, _ = got.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("world")})
	got = updated.(model)

	if got.searchText != "hello world" {
		t.Fatalf("searchText = %q, want hello world", got.searchText)
	}
}

func TestWorktreeContentSearchResultShowsBasenameAndText(t *testing.T) {
	m := model{
		searchKind: searchKindWorktreeContent,
		width:      90,
	}
	result := fileSearchResult{
		Path:      "internal/tui/tui.go",
		Line:      42,
		Text:      "return needle value",
		Positions: contiguousPositions(7, 6),
	}

	line := ansi.Strip(m.renderSearchResult(result))

	if !strings.Contains(line, "tui.go") {
		t.Fatalf("result missing basename:\n%s", line)
	}
	if strings.Contains(line, "internal/tui/tui.go") {
		t.Fatalf("result includes full path:\n%s", line)
	}
	if !strings.Contains(line, "return needle value") {
		t.Fatalf("result missing matched text:\n%s", line)
	}
}

func TestTabCyclesTopLevelCommitAndDirectoryViews(t *testing.T) {
	m := model{
		root:   newTUITestRepo(t),
		mode:   modeCommits,
		width:  100,
		height: 24,
	}
	writeTUITestFile(t, m.root, "internal/tui/tui.go", "package tui\n")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(model)
	if got.mode != modeDirectories {
		t.Fatalf("first tab mode = %v, want modeDirectories", got.mode)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
	got = updated.(model)
	if got.mode != modeCommits {
		t.Fatalf("second tab mode = %v, want modeCommits", got.mode)
	}
}

func TestRequestDrawerEnterOpensFullRequest(t *testing.T) {
	m := model{
		mode:          modeCommits,
		requestDrawer: true,
		requests: []transcript.Request{
			{ID: "7", Agent: "claude", Model: "sonnet", Message: "full request body", Timestamp: "2026-06-25T00:00:00Z", EditedFiles: []string{"README.md"}},
		},
		width:  80,
		height: 24,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.mode != modeRequest {
		t.Fatalf("mode = %v, want modeRequest", got.mode)
	}
	if got.requestReturn != modeCommits {
		t.Fatalf("requestReturn = %v, want modeCommits", got.requestReturn)
	}
	view := ansi.Strip(got.viewRequestFull())
	for _, want := range []string{"claude", "sonnet", "README.md", "full request body"} {
		if !strings.Contains(view, want) {
			t.Fatalf("request full view missing %q:\n%s", want, view)
		}
	}
}

func TestRequestListRendersBulletTimeAgentAndRequest(t *testing.T) {
	m := model{
		requests: []transcript.Request{
			{
				ID:        "7",
				Agent:     "claude",
				Model:     "sonnet",
				Message:   "full request body",
				Timestamp: "2026-06-25T13:14:15Z",
			},
		},
		width: 120,
	}

	view := ansi.Strip(m.viewRequestsList(120))

	if !strings.Contains(view, "● 06-25 13:14 [claude sonnet] full request body") {
		t.Fatalf("request list row missing expected format:\n%s", view)
	}
}

func TestWrappedCommitListPreservesLongContentAndFocusLine(t *testing.T) {
	m := model{
		mode:      modeCommits,
		wrapLines: true,
		width:     24,
		commits: []git.Commit{
			{Hash: "11111111", ShortHash: "11111111", Date: "06-30 12:34", Subject: "first commit subject is very long"},
			{Hash: "22222222", ShortHash: "22222222", Date: "06-30 12:35", Subject: "second commit subject is also very long"},
		},
		commitIdx: 1,
	}

	view := ansi.Strip(m.viewCommitsList(24))
	joined := strings.ReplaceAll(view, "\n", "")
	if !strings.Contains(joined, "first commit subject is very long") {
		t.Fatalf("wrapped commit list lost first commit subject:\n%s", view)
	}
	if !strings.Contains(joined, "second commit subject is also very long") {
		t.Fatalf("wrapped commit list lost second commit subject:\n%s", view)
	}
	if got := m.commitFocusLine(); got <= 1 {
		t.Fatalf("commitFocusLine = %d, want wrapped offset beyond first row", got)
	}
}

func TestWrappedRequestListPreservesLongContentAndFocusLine(t *testing.T) {
	m := model{
		wrapLines:  true,
		width:      28,
		requestIdx: 1,
		requests: []transcript.Request{
			{
				ID:        "1",
				Agent:     "codex",
				Model:     "gpt-5",
				Message:   "first request body is very long and should wrap cleanly",
				Timestamp: "2026-06-30T12:34:56Z",
			},
			{
				ID:        "2",
				Agent:     "claude",
				Model:     "sonnet",
				Message:   "second request body is also long and should remain visible",
				Timestamp: "2026-06-30T12:35:56Z",
			},
		},
	}

	view := ansi.Strip(m.viewRequestsList(28))
	joined := strings.ReplaceAll(view, "\n", "")
	if !strings.Contains(joined, "first request body is very long and should wrap cleanly") {
		t.Fatalf("wrapped request list lost first request body:\n%s", view)
	}
	if !strings.Contains(joined, "second request body is also long and should remain visible") {
		t.Fatalf("wrapped request list lost second request body:\n%s", view)
	}
	if got := m.requestFocusLine(); got <= 1 {
		t.Fatalf("requestFocusLine = %d, want wrapped offset beyond first row", got)
	}
}

func TestDirectoryListUsesColorsInsteadOfKindPrefixes(t *testing.T) {
	m := model{
		mode: modeDirectories,
		expanded: map[string]bool{
			"internal": true,
		},
		dirEntries: []directoryEntry{
			{Path: "README.md", DisplayName: "README.md"},
			{Path: "internal", DisplayName: "internal", IsDir: true, FileCount: 2},
			{Path: "internal/tui.go", DisplayName: "tui.go", Depth: 1},
		},
	}

	view := ansi.Strip(m.viewDirectoryList(80))

	if strings.Contains(view, "[f]") || strings.Contains(view, "[+]") || strings.Contains(view, "[-]") {
		t.Fatalf("directory list still contains kind prefixes:\n%s", view)
	}
	if !strings.Contains(view, "internal/  2 files") {
		t.Fatalf("directory list missing trailing slash directory marker:\n%s", view)
	}
	if !strings.Contains(view, "README.md") || !strings.Contains(view, "  tui.go") {
		t.Fatalf("directory list missing file entries:\n%s", view)
	}
}

func TestSearchBodyRendersOnlyVisibleResults(t *testing.T) {
	results := make([]fileSearchResult, 1000)
	for i := range results {
		results[i] = fileSearchResult{Path: fmt.Sprintf("files/file-%04d.go", i)}
	}
	m := model{
		searchOpen:    true,
		searchIdx:     500,
		searchResults: results,
		width:         60,
	}

	body := ansi.Strip(m.viewSearchBody(7))

	if got := lipgloss.Height(body); got != 7 {
		t.Fatalf("search body height = %d, want 7", got)
	}
	if !strings.Contains(body, "file-0500.go") {
		t.Fatalf("search body does not contain selected result:\n%s", body)
	}
	if strings.Contains(body, "file-0000.go") || strings.Contains(body, "file-0999.go") {
		t.Fatalf("search body rendered offscreen results:\n%s", body)
	}
}

func TestSearchBodyUsesAvailableWidth(t *testing.T) {
	m := model{
		searchOpen: true,
		width:      160,
	}

	body := ansi.Strip(m.viewSearchBody(7))
	lines := strings.Split(body, "\n")

	if len(lines) == 0 {
		t.Fatal("search body rendered no lines")
	}
	if got, want := ansi.StringWidth(lines[0]), m.frameInnerWidth(); got != want {
		t.Fatalf("search body width = %d, want %d:\n%s", got, want, body)
	}
}

func TestSearchViewShowsCenteredDialogWithinFrame(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "internal/tui/tui.go", "package tui\n")
	writeTUITestFile(t, root, "internal/hooks/hooks.go", "package hooks\n")
	m := model{
		root:   root,
		mode:   modeCommits,
		width:  80,
		height: 20,
	}
	m.openSearch()
	m.searchText = "tui"
	m.updateSearchResults()

	view := ansi.Strip(m.View())

	for _, want := range []string{"/ tui", "matches", "internal/tui/tui.go"} {
		if !strings.Contains(view, want) {
			t.Fatalf("search view missing %q:\n%s", want, view)
		}
	}
}

func TestFrameIncludesBottomStatusBar(t *testing.T) {
	m := model{
		commits: []git.Commit{{Hash: "abc", ShortHash: "abc"}},
		files:   []string{"file.go"},
		diffLines: []string{
			"@@ -1 +1 @@",
			"-old",
			"+new",
		},
		mode:   modeDiff,
		width:  80,
		height: 12,
	}

	view := m.View()
	if got := lipgloss.Height(view); got != m.height {
		t.Fatalf("frame height = %d, want %d", got, m.height)
	}
	if !strings.Contains(ansi.Strip(view), "Full") {
		t.Fatalf("frame missing status commands:\n%s", ansi.Strip(view))
	}
}

func TestRepositoryContextLineCompactsLongPath(t *testing.T) {
	m := model{
		root:   "/very/long/path/that/will/not/fit/inside/the/context/window/agentgit",
		branch: "main",
		head:   "abcdef123456",
	}

	line := m.repositoryContextLine(48)

	if width := lipgloss.Width(line); width > 14 {
		t.Fatalf("repository context width = %d, want <= 14: %q", width, line)
	}
	if !strings.Contains(line, "...") || !strings.Contains(line, "agentgit") {
		t.Fatalf("repository context did not compact path usefully: %q", line)
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

func TestViewFilesBodyRendersOnlyVisibleWindow(t *testing.T) {
	var files []string
	for i := 0; i < 1000; i++ {
		files = append(files, fmt.Sprintf("file-%04d-%s", i, strings.Repeat("x", 40)))
	}
	m := model{
		commits: []git.Commit{{Hash: "abc", ShortHash: "abc", Subject: "test"}},
		files:   files,
		fileIdx: 500,
		width:   32,
	}

	body := m.viewFilesBody(7)
	lines := strings.Split(body, "\n")

	if len(lines) != 7 {
		t.Fatalf("rendered %d lines, want 7", len(lines))
	}
	if strings.Contains(body, "file-0000") || !strings.Contains(body, "file-0500") {
		t.Fatalf("file body rendered entries outside the visible window")
	}
	for _, line := range lines {
		if !strings.Contains(line, "\x1b[K") {
			t.Fatalf("line missing clear-to-end sequence: %q", line)
		}
		if width := ansi.StringWidth(ansi.Strip(line)); width != 30 {
			t.Fatalf("line width = %d, want 30: %q", width, line)
		}
	}
}

func TestFrameLineClearsAndPadsLongLines(t *testing.T) {
	got := frameLine("abcdefghijklmnopqrstuvwxyz", 10)

	if !strings.Contains(got, "\x1b[K") {
		t.Fatalf("frameLine missing clear-to-end sequence: %q", got)
	}
	if width := ansi.StringWidth(ansi.Strip(got)); width != 10 {
		t.Fatalf("frameLine width = %d, want 10: %q", width, got)
	}
}

func TestFilesViewMarksFileStatuses(t *testing.T) {
	m := model{
		commits: []git.Commit{{Hash: "abc", ShortHash: "abc"}},
		files:   []string{"created.go", "updated.go", "deleted.go"},
		fileStatusCache: map[string]map[string]string{
			"abc": {
				"created.go": "created",
				"updated.go": "updated",
				"deleted.go": "deleted",
			},
		},
		mode:  modeFiles,
		width: 80,
	}

	view := ansi.Strip(m.viewFilesList(80))

	for _, want := range []string{"created.go  created", "updated.go  updated", "deleted.go  deleted"} {
		if !strings.Contains(view, want) {
			t.Fatalf("files view missing %q:\n%s", want, view)
		}
	}
}

func TestPanelTitleShowsSelectedFileStatus(t *testing.T) {
	m := model{
		commits: []git.Commit{{Hash: "abc", ShortHash: "abc"}},
		files:   []string{"updated.go"},
		fileStatusCache: map[string]map[string]string{
			"abc": {"updated.go": "updated"},
		},
		mode:  modeFiles,
		width: 80,
	}

	title := ansi.Strip(m.panelTitle())

	for _, want := range []string{"Files", "updated.go", "updated"} {
		if !strings.Contains(title, want) {
			t.Fatalf("panel title missing %q: %q", want, title)
		}
	}
}

func TestFilesModeViewUsesBorderSummaryInsteadOfPreview(t *testing.T) {
	m := model{
		commits: []git.Commit{{Hash: "abc", ShortHash: "abc"}},
		files:   []string{"updated.go"},
		fileStatusCache: map[string]map[string]string{
			"abc": {"updated.go": "updated"},
		},
		mode:   modeFiles,
		width:  100,
		height: 20,
	}

	view := ansi.Strip(m.View())

	for _, want := range []string{"Files", "updated.go", "updated"} {
		if !strings.Contains(view, want) {
			t.Fatalf("files mode view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "File Preview") || strings.Contains(view, "press Enter/Right for diff") {
		t.Fatalf("files mode view still includes preview block:\n%s", view)
	}
}

func TestRenderFileLabelShowsRenamePath(t *testing.T) {
	m := model{
		fileStatusCache: map[string]map[string]string{
			"abc": {"new.txt": "renamed"},
		},
		fileChangeCache: map[string]map[string]git.FileChange{
			"abc": {"new.txt": {Path: "new.txt", OldPath: "old.txt", Status: "renamed"}},
		},
	}

	label := ansi.Strip(m.renderFileLabel("new.txt", "abc", 80, false))

	if !strings.Contains(label, "old.txt -> new.txt") {
		t.Fatalf("rename label = %q, want old and new path", label)
	}
	if !strings.Contains(label, "renamed") {
		t.Fatalf("rename label = %q, want renamed status", label)
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

func TestLoadDirectoryEntriesUsesCurrentWorktreeFiles(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "internal/tui/tui.go", "package tui\n")
	writeTUITestFile(t, root, "README.md", "readme\n")
	writeTUITestFile(t, root, "deleted.txt", "deleted\n")
	runTUITestGit(t, root, "add", ".")
	runTUITestGit(t, root, "commit", "-m", "current files")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeTUITestFile(t, root, "internal/hooks/hooks.go", "package hooks\n")
	writeTUITestFile(t, root, "ignored.log", "ignored\n")
	writeTUITestFile(t, root, ".gitignore", "*.log\n")

	m := model{
		root: root,
		commits: []git.Commit{
			{Hash: "c1", ShortHash: "c1", Subject: "newest"},
			{Hash: "c2", ShortHash: "c2", Subject: "older"},
		},
		fileCache: map[string][]string{
			"c1": {"internal/tui/tui.go", "README.md"},
			"c2": {"internal/hooks/hooks.go", "deleted.txt"},
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
	if _, ok := entries["ignored.log"]; ok {
		t.Fatal("ignored file was included in directory entries")
	}
	if _, ok := entries["deleted.txt"]; ok {
		t.Fatal("deleted tracked file was included in directory entries")
	}
}

func TestLoadDirectoryEntriesFollowsRenamedFileHistory(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "old.txt", "base\n")
	runTUITestGit(t, root, "add", "old.txt")
	runTUITestGit(t, root, "commit", "-m", "initial")
	writeTUITestFile(t, root, "old.txt", "changed\n")
	runTUITestGit(t, root, "add", "old.txt")
	runTUITestGit(t, root, "commit", "-m", "change old")
	runTUITestGit(t, root, "mv", "old.txt", "new.txt")
	runTUITestGit(t, root, "commit", "-m", "rename")
	commits, err := git.CommitsWithUncommitted(root, 10)
	if err != nil {
		t.Fatal(err)
	}
	m := model{
		root:            root,
		commits:         commits,
		fileCache:       map[string][]string{},
		fileStatusCache: map[string]map[string]string{},
		fileChangeCache: map[string]map[string]git.FileChange{},
	}

	m.loadDirectoryEntries()

	var newEntry directoryEntry
	for _, entry := range m.dirEntries {
		if entry.Path == "old.txt" {
			t.Fatal("directory entries included old rename path")
		}
		if entry.Path == "new.txt" {
			newEntry = entry
		}
	}
	if newEntry.Path == "" {
		t.Fatalf("directory entries missing new.txt: %+v", m.dirEntries)
	}
	if len(newEntry.CommitIndexes) != len(commits) {
		t.Fatalf("new.txt commit indexes = %v, want all %d commits", newEntry.CommitIndexes, len(commits))
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

func TestLeftCollapsesSelectedDirectory(t *testing.T) {
	m := model{
		commits: []git.Commit{{Hash: "c1", ShortHash: "c1", Subject: "newest"}},
		fileCache: map[string][]string{
			"c1": {"internal/tui/tui.go"},
		},
		expanded: map[string]bool{
			"internal":     true,
			"internal/tui": true,
		},
		mode: modeDirectories,
	}
	m.loadDirectoryEntries()
	for i, entry := range m.visibleDirectoryEntries() {
		if entry.Path == "internal/tui" {
			m.dirIdx = i
			break
		}
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	got := updated.(model)

	if got.expanded["internal/tui"] {
		t.Fatal("selected directory remained expanded")
	}
	if got.visibleDirectoryEntries()[got.dirIdx].Path != "internal/tui" {
		t.Fatalf("selected path = %q, want internal/tui", got.visibleDirectoryEntries()[got.dirIdx].Path)
	}
}

func TestLeftFromFileCollapsesAndSelectsParentDirectory(t *testing.T) {
	m := model{
		commits: []git.Commit{{Hash: "c1", ShortHash: "c1", Subject: "newest"}},
		fileCache: map[string][]string{
			"c1": {"internal/tui/tui.go"},
		},
		expanded: map[string]bool{
			"internal":     true,
			"internal/tui": true,
		},
		mode: modeDirectories,
	}
	m.loadDirectoryEntries()
	for i, entry := range m.visibleDirectoryEntries() {
		if entry.Path == "internal/tui/tui.go" {
			m.dirIdx = i
			break
		}
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	got := updated.(model)
	entries := got.visibleDirectoryEntries()

	if got.expanded["internal/tui"] {
		t.Fatal("parent directory remained expanded")
	}
	if entries[got.dirIdx].Path != "internal/tui" {
		t.Fatalf("selected path = %q, want internal/tui", entries[got.dirIdx].Path)
	}
}

func TestTabCyclesBetweenCommitAndDirectoryViews(t *testing.T) {
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

func TestEnterDirectoryEntryOpensCurrentWorktreeFile(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "internal/tui/tui.go", "committed content\n")
	runTUITestGit(t, root, "add", ".")
	runTUITestGit(t, root, "commit", "-m", "committed")
	writeTUITestFile(t, root, "internal/tui/tui.go", "current worktree content\n")

	m := model{
		root: root,
		commits: []git.Commit{
			{Hash: "c1", ShortHash: "c1", Subject: "newest"},
		},
		fileCache: map[string][]string{
			"c1": {"internal/tui/tui.go", "README.md"},
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

	if m.mode != modeFullFile {
		t.Fatalf("mode = %v, want modeFullFile", m.mode)
	}
	if !m.worktreeFile {
		t.Fatal("worktreeFile = false, want true")
	}
	if len(m.files) != 1 || m.files[0] != "internal/tui/tui.go" {
		t.Fatalf("files = %v, want selected worktree file", m.files)
	}
	content := ansi.Strip(strings.Join(m.fullLines, "\n"))
	if !strings.Contains(content, "current worktree content") {
		t.Fatalf("full file does not contain current content: %q", content)
	}
	if strings.Contains(content, "committed content") {
		t.Fatalf("full file contains committed content instead of current content: %q", content)
	}
}

func TestBackFromDirectoryFileRestoresDirectoryDepth(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "internal/tui/tui.go", "package tui\n")
	writeTUITestFile(t, root, "internal/hooks/hooks.go", "package hooks\n")
	m := model{
		root: root,
		commits: []git.Commit{
			{Hash: "c1", ShortHash: "c1", Subject: "newest"},
		},
		fileCache: map[string][]string{
			"c1": {"internal/tui/tui.go", "internal/hooks/hooks.go"},
		},
		expanded: map[string]bool{
			"internal":     true,
			"internal/tui": true,
		},
		mode: modeDirectories,
	}
	m.loadDirectoryEntries()
	for i, entry := range m.visibleDirectoryEntries() {
		if entry.Path == "internal/tui/tui.go" {
			m.dirIdx = i
			break
		}
	}
	wantDirIdx := m.dirIdx

	m.enter(false)

	if m.mode != modeFullFile {
		t.Fatalf("mode after opening directory file = %v, want modeFullFile", m.mode)
	}
	if m.fileReturn != modeDirectories {
		t.Fatalf("fileReturn = %v, want modeDirectories", m.fileReturn)
	}

	m.back()
	if m.mode != modeDirectories {
		t.Fatalf("mode after leaving current file = %v, want modeDirectories", m.mode)
	}
	if m.dirIdx != wantDirIdx {
		t.Fatalf("dirIdx = %d, want %d", m.dirIdx, wantDirIdx)
	}
	if !m.expanded["internal"] || !m.expanded["internal/tui"] {
		t.Fatalf("expanded directory depth was lost: %+v", m.expanded)
	}
}

func TestRefreshCurrentDirectoryFileReloadsWorktreeContent(t *testing.T) {
	root := newTUITestRepo(t)
	writeTUITestFile(t, root, "current.txt", "before refresh\n")
	runTUITestGit(t, root, "add", ".")
	runTUITestGit(t, root, "commit", "-m", "base")
	commits, err := git.CommitsWithUncommitted(root, 10)
	if err != nil {
		t.Fatal(err)
	}
	m := model{
		root:      root,
		limit:     10,
		commits:   commits,
		fileCache: map[string][]string{},
		diffCache: map[string][]string{},
		fullCache: map[string][]string{},
		expanded:  map[string]bool{},
		mode:      modeDirectories,
	}
	m.loadDirectoryEntries()
	for i, entry := range m.visibleDirectoryEntries() {
		if entry.Path == "current.txt" {
			m.dirIdx = i
			break
		}
	}
	m.enter(false)
	writeTUITestFile(t, root, "current.txt", "after refresh\n")

	m.refresh()

	if m.mode != modeFullFile || !m.worktreeFile {
		t.Fatalf("mode/worktree after refresh = %v/%v", m.mode, m.worktreeFile)
	}
	if got := ansi.Strip(strings.Join(m.fullLines, "\n")); !strings.Contains(got, "after refresh") {
		t.Fatalf("refreshed content = %q, want current worktree content", got)
	}
}

func TestBackFromCommitFileReturnsToCommits(t *testing.T) {
	m := model{
		commits: []git.Commit{
			{Hash: "c1", ShortHash: "c1", Subject: "newest"},
		},
		fileCache: map[string][]string{
			"c1": {"README.md"},
		},
		mode: modeCommits,
	}

	m.enter(false)
	m.back()

	if m.mode != modeCommits {
		t.Fatalf("mode after leaving commit files = %v, want modeCommits", m.mode)
	}
}

func newTUITestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runTUITestGit(t, root, "init")
	runTUITestGit(t, root, "config", "user.email", "agentgit@example.test")
	runTUITestGit(t, root, "config", "user.name", "agentgit")
	return root
}

func writeTUITestFile(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runTUITestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func BenchmarkHighlightLargeGoFile(b *testing.B) {
	var code strings.Builder
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&code, "func value%d() int { return %d }\n", i, i)
	}
	text := code.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Highlight("bench.go", text)
	}
}

func BenchmarkHighlightDiffLargeGoFile(b *testing.B) {
	lines := make([]string, 0, 3003)
	lines = append(lines, "--- a/bench.go", "+++ b/bench.go", "@@ -1,3000 +1,3000 @@")
	for i := 0; i < 3000; i++ {
		switch i % 3 {
		case 0:
			lines = append(lines, fmt.Sprintf("-func old%d() int { return %d }", i, i))
		case 1:
			lines = append(lines, fmt.Sprintf("+func new%d() int { return %d }", i, i))
		default:
			lines = append(lines, fmt.Sprintf(" func keep%d() int { return %d }", i, i))
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = highlightDiff("bench.go", lines)
	}
}

func BenchmarkHighlightDiffLineByLineLargeGoFile(b *testing.B) {
	lines := make([]string, 0, 3003)
	lines = append(lines, "--- a/bench.go", "+++ b/bench.go", "@@ -1,3000 +1,3000 @@")
	for i := 0; i < 3000; i++ {
		switch i % 3 {
		case 0:
			lines = append(lines, fmt.Sprintf("-func old%d() int { return %d }", i, i))
		case 1:
			lines = append(lines, fmt.Sprintf("+func new%d() int { return %d }", i, i))
		default:
			lines = append(lines, fmt.Sprintf(" func keep%d() int { return %d }", i, i))
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = highlightDiffLineByLineForBenchmark("bench.go", lines)
	}
}

func highlightDiffLineByLineForBenchmark(filename string, lines []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		if len(line) > 0 && (line[0] == '+' || line[0] == '-' || line[0] == ' ') {
			highlighted := Highlight(filename, line[1:])
			if len(highlighted) > 0 {
				result[i] = string(line[0]) + highlighted[0]
				continue
			}
		}
		result[i] = line
	}
	return result
}
