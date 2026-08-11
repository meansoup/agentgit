# agentgit

**agentgit** is a Git-aware AI-agent workspace. It can browse Git history plus local agent transcripts, and it can run an agent CLI inside a tmux-like terminal wrapper with a persistent one-line command bar.

[한국어 문서 (README_ko.md)](./README_ko.md)

## Quick Start

1. **Install**: Place the `agentgit` binary in your `PATH`.
2. **Use**: Run your agent normally (e.g., `codex`). Then browse the repository:
   ```sh
   agentgit
   ```

## Essential Commands

| Command | Description |
| :--- | :--- |
| `agentgit` | Open the history browser for the current directory. |
| `agentgit [path]` | Browse history for a specific repo, folder, or file. |
| `agentgit --limit 50` | Limit the number of commits shown. |
| `agentgit terminal` | Run the first available agent CLI from `codex`, `claude`, or `gemini` inside the wrapper. |
| `agentgit terminal -- codex` | Run a specific command inside the wrapper. |

## Terminal Wrapper

`agentgit terminal` starts an agent CLI in the current repository through a PTY. The agent gets the full terminal minus the bottom status line, and `agentgit` owns that last line for prefix commands.

- **Prefix key**: `Ctrl+G`
- **Help**: `Ctrl+G`, then `?`
- **Redraw status**: `Ctrl+G`, then `r`
- **Send literal Ctrl+G to the agent**: `Ctrl+G`, then `g`
- **Quit wrapper and terminate the agent process**: `Ctrl+G`, then `q`

If no command is provided, `agentgit terminal` uses `AGENTGIT_AGENT` when set, otherwise it tries `codex`, `claude`, then `gemini`.

## TUI Navigation

The top context bar shows the base path, Git branch, HEAD, commit count, and dirty file count.

- **Move**: `Up`/`Down`
- **Toggle Commit/Directory View**: `Tab`
- **Select latest commits**: `s`, then `Space` to select, `x` to delete, `m` to merge, `y` to confirm
- **Search files**: `Ctrl+P`, type a fuzzy query, then `Enter` to reveal the file in Directory view
- **Recent files**: `Ctrl+E`, filter recently opened files and `Enter` to reopen
- **Directory folders**: `Enter`/`Right` toggles folders and opens files
- **Drill down**: `Right` or `Enter` (Commit → Files → Diff)
- **Open image**: `Enter` on an image file in the file list
- **Go back**: `Left` or `Backspace`
- **Request drawer**: `v` toggles a bottom drawer; `Enter` opens full request details
- **Push**: `g` then `p` runs `git push`
- **Delete merged branches**: `g` then `b` then `d` deletes local branches from `git branch --merged`, excluding the current branch and `main`/`master`/`develop`/`dev`
- **Refresh**: `r`
- **Toggle View**: `m` (Unified/Split diff)
- **Toggle Line Numbers**: `l` (Diff/Full file)
- **Toggle Long-Line Wrapping**: `w` (Diff/Full file/Request)
- **Next/Prev Hunk**: `n`/`p`
- **Help dialog**: `?`
- **Quit**: `Ctrl+C`

## How It Works

- **Passive by default**: Browsing does not install hooks, edit global config, change Git hooks, create commits, reset commits, or write request records.
- **Select Mode**: Explicitly selected latest commits can be deleted or merged only when the working tree is clean and the selection is a contiguous range starting at `HEAD`. Delete uses `git reset --hard`; merge squashes the selected commits.
- **Transcripts**: Requests are scanned from local agent transcript files for Claude, Codex, and Gemini.
- **Facts only**: Requests are not linked to commits by timestamp or other approximation. Git data comes from Git, and request data comes from transcript fields.
- **Local DB**: The SQLite schema can still be initialized at `~/.local/share/agentgit/agentgit.sqlite3`, but request browsing is driven by transcript scans.

## Installation

Download the binary for your OS and architecture, then:

```sh
# Example for macOS (Apple Silicon)
mkdir -p ~/.local/bin
install -m 0755 dist/agentgit_darwin_arm64 ~/.local/bin/agentgit

# Add to PATH (zsh)
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

## Build

```sh
# Local binary
go build -o dist/agentgit ./cmd/agentgit

# Release binaries
make release
```
