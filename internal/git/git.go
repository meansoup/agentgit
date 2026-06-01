package git

import (
	"bytes"
	"errors"
	"fmt"
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
	out, err := RunBytes(cwd, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func RunBytes(cwd string, args ...string) ([]byte, error) {
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
		return nil, errors.New(msg)
	}
	return stdout.Bytes(), nil
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

func Head(root string) (string, error) {
	out, err := Run(root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func CommitsAfter(root string, afterHash string) ([]string, error) {
	if afterHash == "" {
		return nil, nil
	}
	out, err := Run(root, "rev-list", "--reverse", afterHash+"..HEAD")
	if err != nil {
		return nil, err
	}
	var hashes []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			hashes = append(hashes, line)
		}
	}
	return hashes, nil
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
	out, err := Run(root, "show", "--format=", "--no-ext-diff", "--unified=999999", commitHash, "--", path)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
}

func CatFile(root string, commitHash string, path string) (string, error) {
	out, err := CatFileBytes(root, commitHash, path)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func CatFileBytes(root string, commitHash string, path string) ([]byte, error) {
	return RunBytes(root, "show", commitHash+":"+path)
}
