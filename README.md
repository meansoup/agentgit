# agentgit

`agentgit` records AI-agent requests in a local SQLite database, links them to
Git commits through hooks, and provides a TUI for browsing commits, files, and
diffs.

## Install for development

```sh
python3 -m pip install -e .
```

If editable install is not available, add this checkout's `bin` directory to
`PATH`:

```sh
export PATH="/path/to/agentgit/bin:$PATH"
```

## Setup in any Git repository

```sh
agentgit setup
```

This installs a `post-commit` hook in the current repository and initializes the
local database. The default database is:

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
