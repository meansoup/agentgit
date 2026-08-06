package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const UncommittedHash = "__agentgit_uncommitted__"

type Commit struct {
	Hash      string
	ShortHash string
	Date      string
	Subject   string
}

type FileChange struct {
	Path    string
	OldPath string
	Status  string
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

func RunCombined(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		if trimmed == "" {
			trimmed = err.Error()
		}
		return trimmed, errors.New(trimmed)
	}
	return trimmed, nil
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

func ShortHead(root string) string {
	head, err := Head(root)
	if err != nil {
		return "none"
	}
	if len(head) <= 8 {
		return head
	}
	return head[:8]
}

func Parent(root string, commitHash string) (string, error) {
	if commitHash == "" || commitHash == UncommittedHash {
		return "", errors.New("commit does not have a parent")
	}
	out, err := Run(root, "rev-parse", commitHash+"^")
	if err != nil {
		return "", errors.New("selected range includes the root commit")
	}
	return strings.TrimSpace(out), nil
}

func Branch(root string) string {
	out, err := Run(root, "branch", "--show-current")
	if err == nil {
		if branch := strings.TrimSpace(out); branch != "" {
			return branch
		}
	}
	head := ShortHead(root)
	if head == "none" {
		return "none"
	}
	return "detached:" + head
}

func GitPath(root string, path string) (string, error) {
	out, err := Run(root, "rev-parse", "--git-path", path)
	if err != nil {
		return "", err
	}
	resolved := strings.TrimSpace(out)
	if filepath.IsAbs(resolved) {
		return resolved, nil
	}
	return filepath.Join(root, resolved), nil
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
	paths := map[string]bool{}
	for _, change := range parseStatusFileChanges(out) {
		if change.Path != "" {
			paths[change.Path] = true
		}
	}
	return paths, nil
}

func IsWorkingTreeClean(root string) (bool, error) {
	out, err := Run(root, "status", "--porcelain=v1")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

func Push(root string) (string, error) {
	return RunCombined(root, "push")
}

func DeleteMergedBranches(root string) ([]string, string, error) {
	out, err := Run(root, "branch", "--merged")
	if err != nil {
		return nil, "", err
	}
	branches := mergedBranchesToDelete(out)
	if len(branches) == 0 {
		return nil, "", nil
	}
	args := append([]string{"branch", "-d"}, branches...)
	deleteOut, err := RunCombined(root, args...)
	if err != nil {
		return branches, deleteOut, err
	}
	return branches, deleteOut, nil
}

func mergedBranchesToDelete(out string) []string {
	protected := map[string]bool{
		"main":    true,
		"master":  true,
		"develop": true,
		"dev":     true,
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		if protected[line] {
			continue
		}
		branches = append(branches, line)
	}
	sort.Strings(branches)
	return branches
}

func Commits(root string, limit int) ([]Commit, error) {
	format := "%H%x1f%h%x1f%ad%x1f%s"
	out, err := Run(root, "log", fmt.Sprintf("--max-count=%d", limit), "--abbrev=8", "--date=format:%m-%d %H:%M", "--pretty=format:"+format)
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

func CommitsWithUncommitted(root string, limit int) ([]Commit, error) {
	commits, err := Commits(root, limit)
	if err != nil {
		return nil, err
	}
	files, err := UncommittedFiles(root)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return commits, nil
	}
	uncommitted := Commit{
		Hash:      UncommittedHash,
		ShortHash: "uncommitted",
		Date:      "working tree",
		Subject:   fmt.Sprintf("Uncommitted files (%d)", len(files)),
	}
	return append([]Commit{uncommitted}, commits...), nil
}

func ResetHard(root string, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return errors.New("reset target is empty")
	}
	_, err := Run(root, "reset", "--hard", ref)
	return err
}

func DiscardUncommitted(root string) error {
	if _, err := Run(root, "reset", "--hard", "HEAD"); err != nil {
		return err
	}
	_, err := Run(root, "clean", "-fd")
	return err
}

func SquashSince(root string, baseRef string, message []string) (string, error) {
	if strings.TrimSpace(baseRef) == "" {
		return "", errors.New("squash base is empty")
	}
	if len(message) == 0 || strings.TrimSpace(message[0]) == "" {
		return "", errors.New("squash commit message is empty")
	}
	tree, err := Run(root, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", strings.TrimSpace(tree), "-p", baseRef}
	for _, part := range message {
		args = append(args, "-m", part)
	}
	out, err := Run(root, args...)
	if err != nil {
		return "", err
	}
	newHash := strings.TrimSpace(out)
	if err := ResetHard(root, newHash); err != nil {
		return "", err
	}
	return newHash, nil
}

func ChangedFiles(root string, commitHash string) ([]string, error) {
	changes, err := ChangedFileChanges(root, commitHash)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(changes))
	for _, change := range changes {
		if change.Path != "" {
			files = append(files, change.Path)
		}
	}
	return files, nil
}

func ChangedFileChanges(root string, commitHash string) ([]FileChange, error) {
	if commitHash == UncommittedHash {
		out, err := Run(root, "status", "--porcelain=v1", "-z")
		if err != nil {
			return nil, err
		}
		return parseStatusFileChanges(out), nil
	}
	out, err := RunBytes(root, "show", "--pretty=format:", "--name-status", "-M", "-z", commitHash)
	if err != nil {
		return nil, err
	}
	return parseNameStatusFileChanges(out), nil
}

func ChangedFileStatuses(root string, commitHash string) (map[string]string, error) {
	changes, err := ChangedFileChanges(root, commitHash)
	if err != nil {
		return nil, err
	}
	statuses := make(map[string]string, len(changes))
	for _, change := range changes {
		if change.Path != "" {
			statuses[change.Path] = change.Status
		}
	}
	return statuses, nil
}

func parseNameStatusFileChanges(out []byte) []FileChange {
	var changes []FileChange
	parts := bytes.Split(out, []byte{0})
	for i := 0; i < len(parts); i++ {
		status := string(parts[i])
		if status == "" {
			continue
		}
		if i+1 >= len(parts) {
			break
		}
		path := string(parts[i+1])
		oldPath := ""
		i++
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i+1 >= len(parts) {
				break
			}
			oldPath = path
			path = string(parts[i+1])
			i++
		}
		if path != "" {
			changes = append(changes, FileChange{
				Path:    path,
				OldPath: oldPath,
				Status:  statusKind(status),
			})
		}
	}
	return changes
}

func parseStatusFileChanges(out string) []FileChange {
	parts := strings.Split(out, "\x00")
	changes := make([]FileChange, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if entry == "" || len(entry) < 4 {
			continue
		}
		status := entry[:2]
		path := entry[3:]
		oldPath := ""
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			i++
			if i < len(parts) {
				oldPath = parts[i]
			}
		}
		if path != "" {
			changes = append(changes, FileChange{
				Path:    path,
				OldPath: oldPath,
				Status:  statusKind(status),
			})
		}
	}
	return changes
}

func UncommittedFiles(root string) ([]string, error) {
	paths, err := StatusPaths(root)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(paths))
	for path := range paths {
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

func UncommittedFileStatuses(root string) (map[string]string, error) {
	statuses, err := ChangedFileStatuses(root, UncommittedHash)
	if err != nil {
		return nil, err
	}
	return statuses, nil
}

func statusKind(status string) string {
	if strings.HasPrefix(status, "R") {
		return "renamed"
	}
	if strings.Contains(status, "D") {
		return "deleted"
	}
	if strings.Contains(status, "A") || strings.Contains(status, "?") || strings.Contains(status, "C") {
		return "created"
	}
	return "updated"
}

func WorktreeFiles(root string) ([]string, error) {
	out, err := RunBytes(root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, raw := range bytes.Split(out, []byte{0}) {
		path := string(raw)
		if path == "" || seen[path] {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || info.IsDir() {
			continue
		}
		seen[path] = true
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

func UnifiedDiff(root string, commitHash string, path string) ([]string, error) {
	if commitHash == UncommittedHash {
		return UncommittedDiff(root, path)
	}
	paths := []string{path}
	if change, ok, err := renamedFileChange(root, commitHash, path); err != nil {
		return nil, err
	} else if ok {
		paths = []string{change.OldPath, change.Path}
	}
	args := append([]string{"show", "--format=", "--no-ext-diff", "-M", "--unified=999999", commitHash, "--"}, paths...)
	out, err := Run(root, args...)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
}

func UncommittedDiff(root string, path string) ([]string, error) {
	if change, ok, err := renamedFileChange(root, UncommittedHash, path); err != nil {
		return nil, err
	} else if ok {
		out, err := Run(root, "diff", "--no-ext-diff", "-M", "--unified=999999", "HEAD", "--", change.OldPath, change.Path)
		if err != nil {
			return nil, err
		}
		return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
	}
	if isTracked(root, path) {
		out, err := Run(root, "diff", "--no-ext-diff", "--unified=999999", "HEAD", "--", path)
		if err != nil {
			return nil, err
		}
		return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return nil, err
	}
	if bytes.Contains(data, []byte{0}) {
		return []string{
			"diff --git a/" + path + " b/" + path,
			"new file mode 100644",
			"Binary file /dev/null and b/" + path + " differ",
		}, nil
	}
	content := strings.TrimRight(string(data), "\n")
	body := []string{}
	if content != "" {
		for _, line := range strings.Split(content, "\n") {
			body = append(body, "+"+line)
		}
	}
	diff := []string{
		"diff --git a/" + path + " b/" + path,
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/" + path,
		fmt.Sprintf("@@ -0,0 +1,%d @@", len(body)),
	}
	return append(diff, body...), nil
}

func renamedFileChange(root string, commitHash string, path string) (FileChange, bool, error) {
	changes, err := ChangedFileChanges(root, commitHash)
	if err != nil {
		return FileChange{}, false, err
	}
	for _, change := range changes {
		if change.Path == path && change.Status == "renamed" && change.OldPath != "" {
			return change, true, nil
		}
	}
	return FileChange{}, false, nil
}

func isTracked(root string, path string) bool {
	_, err := Run(root, "ls-files", "--error-unmatch", "--", path)
	return err == nil
}

func CatFile(root string, commitHash string, path string) (string, error) {
	out, err := CatFileBytes(root, commitHash, path)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func CatFileBytes(root string, commitHash string, path string) ([]byte, error) {
	if commitHash == UncommittedHash {
		return os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	}
	return RunBytes(root, "show", commitHash+":"+path)
}
