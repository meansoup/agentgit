# agentgit

한국어 문서: [README.ko.md](./README.ko.md)

Keep this file in sync with `README.ko.md`.

`agentgit` links AI-agent requests to Git commits without changing how you use
the agent CLI. After setup, keep using `codex` normally. `agentgit` receives
Codex lifecycle hook events, records requests in a local SQLite database, creates
request-scoped commits when a request changed files, and provides a TUI for
browsing request-linked commit history.

## Commands

There are two user-facing commands:

```sh
agentgit
agentgit setup codex
```

`agentgit [path]` shows request-linked commits for the current path or a
specified path.

`agentgit setup codex` installs Codex lifecycle hooks once for this PC.

Internal hook command:

```sh
agentgit hook codex
```

This command is written into Codex hook configuration and is not meant to be run
manually.

## Quick Start

```sh
# Install the release binary somewhere on PATH first.
agentgit setup codex

cd /path/to/project
codex
# Use Codex normally. If the request changes files in a Git repository,
# agentgit records the request and creates a request-scoped commit.

agentgit
```

Codex may ask you to review and trust the installed hook from `/hooks`. This is
normal for non-managed Codex hooks.

## How It Works

`agentgit setup codex` writes hook configuration to:

```text
~/.codex/hooks.json
```

The Codex hook flow is:

- `UserPromptSubmit`: record the request message, agent name, model, session id,
  turn id, current Git root, dirty-file baseline, and current `HEAD`.
- `Stop`: compare the current working tree to the baseline, commit only files
  changed by that request, and link the resulting commit to the recorded request.
- If Codex or the user already created commits during the request, `agentgit`
  links commits created after the baseline `HEAD`.

The database is local and CLI-agnostic:

```text
~/.local/share/agentgit/agentgit.sqlite3
```

Override it with:

```sh
AGENTGIT_DB=/path/to/agentgit.sqlite3
```

## Browse Request-Linked Commits

Current path:

```sh
agentgit
```

Specific repository, directory, or file path:

```sh
agentgit /path/to/project
agentgit /path/to/project/file.go
```

Limit commit count:

```sh
agentgit --limit 100
agentgit --limit 100 /path/to/project
```

TUI keys:

- `Up` / `Down` or `k` / `j`: move cursor
- `Right` / `Enter` or `l`: commit -> files -> diff
- `Left` / `Backspace` or `h`: diff -> files -> commits
- `m`: toggle unified and split diff views
- `q`: quit

In non-TTY environments, `agentgit` prints a static colorized view instead of
opening the TUI.

Example:

```text
7cbbb0c8 04-06 15:16  commit message
└─ ● [codex gpt-5] request message
12345678 04-06 15:16  another commit
```

## Provider Status

Current setup support:

- `agentgit setup codex`

Planned setup support:

- `agentgit setup claude`
- `agentgit setup gemini`

The database schema uses generic agent terms such as `agent_name`, `model`,
`session_id`, `turn_id`, and `request_commits`, so it is not tied to Codex-only
terminology.

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
go run ./cmd/agentgit -- --help
```
