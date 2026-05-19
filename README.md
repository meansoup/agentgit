# agentgit

`agentgit` records AI-agent requests in a local SQLite database, links them to
Git commits through hooks, and provides a TUI for browsing commits, files, and
diffs.

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

## Run for development

```sh
go run ./cmd/agentgit --help
```

## Install for use

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

## Setup once per PC

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

## Record a Codex request and commit only request changes

```sh
agentgit codex start --model gpt-5 --message "implement request"
agentgit codex commit -m "Implement request"
agentgit codex finish
```

`start` snapshots the dirty files that existed before the request. `commit`
stages and commits only files that became dirty after that snapshot, then the
installed Git hook links the new commit to the active request.

If a request is already in progress before `start` was called, use
`--include-current` to treat existing dirty files as request-owned.

The same flow is available for future providers:

```sh
agentgit gemini start --model gemini-... --message "..."
agentgit claude start --model claude-... --message "..."
```

## Browse request-linked commits

```sh
agentgit log
```

Keys:

- `Up` / `Down`: move cursor
- `Right`: commit -> files -> diff
- `Left`: diff -> files -> commits
- `m`: toggle unified and split diff views
- `q`: quit
