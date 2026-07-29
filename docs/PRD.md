# Product Requirements Document

## Product

`agentgit`

## Summary

`agentgit` is a read-only Go CLI/TUI for browsing Git history and local AI-agent
transcripts. It runs with `agentgit` only. It does not require setup, lifecycle
hooks, global Git configuration, or background capture.

Everything shown in the UI must come from facts already present in Git or in
agent transcript files. Request-to-commit linking by timestamp or other
approximation is out of scope.

## Command Model

```sh
agentgit [--limit 500] [path]
agentgit version
```

`agentgit [path]` opens the TUI or static Git history view for the repository
containing the current path, directory path, or file path.

There are no `setup` or `hook` commands.

## Goals

- Work immediately after placing the binary in `PATH`.
- Keep the Git browser for commit, directory, file, image, and diff views.
- Show a bottom Request drawer sourced from local transcripts.
- Support Claude, Codex, and Gemini transcript locations.
- Display only transcript-confirmed request facts: prompt text, session/turn,
  timestamp, model, agent, and explicitly recorded edited files when present.
- Stay read-only: no hook install, no global config mutation, no Git mutation,
  no automatic commits, and no request database writes.

## Transcript Sources

- Claude: `~/.claude/projects/<cwd-escaped>/*.jsonl`
- Codex: `~/.codex/sessions/**/*.jsonl`
- Gemini: session logs under `~/.gemini`

The parser must include a request only when the transcript source identifies
the current repository path or project mapping deterministically.

## TUI Requirements

- `Tab`: toggle Commit and Directory views.
- `v`: toggle the bottom Request drawer.
- Drawer closed: arrow keys control the Git view.
- Drawer open: arrow keys control the request list.
- Drawer rows show timestamp, agent/model, and the first non-empty prompt line.
- `Enter` in the drawer opens full-screen Request details.
- Request details show full prompt text plus transcript-confirmed edited files.
- `Left`/`Backspace`: details to drawer, then drawer to closed, preserving the
  existing back behavior for Git views.

## Non-Goals

- Installing or managing agent hooks.
- Capturing requests through lifecycle hooks.
- Linking requests to commits.
- Creating commits automatically.
- Rewriting commits or offering remove/squash workflows.
- Inferring request ownership from time proximity, dirty state, or model output.

## Success Criteria

- A user can place the binary in `PATH`, run `agentgit` in a repo, and browse
  Git history with no setup.
- `setup` and `hook` commands do not exist in code or usage text.
- Request rows come from local Claude, Codex, or Gemini transcripts.
- Commits do not display request links.
- The tool remains read-only during browsing.
