package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Commit struct {
	Hash      string
	ShortHash string
	Date      string
	Subject   string
}

func Run(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

func RunAllowError(cwd string, args ...string) string {
	out, _ := Run(cwd, args...)
	return out
}

func RepoRoot(cwd string) (string, error) {
	out, err := Run(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Abs(strings.TrimSpace(out))
}

func GitDir(root string) (string, error) {
	out, err := Run(root, "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(out)
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Abs(filepath.Join(root, path))
}

func StatusPaths(root string) (map[string]bool, error) {
	out, err := Run(root, "status", "--porcelain=v1", "-z")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(out, "\x00")
	paths := map[string]bool{}
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if entry == "" {
			continue
		}
		if len(entry) < 4 {
			continue
		}
		status := entry[:2]
		path := entry[3:]
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			i++
			if i < len(parts) {
				path = parts[i]
			}
		}
		if path != "" {
			paths[path] = true
		}
	}
	return paths, nil
}

func Commits(root string, limit int) ([]Commit, error) {
	format := "%H%x1f%h%x1f%ad%x1f%s"
	out, err := Run(root, "log", fmt.Sprintf("--max-count=%d", limit), "--date=format:%m-%d %H:%M", "--pretty=format:"+format)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 4)
		if len(parts) != 4 {
			continue
		}
		commits = append(commits, Commit{
			Hash:      parts[0],
			ShortHash: parts[1],
			Date:      parts[2],
			Subject:   parts[3],
		})
	}
	return commits, nil
}

func ChangedFiles(root string, commitHash string) ([]string, error) {
	out, err := Run(root, "show", "--pretty=format:", "--name-only", commitHash)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func UnifiedDiff(root string, commitHash string, path string) ([]string, error) {
	out, err := Run(root, "show", "--format=", "--no-ext-diff", commitHash, "--", path)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
}

func CommitPaths(root string, paths map[string]bool, message string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("no request-owned file changes to commit")
	}
	var existing []string
	var deleted []string
	for path := range paths {
		if _, err := os.Stat(filepath.Join(root, path)); err == nil {
			existing = append(existing, path)
		} else if os.IsNotExist(err) {
			out := RunAllowError(root, "ls-files", "--deleted", "--", path)
			if strings.TrimSpace(out) != "" {
				deleted = append(deleted, path)
			}
		} else {
			return "", err
		}
	}
	if len(existing) > 0 {
		args := append([]string{"add", "--"}, existing...)
		if _, err := Run(root, args...); err != nil {
			return "", err
		}
	}
	if len(deleted) > 0 {
		args := append([]string{"add", "-u", "--"}, deleted...)
		if _, err := Run(root, args...); err != nil {
			return "", err
		}
	}
	if _, err := Run(root, "commit", "-m", message); err != nil {
		return "", err
	}
	out, err := Run(root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
