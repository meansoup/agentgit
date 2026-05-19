package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/state"
	"github.com/minkuik/agentgit/internal/store"
)

func GlobalHooksDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".config", "agentgit", "hooks")
	}
	return filepath.Join(home, ".config", "agentgit", "hooks")
}

func InstallGlobal() (string, error) {
	hooksDir := GlobalHooksDir()
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", err
	}
	previous := strings.TrimSpace(git.RunAllowError("", "config", "--global", "--get", "core.hooksPath"))
	var previousDir string
	if previous != "" {
		previousDir = expandHome(previous)
		if !filepath.IsAbs(previousDir) {
			abs, err := filepath.Abs(previousDir)
			if err == nil {
				previousDir = abs
			}
		}
	}
	if previousDir == hooksDir {
		previousDir = ""
	}
	hook := filepath.Join(hooksDir, "post-commit")
	if err := installHookFile(hook, previousDir); err != nil {
		return "", err
	}
	if _, err := git.Run("", "config", "--global", "core.hooksPath", hooksDir); err != nil {
		return "", err
	}
	return hook, nil
}

func InstallLocal(repoRoot string) (string, error) {
	gitDir, err := git.GitDir(repoRoot)
	if err != nil {
		return "", err
	}
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", err
	}
	hook := filepath.Join(hooksDir, "post-commit")
	if err := installHookFile(hook, ""); err != nil {
		return "", err
	}
	return hook, nil
}

func HandlePostCommit() error {
	root, err := git.RepoRoot("")
	if err != nil {
		return err
	}
	requestID, ok, err := state.GetActiveRequest(root)
	if err != nil || !ok {
		return err
	}
	out, err := git.Run(root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	return store.LinkCommit(requestID, strings.TrimSpace(out), root)
}

func installHookFile(hookPath string, previousHooksDir string) error {
	existing, _ := os.ReadFile(hookPath)
	if len(existing) > 0 && !strings.Contains(string(existing), "agentgit hook post-commit") {
		backup := hookPath + ".agentgit-backup"
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if err := os.WriteFile(backup, existing, 0o755); err != nil {
				return err
			}
		}
		content := strings.TrimRight(string(existing), "\n") + "\n\n# agentgit\n" + hookSnippet(previousHooksDir)
		if err := os.WriteFile(hookPath, []byte(content), 0o755); err != nil {
			return err
		}
		return os.Chmod(hookPath, 0o755)
	}
	content := "#!/bin/sh\n# Installed by agentgit.\n" + hookSnippet(previousHooksDir)
	if err := os.WriteFile(hookPath, []byte(content), 0o755); err != nil {
		return err
	}
	return os.Chmod(hookPath, 0o755)
}

func hookSnippet(previousHooksDir string) string {
	lines := []string{
		"if command -v agentgit >/dev/null 2>&1; then",
		"  agentgit hook post-commit",
		"fi",
	}
	if previousHooksDir != "" {
		previous := filepath.Join(previousHooksDir, "post-commit")
		lines = append(lines,
			"",
			fmt.Sprintf("if [ -x '%s' ]; then", previous),
			fmt.Sprintf("  '%s' \"$@\"", previous),
			"fi",
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func expandHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
