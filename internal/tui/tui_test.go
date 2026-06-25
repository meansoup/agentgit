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
	for _, mode := range []mode{modeDiff, modeFullFile, modeRequest} {
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

	if !strings.Contains(joined, "+abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("wrapped diff lost code content:\n%s", got)
	}
	if strings.Contains(got, "...") {
		t.Fatalf("wrapped diff contains truncation marker:\n%s", got)
	}
}

func TestWrappedRequestPreservesLongLine(t *testing.T) {
	message := "abcdefghijklmnopqrstuvwxyz"
	m := model{
		commits: []git.Commit{{Hash: "abc", ShortHash: "abc", Subject: "request"}},
		links: map[string][]store.LinkedRequest{
			"abc": {{AgentName: "codex", Model: "gpt", Message: message}},
		},
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
		"11    │ -old",
		"   21 │ +new",
		"   22 │ +added",
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

	for _, want := range []string{"PATH", "BRANCH", "HEAD", "VIEW", "TARGET", "HELP", "main", "69e67e5", "commits", "dirty 2"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header missing %q:\n%s", want, header)
		}
	}
	if got := lipgloss.Height(m.viewHeader()); got != 5 {
		t.Fatalf("header height = %d, want 5", got)
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

func TestHeaderIncludesPersistentCommands(t *testing.T) {
	m := model{
		mode:  modeFiles,
		width: 120,
	}

	header := ansi.Strip(m.viewHeader())

	for _, command := range []string{"? shortcuts", "/ search", "ctrl+c quit"} {
		if !strings.Contains(header, command) {
			t.Fatalf("header missing command %q:\n%s", command, header)
		}
	}
	for _, removed := range []string{"up/down", "enter/right"} {
		if strings.Contains(header, removed) {
			t.Fatalf("header retained footer shortcut %q:\n%s", removed, header)
		}
	}
}

func TestHeaderRowsKeepFixedDimensionsAcrossViews(t *testing.T) {
	for _, width := range []int{24, 72, 140} {
		for _, mode := range []mode{
			modeCommits,
			modeDirectories,
			modeRequests,
			modeFiles,
			modeDiff,
			modeFullFile,
			modeRequest,
			modeSelect,
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

			header := m.viewHeader()
			lines := strings.Split(header, "\n")
			if len(lines) != 5 {
				t.Fatalf("width %d mode %v header rows = %d, want 5:\n%s", width, mode, len(lines), ansi.Strip(header))
			}
			for i, line := range lines {
				if got := ansi.StringWidth(ansi.Strip(line)); got != m.width {
					t.Fatalf("width %d mode %v row %d width = %d, want %d: %q", width, mode, i, got, m.width, ansi.Strip(line))
				}
			}
		}
	}
}

func TestHeaderSeparatesRepositoryGitViewAndTarget(t *testing.T) {
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

	lines := strings.Split(ansi.Strip(m.viewHeader()), "\n")

	if !strings.Contains(lines[1], "PATH") || !strings.Contains(lines[1], "BRANCH") {
		t.Fatalf("top context row missing path and branch: %q", lines[1])
	}
	if !strings.Contains(lines[2], "HEAD") || !strings.Contains(lines[2], "VIEW") {
		t.Fatalf("middle context row missing head and view: %q", lines[2])
	}
	if !strings.Contains(lines[3], "TARGET") || !strings.Contains(lines[3], "selected commit") {
		t.Fatalf("bottom context row missing target selection: %q", lines[3])
	}
}

func TestSlashOpensFuzzyFileSearch(t *testing.T) {
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

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	got := updated.(model)

	if !got.searchOpen {
		t.Fatal("searchOpen = false after /")
	}
	if len(got.searchResults) != 3 {
		t.Fatalf("initial search results = %d, want 3", len(got.searchResults))
	}
	header := ansi.Strip(got.viewHeader())
	for _, want := range []string{"search", "query type to filter files", "TARGET", "HELP"} {
		if !strings.Contains(header, want) {
			t.Fatalf("search header missing %q:\n%s", want, header)
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

func TestTabCyclesTopLevelViewsIncludingRequests(t *testing.T) {
	m := model{
		root:   newTUITestRepo(t),
		mode:   modeCommits,
		width:  100,
		height: 24,
		requests: []store.RequestSummary{
			{ID: 1, AgentName: "codex", Model: "gpt-5", Message: "one"},
		},
	}
	writeTUITestFile(t, m.root, "internal/tui/tui.go", "package tui\n")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	got := updated.(model)
	if got.mode != modeDirectories {
		t.Fatalf("first tab mode = %v, want modeDirectories", got.mode)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
	got = updated.(model)
	if got.mode != modeRequests {
		t.Fatalf("second tab mode = %v, want modeRequests", got.mode)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
	got = updated.(model)
	if got.mode != modeCommits {
		t.Fatalf("third tab mode = %v, want modeCommits", got.mode)
	}
}

func TestRequestListEnterOpensFullRequest(t *testing.T) {
	m := model{
		mode: modeRequests,
		requests: []store.RequestSummary{
			{ID: 7, AgentName: "claude", Model: "sonnet", Message: "full request body", StartedAt: "2026-06-25T00:00:00Z", CommitRefs: []string{"abc123"}},
		},
		width:  80,
		height: 24,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.mode != modeRequest {
		t.Fatalf("mode = %v, want modeRequest", got.mode)
	}
	if got.requestReturn != modeRequests {
		t.Fatalf("requestReturn = %v, want modeRequests", got.requestReturn)
	}
	view := ansi.Strip(got.viewRequestFull())
	for _, want := range []string{"claude", "sonnet", "abc123", "full request body"} {
		if !strings.Contains(view, want) {
			t.Fatalf("request full view missing %q:\n%s", want, view)
		}
	}
}

func TestRequestListRendersBulletTimeAgentRequestAndShortCommitHashes(t *testing.T) {
	m := model{
		mode: modeRequests,
		requests: []store.RequestSummary{
			{
				ID:         7,
				AgentName:  "claude",
				Model:      "sonnet",
				Message:    "full request body",
				StartedAt:  "2026-06-25T13:14:15Z",
				CommitRefs: []string{"1234567890abcdef", "abcdef1234567890"},
			},
		},
		width: 120,
	}

	view := ansi.Strip(m.viewRequestsList(120))

	if !strings.Contains(view, "● 06-25 13:14 [claude sonnet] full request body (12345678, abcdef12)") {
		t.Fatalf("request list row missing expected format:\n%s", view)
	}
	if strings.Contains(view, "1234567890ab") || strings.Contains(view, "abcdef123456") {
		t.Fatalf("request list row retained long commit hashes:\n%s", view)
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

func TestFrameUsesFormerFooterRowForContent(t *testing.T) {
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
	if strings.Contains(ansi.Strip(view), "ctrl+c quit") {
		t.Fatalf("frame still contains footer shortcuts:\n%s", ansi.Strip(view))
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

func TestTabCyclesBetweenCommitDirectoryAndRequestViews(t *testing.T) {
	m := model{
		commits: []git.Commit{{Hash: "c1", ShortHash: "c1", Subject: "change"}},
		requests: []store.RequestSummary{
			{ID: 1, AgentName: "codex", Model: "gpt-5", Message: "request"},
		},
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
	if got.mode != modeRequests {
		t.Fatalf("mode after second tab = %v, want modeRequests", got.mode)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyTab})
	got = updated.(model)
	if got.mode != modeCommits {
		t.Fatalf("mode after third tab = %v, want modeCommits", got.mode)
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
		links:     map[string][]store.LinkedRequest{},
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
