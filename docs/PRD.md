# Product Requirements Document

## Product

`agentgit`

## Summary

`agentgit` is a local developer tool that links AI-agent requests to Git
commits, stores request metadata in a local database, and provides a terminal
UI for browsing commit history with request context.

The product must support `codex` immediately and be structured so that
additional providers such as `gemini` and `claude` can use the same workflow.

## Problem

AI-assisted coding sessions create a traceability gap:

- Developers can ask an agent to change code, but later it is hard to identify
  which user request produced which commit.
- Teams need commit history to remain clean and segmented by request.
- Request metadata must be queryable locally without depending on an AI model at
  read time.
- Setup must be simple enough to install once and use across repositories on
  the same machine.

## Goals

- Record AI-agent requests locally in a deterministic schema.
- Ensure each agent-driven code change can be committed as a request-scoped
  commit.
- Link commits to the request that produced them by using Git hooks.
- Provide a static, schema-driven command that shows Git history alongside AI
  request context.
- Provide a keyboard-driven terminal UI for exploring commits, changed files,
  and diffs.
- Support one-time machine setup and repeatable usage across repositories.

## Non-Goals

- Cloud sync of request metadata.
- AI-generated summaries at read time.
- Replacing Git hosting UI such as GitHub PR review.
- Multi-user database coordination.
- Automatic commit message generation.

## Primary Users

- Individual developers using AI coding agents locally.
- Teams that want a reproducible local workflow before broader rollout.
- Maintainers packaging `agentgit` for internal or external distribution.

## Core User Stories

1. As a developer, I want to start an AI request so the tool records the
   provider, model, and request message locally.
2. As a developer, I want to commit only the files changed by that request so my
   Git history stays segmented by task.
3. As a developer, I want the commit to be linked automatically to the active
   AI request through a Git hook.
4. As a developer, I want to browse commit history and see which AI request
   produced each commit.
5. As a developer, I want to expand a commit into files and expand a file into a
   diff directly from the terminal.
6. As a maintainer, I want setup to be installed once per machine and then work
   in any repository without per-repo manual hook wiring.
7. As a future integrator, I want `gemini` and `claude` to follow the same
   command pattern as `codex`.

## Functional Requirements

### 1. Request Tracking

- The tool must support `codex start`, `codex commit`, and `codex finish`.
- The tool must expose the same command structure for `gemini` and `claude`.
- `start` must record:
  - provider
  - model
  - request message
  - repository root
  - snapshot of files already dirty before the request
- `finish` must mark the active request as completed.
- Only one active request per repository is required for the initial version.

### 2. Request-Scoped Commit Creation

- When a code-changing request is committed through `agentgit`, only the files
  changed by that request must be included.
- Files that were already dirty before `start` must be excluded by default.
- A user must be able to override that baseline with an explicit option when the
  request began before `start` was invoked.
- Each `agentgit ... commit` call must create a Git commit for that request's
  changes.
- The tool must fail with a clear error if there are no request-owned file
  changes to commit.

### 3. Git Hook to Database Link

- When an `agentgit`-managed commit is created, the active request must be
  linked to the new commit hash through a local Git hook.
- The hook must be installable once per machine through a global Git hooks path.
- The product must preserve an existing global hooks path by chaining the prior
  `post-commit` hook when possible.
- A repository-local hook installation path may exist as a fallback, but the
  primary setup flow must be machine-wide.

### 4. Local Database

- Request and request-to-commit link data must be stored in a local deterministic
  schema.
- The read path must be static and schema-driven. It must not ask an AI model to
  synthesize results from raw data.
- The initial storage engine is SQLite.
- The default database path should be local to the machine user account and
  overridable by environment variable.

### 5. Commit and Request Browser

- The product must provide a command that shows Git commit history for the
  current repository.
- When a commit has linked AI requests, those requests must be shown directly
  beneath the commit entry.
- The UI must visually distinguish:
  - commit id
  - agent provider and model
  - request message
- The browser must support keyboard navigation with a commit-level cursor.
- Pressing `Right` on a commit must expand the changed file list for that commit.
- The file list must have its own cursor and support `Up` and `Down`.
- Pressing `Right` on a file must show the diff for that file.
- Pressing `Left` must navigate back from diff to file list and from file list to
  commit list.
- The diff viewer must support:
  - unified view
  - split view similar to PR-style side-by-side comparison
- Pressing `m` must toggle diff mode between unified and split.
- The browser must support `q` to quit.
- When a TTY is not available, the command must still print a readable static
  view of commits and linked requests.

## UX Requirements

- The commit list should resemble Git log output but add linked request rows
  below matching commits.
- The terminal output should be colorized when supported.
- The request row should clearly indicate provider and model, for example:

```text
7cbbb0c8 04-06 15:16  commit message
└─ ● [codex gpt-5] implement request
```

- Navigation must be keyboard-first and usable without a mouse.

## Setup and Installation Requirements

### Machine-Wide Setup

- The primary setup command must be `agentgit setup`.
- `agentgit setup` must:
  - initialize the database
  - install a global `post-commit` hook
  - configure `git config --global core.hooksPath ...`
- After setup, the workflow should work in any Git repository on the machine.

### Packaging and Distribution

- The project must be buildable as Go distribution artifacts.
- Release artifacts must include:
  - macOS arm64 binary
  - macOS amd64 binary
  - Ubuntu/Linux arm64 binary
  - Ubuntu/Linux amd64 binary
- Documentation must describe:
  - how to build the artifacts
  - how to install a release binary
  - how to run from a source checkout
  - how persistent shell `PATH` configuration differs from temporary `export`
  - platform-specific usage for macOS and Ubuntu

## Non-Functional Requirements

- Local-first operation with no network dependency for core workflows.
- Safe behavior in dirty repositories by respecting pre-existing modified files.
- Commands should be usable from standard developer shells.
- The product must avoid destructive Git operations.
- The implementation should remain understandable and extensible for additional
  providers.

## Data Model

The initial schema must include:

- `agent_requests`
  - request id
  - provider
  - model
  - message
  - repo root
  - baseline dirty-file snapshot
  - started timestamp
  - finished timestamp
- `request_commits`
  - request id
  - commit hash
  - repo root
  - linked timestamp

## CLI Requirements

Required commands:

- `agentgit setup`
- `agentgit log`
- `agentgit codex start --model ... --message ...`
- `agentgit codex commit -m ...`
- `agentgit codex finish`

Required extension shape:

- `agentgit gemini start|commit|finish`
- `agentgit claude start|commit|finish`

Optional fallback command:

- `agentgit setup-local`

## Success Criteria

- A developer can install the tool once on a machine and use it across
  repositories.
- A developer can create request-scoped commits through `agentgit`.
- Each `agentgit` commit is linked to a request in the local database.
- The log browser can display commits with linked request metadata without AI
  assistance.
- The TUI supports navigation from commit to file to diff and back.
- Documentation is sufficient for both local developer usage and release
  packaging.

## Risks and Constraints

- Global `core.hooksPath` changes machine-level Git behavior and must be handled
  carefully.
- Dirty working trees require reliable baseline tracking to avoid accidental
  inclusion of unrelated files.
- Side-by-side diff rendering in terminal UIs is constrained by terminal width.
- Global `core.hooksPath` relies on `agentgit` being available on `PATH`, so
  release installation must place a stable binary in a persistent PATH location.

## Current Status

The current implementation covers the initial end-to-end workflow:

- local SQLite schema
- request start, commit, finish flow
- global setup via `agentgit setup`
- local fallback setup via `agentgit setup-local`
- `codex`, `gemini`, and `claude` command surfaces
- commit-to-request hook linking
- TUI and non-TTY log output
- Go binary build and installation documentation

Future work may add Homebrew packages, Debian packages, broader provider
integrations, and more advanced request lifecycle management, but those are
outside this initial PRD.
