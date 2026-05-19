# agentgit

`agentgit` records AI-agent requests in a local SQLite database, links them to
Git commits through hooks, and provides a TUI for browsing commits, files, and
diffs.

## Build

Build distribution artifacts for release:

```sh
python3 -m pip install --upgrade build
python3 -m build
```

Artifacts are created in `dist/`:

- `dist/agentgit-<version>.tar.gz`
- `dist/agentgit-<version>-py3-none-any.whl`

Optional release checks:

```sh
python3 -m pip install --upgrade twine
python3 -m twine check dist/*
```

## Install for development

```sh
python3 -m pip install -e .
```

## Install for use

For a packaged release, prefer installing the wheel:

```sh
python3 -m pip install dist/agentgit-<version>-py3-none-any.whl
```

If you are running directly from a checkout instead of an installed package, the
`bin/agentgit` wrapper must be on `PATH`.

`export PATH=...` only affects the current shell session. It does not survive
opening a new terminal or rebooting the PC unless you add it to a shell startup
file.

### macOS

Temporary for the current shell only:

```sh
export PATH="/path/to/agentgit/bin:$PATH"
```

Persistent for future terminals on macOS with `zsh`:

```sh
echo 'export PATH="/path/to/agentgit/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

If your macOS shell is `bash` instead:

```sh
echo 'export PATH="/path/to/agentgit/bin:$PATH"' >> ~/.bash_profile
source ~/.bash_profile
```

### Ubuntu

Temporary for the current shell only:

```sh
export PATH="/path/to/agentgit/bin:$PATH"
```

Persistent for future terminals on Ubuntu with `bash`:

```sh
echo 'export PATH="/path/to/agentgit/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

If your Ubuntu shell is `zsh` instead:

```sh
echo 'export PATH="/path/to/agentgit/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

For distribution to end users, the cleaner path is publishing a wheel and having
users install `agentgit` with `pip`, `pipx`, Homebrew, or an OS package rather
than relying on manual `PATH` edits from a source checkout.

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
