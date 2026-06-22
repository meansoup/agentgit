# agentgit

**agentgit** bridges AI coding agent requests with Git commits. Use your agent CLI (`codex`, `gemini`, `claude`) as usual; `agentgit` automatically captures requests and links commits in a local TUI browser.

[한국어 문서 (README_ko.md)](./README_ko.md)

## Quick Start

1. **Install**: Place the `agentgit` binary in your `PATH`.
2. **Setup**: Link to your preferred AI agent.
   ```sh
   agentgit setup codex
   # or
   agentgit setup gemini
   # or
   agentgit setup claude
   ```
3. **Use**: Run your agent normally (e.g., `codex`). Then browse the linked history:
   ```sh
   agentgit
   ```

## Essential Commands

| Command | Description |
| :--- | :--- |
| `agentgit` | Open the history browser for the current directory. |
| `agentgit [path]` | Browse history for a specific repo, folder, or file. |
| `agentgit setup [agent]` | Install hooks for `codex`, `gemini`, or `claude`. |
| `agentgit --limit 50` | Limit the number of commits shown. |

## TUI Navigation

The top context bar shows the base path, Git branch, HEAD, commit count, and dirty file count.

- **Move**: `Up`/`Down`
- **Toggle Commit/Directory View**: `Tab`
- **Search files**: `/`, type a fuzzy query, then `Enter` to reveal the file in Directory view
- **Select latest commits**: `s`, then `Space` to select, `x` to remove, `m` to merge, `y` to confirm
- **Directory folders**: `Enter`/`Right` toggles folders and opens files
- **Drill down**: `Right` or `Enter` (Commit → Files → Diff)
- **Open image**: `Enter` on an image file in the file list
- **Go back**: `Left` or `Backspace`
- **Request details**: `v`
- **Refresh**: `r`
- **Toggle View**: `m` (Unified/Split diff)
- **Toggle Line Numbers**: `l` (Diff/Full file)
- **Toggle Long-Line Wrapping**: `w` (Diff/Full file/Request)
- **Next/Prev Hunk**: `n`/`p`
- **Help dialog**: `?`
- **Quit**: `Ctrl+C`

## How It Works

- **Hooks**: `agentgit setup` installs lifecycle hooks that trigger on agent events.
- **Auto Commit + Linking**: After an agent request, `agentgit` commits new working tree changes automatically when the request started from a clean tree, then links the commit to that request. Commits created manually during a request are also linked.
- **Select Mode**: Remove or merge only a contiguous range of latest commits starting at `HEAD`. Remove can also discard selected uncommitted changes; merge squashes selected commits and moves their request links to the new commit in the local DB.
- **Local DB**: Metadata is stored in `~/.local/share/agentgit/agentgit.sqlite3`.
- **Transparency**: Your existing workflow remains unchanged. `agentgit` works silently in the background.

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
