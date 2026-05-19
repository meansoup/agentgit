package state

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/minkuik/agentgit/internal/git"
)

func Dir(repoRoot string) (string, error) {
	gitDir, err := git.GitDir(repoRoot)
	if err != nil {
		return "", err
	}
	path := filepath.Join(gitDir, "agentgit")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func ActiveRequestFile(repoRoot string) (string, error) {
	dir, err := Dir(repoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "active-request"), nil
}

func SetActiveRequest(repoRoot string, requestID int64) error {
	path, err := ActiveRequestFile(repoRoot)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.FormatInt(requestID, 10)+"\n"), 0o644)
}

func GetActiveRequest(repoRoot string) (int64, bool, error) {
	path, err := ActiveRequestFile(repoRoot)
	if err != nil {
		return 0, false, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return 0, false, nil
	}
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func ClearActiveRequest(repoRoot string) error {
	path, err := ActiveRequestFile(repoRoot)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
