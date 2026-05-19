# agentgit

`agentgit` records AI-agent requests in a local SQLite database, links them to
Git commits through hooks, and provides a TUI for browsing commits, files, and
diffs.

## Quick Start

```sh
# Install the release binary somewhere on PATH first.
agentgit setup

cd /path/to/project
agentgit codex start --model gpt-5 --message "implement login validation"
# Run Codex and edit files.
agentgit codex commit -m "Implement login validation"
agentgit codex finish

agentgit log
```

`agentgit setup` is a one-time setup per PC. It initializes the local database
and installs a global Git `post-commit` hook, so the same setup works in any Git
repository on the machine.

## Daily Usage

### 1. Start an Agent Request

```sh
agentgit codex start --model gpt-5 --message "describe the request"
```

The same request flow is available for Claude and Gemini:

```sh
agentgit claude start --model claude-sonnet-4.5 --message "describe the request"
agentgit gemini start --model gemini-2.5-pro --message "describe the request"
```

`start` snapshots files that were already dirty before the request. Those files
are excluded from the request commit by default.

If the AI request already changed files before you called `start`, use:

```sh
agentgit codex start --model gpt-5 --message "describe the request" --include-current
```

### 2. Commit Only Request-Owned Changes

After the agent changes code:

```sh
agentgit codex commit -m "Implement requested change"
```

This stages and commits only files that became dirty after `start`. The Git hook
links the new commit to the active request in the local SQLite database.

Use the matching provider command for other agents:

```sh
agentgit claude commit -m "Implement requested change"
agentgit gemini commit -m "Implement requested change"
```

### 3. Finish the Request

```sh
agentgit codex finish
```

Provider-specific variants are also available:

```sh
agentgit claude finish
agentgit gemini finish
```

### 4. Browse Request-Linked Commits

```sh
agentgit log
```

Useful option:

```sh
agentgit log --limit 100
```

TUI keys:

- `Up` / `Down` or `k` / `j`: move cursor
- `Right` / `Enter` or `l`: commit -> files -> diff
- `Left` / `Backspace` or `h`: diff -> files -> commits
- `m`: toggle unified and split diff views
- `q`: quit

In non-TTY environments, `agentgit log` prints a static colorized view instead
of opening the TUI.

## Commands

```sh
agentgit setup
agentgit setup-local
agentgit log --limit 500
agentgit version

agentgit codex start --model <model> --message <message>
agentgit codex commit -m <commit-message>
agentgit codex finish

agentgit claude start --model <model> --message <message>
agentgit claude commit -m <commit-message>
agentgit claude finish

agentgit gemini start --model <model> --message <message>
agentgit gemini commit -m <commit-message>
agentgit gemini finish
```

Generic provider form:

```sh
agentgit request --provider codex start --model gpt-5 --message "request"
agentgit request --provider claude commit -m "commit message"
agentgit request --provider gemini finish
```

## Install

For a packaged release, install the matching binary for your OS and CPU
architecture somewhere on `PATH`.

### macOS

Apple Silicon:

```sh
install -m 0755 dist/agentgit_<version>_darwin_arm64 /usr/local/bin/agentgit
```

Intel Mac:

```sh
install -m 0755 dist/agentgit_<version>_darwin_amd64 /usr/local/bin/agentgit
```

If `/usr/local/bin` is not writable, install to a user-owned directory:

```sh
mkdir -p "$HOME/.local/bin"
install -m 0755 dist/agentgit_<version>_darwin_arm64 "$HOME/.local/bin/agentgit"
```

Persistent `PATH` for macOS `zsh`:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

Persistent `PATH` for macOS `bash`:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bash_profile
source ~/.bash_profile
```

### Ubuntu

x86_64:

```sh
install -m 0755 dist/agentgit_<version>_linux_amd64 /usr/local/bin/agentgit
```

ARM64:

```sh
install -m 0755 dist/agentgit_<version>_linux_arm64 /usr/local/bin/agentgit
```

If `/usr/local/bin` is not writable, install to a user-owned directory:

```sh
mkdir -p "$HOME/.local/bin"
install -m 0755 dist/agentgit_<version>_linux_amd64 "$HOME/.local/bin/agentgit"
```

Persistent `PATH` for Ubuntu `bash`:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

Persistent `PATH` for Ubuntu `zsh`:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

`export PATH=...` only affects the current shell session. It does not survive
opening a new terminal or rebooting the PC unless you add it to a shell startup
file.

For local source-checkout development, `bin/agentgit` runs the Go command with
`go run`; release users should install the compiled binary instead.

## Setup

```sh
agentgit setup
```

This initializes the database and configures Git's global `core.hooksPath` to
`~/.config/agentgit/hooks`, so commits in any repository can be linked to active
agent requests. If another global hooks path already exists, agentgit preserves
and calls its `post-commit` hook after recording its own data.

## Setup in one Git repository

```sh
agentgit setup-local
```

Use this only when you want repository-local setup instead of PC-wide setup. It
installs a `post-commit` hook in the current repository and initializes the local
database. The default database is:

```text
~/.local/share/agentgit/agentgit.sqlite3
```

Override it with `AGENTGIT_DB=/path/to/agentgit.sqlite3`.

## Build

Build a local binary:

```sh
go build -o dist/agentgit ./cmd/agentgit
```

Build release binaries for macOS and Ubuntu/Linux:

```sh
make release
```

Release artifacts are created in `dist/`:

- `agentgit_<version>_darwin_amd64`
- `agentgit_<version>_darwin_arm64`
- `agentgit_<version>_linux_amd64`
- `agentgit_<version>_linux_arm64`

Run for development:

```sh
go run ./cmd/agentgit --help
```
