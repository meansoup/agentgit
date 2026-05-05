# Wireframe

## Basic Layout

### Screen 1: Change Sets (Request Graph)

Shows the timeline of agent requests and the resulting commits or uncommitted changes.

```text
+--------------------------------------------------------------------------------------------+
| AgentGit         | /path/to/repo | ready                                      Change Sets  |
+--------------------------------------------------------------------------------------------+
| ○ 05-05 14:20 [Gemini] "Refactor the TUI layout to use steps"                              |
| └─ ● a1b2c3d4 05-05 14:21 Implement step-based screen transitions                          |
|                                                                                            |
| ○ 05-05 14:25 [Claude] "Add support for parsing Codex logs"                                |
| └─ ● e5f6g7h8 05-05 14:26 Add internal/log/codex.go and update linker                      |
|                                                                                            |
| ○ 05-05 14:30 [Gemini] "Fix bug in diff truncation logic"                                  |
| > └─ ○ (uncommitted) Fix edge case in safeRepeat function                                  |
+--------------------------------------------------------------------------------------------+
| up/down move | enter/right open | esc/left back | r refresh | q quit                     |
+--------------------------------------------------------------------------------------------+
```

### Screen 2: Files

Shows the list of modified files for the selected changeset.

```text
+--------------------------------------------------------------------------------------------+
| AgentGit         | /path/to/repo | ready                                            Files  |
+--------------------------------------------------------------------------------------------+
| Files | a1b2c3d4 | Implement step-based screen transitions                                 |
|--------------------------------------------------------------------------------------------|
| > internal/tui/app.go                                                                      |
|   [M] +120 -45                                                                             |
|                                                                                            |
|   internal/tui/graph.go                                                                    |
|   [M] +35 -10                                                                              |
+--------------------------------------------------------------------------------------------+
| up/down move | enter/right open | esc/left back | r refresh | q quit                     |
+--------------------------------------------------------------------------------------------+
```

### Screen 3: Diff

Shows the detailed unified diff for the selected file.

```text
+--------------------------------------------------------------------------------------------+
| AgentGit         | /path/to/repo | ready                                             Diff  |
+--------------------------------------------------------------------------------------------+
| Diff | a1b2c3d4 | internal/tui/app.go                                                      |
|--------------------------------------------------------------------------------------------|
| diff --git a/internal/tui/app.go b/internal/tui/app.go                                      |
| index 89abcde..1234567 100644                                                              |
| --- a/internal/tui/app.go                                                                  |
| +++ b/internal/tui/app.go                                                                  |
| @@ -10,6 +10,15 @@                                                                         |
|  const (                                                                                   |
| -    screenMain = iota                                                                     |
| +    screenGraph = iota                                                                    |
| +    screenFiles                                                                           |
| +    screenDiff                                                                            |
|  )                                                                                         |
+--------------------------------------------------------------------------------------------+
| up/down scroll | esc/left back | q quit                                                   |
+--------------------------------------------------------------------------------------------+
```

## Screen Roles

- **Screen 1 (Change Sets):** Focuses on exploring units of changes (commits/working tree) within the context of agent requests.
- **Screen 2 (Files):** Focuses on exploring individual files within the selected unit of change.
- **Screen 3 (Diff):** Focuses on read-only diff inspection.

## Navigation Rules

- `Enter` or `Right`: Move to the next screen (drill down).
- `Esc` or `Left`: Move to the previous screen (go up).
- `r`: Refresh the data from the repository and logs.
- `q`: Exit the application.

## Empty States

### Not a Git Repository
The app should exit with an error message if run outside a repository.
```text
Error: not a git repository
agentgit must be run from within a git repository or pointing to one
```

### No Changes
If there are no recent requests or commits, Screen 1 should still show the structure but with minimal entries.
```text
Change Sets
> └─ ○ (uncommitted) 
```

File Screen:
```text
No changed files
```

Diff Screen:
```text
No diff available
```
