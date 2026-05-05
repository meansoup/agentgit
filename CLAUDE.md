# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**agentgit** is a TUI (Terminal User Interface) code review tool for agent-assisted development. It displays the relationship between user requests (from Claude/Gemini logs) and git commits, allowing developers to quickly understand which changes resulted from which requests.

The three-screen UX:
1. **Graph** - Overview of requests and linked commits
2. **Files** - Files changed in selected changeset
3. **Diff** - Detailed diff for selected file

See `prd.md` and `wireframe.md` for full product specs.

## Build & Run

### Build
```bash
go build -o agentgit
```

### Run
```bash
./agentgit [path]
```

The app auto-detects the git repository root. If a path is provided, it changes to that directory first.

### Testing Request Log Parsing
```bash
go run test_logs.go
```
This utility verifies that Claude and Gemini request logs are being parsed correctly in your environment.

## Project Structure

### Core Packages

- **`internal/model/types.go`** - Data models
  - `ChangeSet` - Represents a commit or uncommitted changes
  - `LinkedRequest` - A user request linked to one or more changesets (time-based linking)
  - `ChangedFile` - File metadata (status, additions, deletions)
  - `FileDiff` - Diff content with binary/truncation flags
  - `Commit` - Git commit metadata

- **`internal/git/git.go`** - Git operations
  - `FindRoot()` - Locates repository root via `git rev-parse`
  - `LoadCommits(n)` - Fetches most recent n commits with formatted parsing
  - `GetStatus()` - Gets uncommitted changes
  - `GetDiff(sha)` / `GetUncommittedDiff()` - Generates diffs with truncation handling for large files
  - `IsGitRepository()` - Validation

- **`internal/linker/linker.go`** - Links requests to changesets
  - Parses Claude/Gemini logs from system directories
  - Time-based linking: connects requests that occurred between commit timestamps
  - Groups consecutive requests sharing the same commits (deduplication)
  - Returns changesets: `[uncommitted...] + [commits...]`

- **`internal/log/claude.go` & `gemini.go`** - Request log parsing
  - `LoadClaudeRequests(gitRoot)` - Parses Claude session logs (from `~/.claude/logs/`)
  - `LoadGeminiRequests(gitRoot)` - Parses Gemini logs (from `~/.config/codeium/`)
  - Filters requests by repository working directory match
  - Returns timestamped request summaries

- **`internal/tui/app.go`** - Main TUI controller
  - State machine: `screen` tracks current view (screenGraph, screenFiles, screenDiff)
  - Delegates rendering/input to `Graph`, `Files`, `Diff` sub-models
  - Handles screen transitions (enter/escape, up/down navigation, refresh)

- **`internal/tui/graph.go`** - Request graph visualization
  - Renders changesets with linked requests above
  - Displays "Unlinked Commits" section separately
  - Handles selection and scrolling within viewport

- **`internal/tui/files.go`** - File list for selected changeset
  - Loads file list on demand when changeset selected
  - Shows file status badges ([M], [A], [D], etc.) and change stats
  - Passes selected file to diff screen

- **`internal/tui/diff.go`** - Diff viewer (read-only)
  - Renders patch with color/formatting
  - Handles pagination (pgup/pgdn)
  - Shows binary/truncation warnings

## Key Architecture Patterns

### Request-Changeset Linking
Linking is **time-based only** and doesn't prove causation. Rules (from `prd.md`):
- Requests extracted from log files with session ID and timestamp
- Each commit links requests occurring between its previous commit's timestamp and its own
- Uncommitted changes link requests after the last commit
- Consecutive requests sharing identical commit sets are deduplicated (shown as one request group with multiple requests)
- Commits with no requests move to "Unlinked Commits"

### Data Loading
- App startup loads changesets (commits + uncommitted), requests, and first changeset's file list
- Lazy loading: files load when a changeset is selected, diffs when diff screen enters
- Refresh (r key) reloads data while preserving selection where possible

### Error Handling
- Git errors display in status bar (footer)
- Large diffs truncate with a truncation flag
- Binary files show binary indicator instead of patch
- Missing repositories exit with clear error to stderr

## Development Notes

### Adding Log Providers
To support a new provider (e.g., Codex):
1. Create `internal/log/codex.go` with `LoadCodexRequests(gitRoot)` returning `[]LinkedRequest`
2. Update `linker.LinkRequestsToChangesets()` to call the new loader
3. Test with `test_logs.go`

### Modifying UI Layout
The three-screen paradigm is central to UX. Avoid mixing screens or adding a combined 3-column layout (see wireframe.md limitations). Changes to spacing, colors, and truncation thresholds belong in the `tui/` package styles.

### Diff Rendering
Large diffs are truncated at 50KB by design (see `git.go`). If truncation logic changes, update the `TooLarge` flag rendering in `diff.go`.

### Testing
Currently, there are no unit tests. The project relies on manual testing with `test_logs.go` and live TUI testing. If adding tests, focus on:
- Git operations (mocking `exec.Command`)
- Request log parsing edge cases
- Linking logic correctness

## Dependencies

- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/lipgloss` - Styling library

Go 1.23+ required.

## Performance Considerations

- Commit loading defaults to 50 recent commits (configurable in linker)
- Log parsing happens at app startup; large log files may delay initial render
- Diff rendering is synchronous; very large diffs may briefly freeze the UI (truncation helps)
- File list loading is fast for most repositories

## Common Debugging

**App exits with "not a git repository"**
- Ensure you're running from inside a git repo or pass the path as an argument

**No requests appear in graph**
- Check that Claude/Gemini logs exist in the expected directories (`~/.claude/logs/`, `~/.config/codeium/`)
- Run `go run test_logs.go` to verify log loading

**Diffs show truncation warning**
- File is larger than 50KB; use `git show` or IDE for full diff

**UI rendering looks off**
- Check terminal supports 256 colors; some minimal terminals may degrade rendering
