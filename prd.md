# PRD: AgentGit

## Problem Definition

In agent-based development, changes not directly authored by the user accumulate rapidly. Users often need to quickly answer the following questions:

- What are the recent commits?
- What are the uncommitted changes?
- What was the sequence of agent requests that led to these changes?
- What files and diffs actually changed in a specific commit?

While standard Git tools are strong at showing commits and diffs, they fail to visualize the "request flow" that led to those changes in a single view. AgentGit connects agent request logs with Git history along a timeline, allowing users to quickly review the results of agent-led work.

## Product Goals

- Users should be able to grasp the recent workflow within 10 seconds of launching.
- The relationship between requests and commits should be easily scannable on a single screen.
- Users can review file lists and diffs for a selected commit or working tree change using only the keyboard.
- Uncommitted changes should be visible within the same task context.

## Non-Goals

- Providing code editing features.
- Replacing the IDE.
- Replacing all Git functionalities.
- Proving absolute causal relationships between requests and commits.

## Target Audience

- Developers modifying code alongside AI agents.
- Users who want to quickly review changes made by agents.
- Users who need to verify actual code reflection results against the request flow.

## Key User Questions

- What were the recent requests in this repository?
- Which of those requests are associated with which commits?
- If multiple requests are linked to the same commit, what was that group of requests?
- What changes occurred after the last commit (uncommitted changes)?
- What are the actual modified files and diffs in a specific commit?

## Key User Flows

### Flow 1: Checking Recent Workflow in the Request Graph

1. The user launches the TUI.
2. The first screen displays a list of requests and their associated commits/changesets.
3. Consecutive requests sharing the same set of commits are read as a single block.
4. Unlinked commits are displayed separately or integrated into the timeline.

Success Criteria:
- The relationship between recent requests and commits is immediately clear on the first screen.
- The flow remains readable even when multiple requests point to the same commit.

### Flow 2: Reviewing Commit or Working Tree Changes

1. The user selects a commit or working tree entry from the graph.
2. The user views the file list for that changeset.
3. The user selects a file to view its diff.

Success Criteria:
- The file list appears quickly after selecting a changeset.
- No need to switch to other tools to verify the diff.

### Flow 3: Verifying Working Tree State Based on Requests

1. The user selects the uncommitted changes entry.
2. Requests that occurred after the last commit are displayed alongside.
3. The user reviews uncommitted files and their diffs.

Success Criteria:
- Uncommitted changes can be understood within the context of recent agent requests.

## Functional Requirements

### Essential

- Automatically detect the repository root.
- Load uncommitted changes and recent commit history.
- Display the Request Graph as the initial screen.
- Parse Claude, Codex, and Gemini request logs, filtering for those relevant to the current repository.
- Show the file list for the selected changeset.
- Show the diff for the selected file.
- Support refreshing the view.
- Display Git command errors at the bottom of the screen.

### Optional

- Separate staged vs. unstaged changes.
- Search functionality.
- Request filtering.
- Toggle visibility by provider.
- Advanced diff syntax highlighting.
- Improved handling of binary files.

## Request-Commit Linking Rules

Linking in this product is "timeline-based" and does not imply direct causality.

Rules:
- Requests are extracted from session logs.
- Requests are filtered by comparing the repository path with the session's `cwd`.
- Each commit is linked to requests that occurred between the previous commit and the current one.
- The first commit of a session links to requests from the session start to that commit.
- Working tree entries link to requests occurring after the last commit.
- Consecutive requests sharing the same changeset are grouped in the graph.

## UI Requirements

The app uses a step-based single-screen transition structure rather than a multi-pane layout.

- **Screen 1: Request Graph (Change Sets)**
- **Screen 2: Files**
- **Screen 3: Diff**

Detailed Requirements:
- The first screen focuses on the relationship between requests and commits.
- File and diff screens should optionally show context from the selected changeset.
- Selectable items and non-selectable request lines must be visually distinct.
- Full navigation must be possible using only the keyboard.

## State Transitions

- On startup: Load repo root, changesets, and initial file/diff data.
- Selecting a changeset: Load and display the file list.
- Selecting a file: Load and display the diff.
- Refresh: Maintain current selection state where possible.

## Data Model

### ChangeSet
```text
id
type: uncommitted | commit
title
commit_hash?
author?
timestamp?
summary?
file_count
requests[]
```

### LinkedRequest
```text
id
provider
session_id
text
timestamp
commit_count
commit_ids[]
```

### ChangedFile
```text
path
old_path?
status: added | modified | deleted | renamed | copied | unknown
additions
deletions
```

### FileDiff
```text
path
patch
is_binary
too_large
```

## Error Handling

- Show a clear error if not in a Git repository.
- Display stderr messages for failed Git commands.
- Truncate large diffs and indicate truncation.
- Explicitly state binary status for binary files instead of showing raw patches.

## Performance Requirements

- Initial rendering should be near-instant for typical repositories.
- File list updates when switching changesets should have no perceptible lag.
- Opening text diffs should be responsive.
- UI should remain responsive even with large diffs.

## Current Open Issues

- Whether to display automatic injection prompts (e.g., `AGENTS.md instructions`) as regular requests.
- Refining the "linked" terminology for better accuracy.
- Default number of commits to load.
- Need for provider-based filtering or hiding.
- UI for request search or folding.
