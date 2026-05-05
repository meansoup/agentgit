# AgentGit

[🇰🇷 한국어 버전](./README_ko.md)

AgentGit is a TUI (Terminal User Interface) tool designed to help developers quickly review code changes made by AI agents. It connects agent request logs with Git history along a timeline, allowing you to understand the "why" and "how" behind every commit and uncommitted change.

## 🚀 Key Features

- **Request Graph**: Visualize the relationship between AI agent requests and Git changesets in a single timeline.
- **Timeline-based Linking**: Automatically associates requests from Claude, Gemini, and Codex with the commits that followed them.
- **Step-based Navigation**:
  - **Screen 1 (Change Sets)**: Explore the high-level workflow and request history.
  - **Screen 2 (Files)**: Drill down into specific modified files for a selected changeset.
  - **Screen 3 (Diff)**: Inspect detailed unified diffs with keyboard-driven scrolling.
- **Multi-Agent Support**: Parses logs from major AI providers to filter context-relevant requests.

## 🛠 How it Works

AgentGit uses "Timeline-based Linking" to associate requests with code changes. It identifies requests relevant to the current repository and links them to:
1. **Commits**: Requests that occurred between the previous commit and the current one.
2. **Working Tree**: Requests that occurred after the most recent commit.

*Note: This linking is based on timestamps and session context, intended for rapid review rather than absolute causal proof.*

## ⌨️ Controls

- `Up/Down`: Navigate lists or scroll diffs.
- `Enter / Right`: Drill down (ChangeSet -> Files -> Diff).
- `Esc / Left`: Go back to the previous screen.
- `r`: Refresh logs and repository state.
- `q`: Quit.

## 📦 Installation

### Homebrew (macOS / Linux)
```bash
brew tap meansoup/tap
brew install agentgit
```

### APT (Ubuntu / Debian)
1. Download the latest `.deb` file from [Releases](https://github.com/meansoup/agentgit/releases).
2. Install it using `apt`:
```bash
sudo apt install ./agentgit_0.1.0_amd64.deb
```

### From Source

**Prerequisites:** [Go](https://go.dev/doc/install) 1.23 or higher.

```bash
# Clone the repository
git clone https://github.com/meansoup/agentgit.git
cd agentgit

# Build and install
go build -o agentgit main.go
```

## 📖 Usage

Run `agentgit` from within any Git repository:

```bash
./agentgit
```

Or specify a target path:

```bash
./agentgit /path/to/your/repo
```

## 📄 License

MIT
