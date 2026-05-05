package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/minkuik/agentgit/internal/model"
)

// FindRoot finds the git repository root
func FindRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to find git root: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

// LoadCommits loads the most recent n commits
func LoadCommits(n int) ([]model.Commit, error) {
	format := "%H%n%an%n%ai%n%s%n%b%n---COMMIT_END---"
	cmd := exec.Command("git", "log", fmt.Sprintf("-%d", n), "--format="+format)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to load commits: %w", err)
	}

	var commits []model.Commit
	parts := strings.Split(out.String(), "---COMMIT_END---\n")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		lines := strings.Split(part, "\n")
		if len(lines) < 4 {
			continue
		}

		hash := lines[0]
		author := lines[1]
		timestamp, err := time.Parse("2006-01-02 15:04:05 -0700", lines[2])
		if err != nil {
			timestamp = time.Now()
		} else {
			timestamp = timestamp.UTC()
		}
		title := lines[3]
		body := ""
		if len(lines) > 4 {
			body = strings.Join(lines[4:], "\n")
		}

		commits = append(commits, model.Commit{
			Hash:      hash,
			Author:    author,
			Timestamp: timestamp,
			Title:     title,
			Body:      body,
		})
	}

	return commits, nil
}

// WorkingTreeFiles lists files with uncommitted changes
func WorkingTreeFiles() ([]model.ChangedFile, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to get working tree status: %w", err)
	}

	var files []model.ChangedFile
	lines := strings.Split(out.String(), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}

		status := line[:2]
		path := strings.TrimSpace(line[3:])

		statusCode := "unknown"
		switch status {
		case " M", "M ", "MM":
			statusCode = "modified"
		case " A", "A ", "AA":
			statusCode = "added"
		case " D", "D ":
			statusCode = "deleted"
		case " R", "R ":
			statusCode = "renamed"
		case " C", "C ":
			statusCode = "copied"
		case "??":
			statusCode = "unknown"
		}

		files = append(files, model.ChangedFile{
			Path:   path,
			Status: statusCode,
		})
	}

	return files, nil
}

// CommitFiles lists files changed in a commit
func CommitFiles(hash string) ([]model.ChangedFile, error) {
	cmd := exec.Command("git", "diff-tree", "--name-status", "-r", hash)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to get commit files: %w", err)
	}

	var files []model.ChangedFile
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		status := parts[0]
		path := parts[len(parts)-1]
		oldPath := ""
		if len(parts) > 2 && (status == "R" || status == "C") {
			oldPath = parts[1]
		}

		files = append(files, model.ChangedFile{
			Path:    path,
			OldPath: oldPath,
			Status:  statusCodeToString(status),
		})
	}

	return files, nil
}

// FileDiff gets the diff for a file in a commit
func FileDiff(hash, path string) (model.FileDiff, error) {
	parentHash := hash + "^"
	cmd := exec.Command("git", "diff", parentHash, hash, "--", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil && !strings.Contains(err.Error(), "exit status 1") {
		return model.FileDiff{}, fmt.Errorf("failed to get diff: %w", err)
	}

	patch := out.String()
	isBinary := strings.Contains(patch, "Binary files")
	tooLarge := len(patch) > 100000

	return model.FileDiff{
		Path:     path,
		Patch:    patch,
		IsBinary: isBinary,
		TooLarge: tooLarge,
	}, nil
}

// WorkingTreeDiff gets the diff for a file in the working tree
func WorkingTreeDiff(path string) (model.FileDiff, error) {
	cmd := exec.Command("git", "diff", "HEAD", "--", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil && !strings.Contains(err.Error(), "exit status 1") {
		return model.FileDiff{}, fmt.Errorf("failed to get working tree diff: %w", err)
	}

	patch := out.String()
	isBinary := strings.Contains(patch, "Binary files")
	tooLarge := len(patch) > 100000

	return model.FileDiff{
		Path:     path,
		Patch:    patch,
		IsBinary: isBinary,
		TooLarge: tooLarge,
	}, nil
}

// GetCommitChanges returns detailed change statistics for a commit
func GetCommitChanges(hash string) (additions, deletions int, err error) {
	cmd := exec.Command("git", "diff", hash+"^", hash, "--numstat")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil && !strings.Contains(err.Error(), "exit status 1") {
		return 0, 0, fmt.Errorf("failed to get commit changes: %w", err)
	}

	for _, line := range strings.Split(out.String(), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			if add, err := strconv.Atoi(parts[0]); err == nil {
				additions += add
			}
			if del, err := strconv.Atoi(parts[1]); err == nil {
				deletions += del
			}
		}
	}

	return
}

// GetFileContent retrieves the content of a file at a specific commit
func GetFileContent(hash, path string) (string, error) {
	cmd := exec.Command("git", "show", fmt.Sprintf("%s:%s", hash, path))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get file content: %w", err)
	}
	return out.String(), nil
}

// GetWorkingTreeFileContent retrieves the content of a file from the working tree
func GetWorkingTreeFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read working tree file: %w", err)
	}
	return string(data), nil
}

func statusCodeToString(code string) string {
	switch code {
	case "M":
		return "modified"
	case "A":
		return "added"
	case "D":
		return "deleted"
	case "R":
		return "renamed"
	case "C":
		return "copied"
	default:
		return "unknown"
	}
}

// GetCurrentDir returns the current working directory
func GetCurrentDir() string {
	dir, _ := os.Getwd()
	return dir
}

// IsGitRepository checks if the current directory is a git repository
func IsGitRepository() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// GetRelativePath returns the relative path from git root
func GetRelativePath(fullPath string) (string, error) {
	root, err := FindRoot()
	if err != nil {
		return "", err
	}
	return filepath.Rel(root, fullPath)
}
