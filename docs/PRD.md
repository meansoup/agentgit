# Product Requirements Document

## Product

`agentgit`

## Summary

`agentgit` is a distributable Go CLI/TUI tool that records coding-agent
requests through each agent CLI's native hook system and links those requests to
Git commits in a local SQLite database.

The user must continue using each agent CLI directly. For Codex, the user runs
`codex` exactly as before. `agentgit` must work through installed hooks, not
through replacement commands such as `agentgit codex start`.

## Corrected Command Model

There are two user-facing commands:

```sh
agentgit
agentgit setup codex
```

`agentgit [path]` opens the TUI or static log view for the current path or the
specified path.

`agentgit setup codex` installs persistent Codex hook configuration for this
machine.

Internal hook entrypoints such as `agentgit hook codex` may exist, but they are
not user-facing workflow commands.

## Problem

AI coding agents can edit files and create commits, but the request that caused
each change is often lost. Developers need a local, deterministic way to see:

- which request was sent to an agent
- which agent and model handled it
- which Git commit was created for the request
- which files and diffs belong to the commit

This must happen without changing the normal agent CLI workflow.

## Goals

- Keep Codex usage unchanged: users continue to run `codex`.
- Install Codex hooks once per machine with `agentgit setup codex`.
- Capture Codex request metadata from Codex lifecycle hook payloads.
- Automatically commit request-owned file changes when a request changes files
  inside a Git repository.
- Store request and commit links in a local SQLite database using generic,
  provider-neutral terminology.
- Provide `agentgit [path]` to statically load schema-backed data and show
  request-linked Git history.
- Preserve a TUI flow for commit cursor, file cursor, and file diff navigation.
- Keep the implementation in Go for maintainable binary distribution on macOS
  and Ubuntu/Linux.

## Non-Goals

- Replacing `codex`, `claude`, or `gemini` commands.
- Requiring users to run `agentgit codex start`, `agentgit codex commit`, or
  `agentgit codex finish`.
- Using an AI model to reconstruct request-commit links at read time.
- Cloud sync or multi-user database coordination.
- Git hosting integration.

## Required Codex Workflow

Setup:

```sh
agentgit setup codex
```

Daily use:

```sh
cd /path/to/git/project
codex
agentgit
```

Expected behavior:

- Codex runs normally.
- Agentgit receives Codex hook events.
- If a Codex request creates commits in a Git repository, agentgit links those
  commits to the request.
- Agentgit records the request, agent name, model, session id, turn id, commit
  id, repository root, and timestamps in the local database.
- `agentgit` shows the current repository's commit list with linked request rows.

## Future Agent Workflow

The same model must be extended to:

```sh
agentgit setup claude
agentgit setup gemini
```

Claude and Gemini users must continue using their native CLIs. Agentgit should
integrate through each tool's hook/config system.

## Functional Requirements

### 1. Setup

- `agentgit setup codex` must install Codex lifecycle hook configuration for the
  current machine.
- Setup must persist after terminal restart and PC reboot.
- Setup must not require per-repository manual installation.
- Setup must initialize the local database.
- Setup must preserve existing Codex hooks when possible and avoid duplicate
  agentgit hook entries.

### 2. Codex Hook Capture

- Agentgit must use Codex lifecycle hooks instead of replacing the Codex command.
- On request submission, agentgit must record:
  - agent name
  - model
  - request message
  - session id
  - turn id
  - current working directory
  - Git repository root, when available
  - dirty-file baseline
  - current `HEAD`
- On request stop/completion, agentgit must detect commits created after the
  request baseline `HEAD`.

### 3. Request-Scoped Commit Linking

- If commits are created during the request, agentgit must link commits created
  after the request baseline `HEAD`.
- agentgit must not create commits or modify commit messages.
- If no Git repository is present, agentgit must not create a commit.
- If no commits are created during the request, agentgit must not create an
  empty commit.

### 4. Local Database

- The database must be local SQLite by default:

```text
~/.local/share/agentgit/agentgit.sqlite3
```

- `AGENTGIT_DB` must override the database path.
- Schema terminology must be provider-neutral and not Codex-specific.
- Required request fields:
  - request id
  - agent name
  - model
  - request message
  - session id
  - turn id
  - repository root
  - baseline dirty-file snapshot
  - baseline `HEAD`
  - started timestamp
  - finished timestamp
- Required link fields:
  - request id
  - commit hash
  - repository root
  - linked timestamp

### 5. Request-Commit Browser

- `agentgit` with no args must show the current path's Git commit list.
- `agentgit <file_path>` must show the Git commit list for the repository
  containing that file path.
- `agentgit <directory_path>` must show the Git commit list for that repository.
- Request metadata must be loaded from the local database through static SQL
  based on the schema.
- No AI model may be used to construct the request-commit view.

Display format:

```text
7cbbb0c8 04-06 15:16  commit message
└─ ● [codex model] request message
12345678 04-06 15:16  another commit
abcdefgh 04-06 15:16  another commit
└─ ● [codex model] request message
```

Visual requirements:

- Commit id has a distinct color.
- Agent/model label has a distinct color.
- Request message has a distinct color.

TUI navigation:

- Commit list has a commit-level cursor.
- `Up` and `Down` move through commits.
- `Right` on a commit opens the changed-file list for that commit.
- File list has its own cursor.
- `Up` and `Down` move through files.
- `Right` on a file opens the file diff.
- `Left` returns from diff to files and from files to commits.
- `m` toggles unified diff and split diff.
- `q` exits.

### 6. Distribution and Maintenance

- The implementation language is Go.
- The product must build to standalone binaries for:
  - macOS amd64
  - macOS arm64
  - Ubuntu/Linux amd64
  - Ubuntu/Linux arm64
- Documentation must cover install, persistent PATH setup, setup, browsing, and
  release builds.

## Success Criteria

- A user installs agentgit, runs `agentgit setup codex`, then continues using
  `codex` directly.
- A Codex request that creates commits in a Git repo links those commits to the
  request.
- The request and commit are linked in the local database.
- `agentgit` shows the linked request under the matching commit.
- `agentgit <file_path>` works from a file path.
- The TUI supports commit/file/diff navigation and diff mode switching.

## Current Status

Implemented:

- Go CLI/TUI
- `agentgit [path]`
- `agentgit setup codex`
- Codex `UserPromptSubmit` and `Stop` hook integration
- local SQLite storage
- provider-neutral schema extensions such as `agent_name`, `session_id`,
  `turn_id`, and `baseline_head`
- request-scoped commit linking from Codex hook events
- commit/request TUI with file and diff drill-down
- macOS and Linux release builds

Planned:

- `agentgit setup claude`
- `agentgit setup gemini`
- richer hook diagnostics and setup verification
