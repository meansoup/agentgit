# agentgit

**agentgit** bridges AI coding agent requests with Git commits. Use your agent CLI (`codex`, `gemini`) as usual; `agentgit` automatically captures requests and links commits in a local TUI browser.

[한국어 문서 (README_ko.md)](./README_ko.md)

## Quick Start

1. **Install**: Place the `agentgit` binary in your `PATH`.
2. **Setup**: Link to your preferred AI agent.
   ```sh
   agentgit setup codex
   # or
   agentgit setup gemini
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
| `agentgit setup [agent]` | Install hooks for `codex` or `gemini`. |
| `agentgit --limit 50` | Limit the number of commits shown. |

## TUI Navigation

- **Move**: `j`/`k` or `Up`/`Down`
- **Drill down**: `l`, `Right`, or `Enter` (Commit → Files → Diff)
- **Open image**: `Enter` on an image file in the file list
- **Go back**: `h`, `Left`, or `Backspace`
- **Toggle View**: `m` (Unified/Split diff)
- **Next/Prev Hunk**: `n`/`p`
- **Quit**: `q`

## How It Works

- **Hooks**: `agentgit setup` installs lifecycle hooks that trigger on agent events.
- **Commit Linking**: When commits are created during an agent request, `agentgit` links them to that request without creating commits or changing commit messages.
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
