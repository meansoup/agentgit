package terminal

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/minkuik/agentgit/internal/git"
)

type gitStatusState struct {
	Branch      string
	HasUpstream bool
	Ahead       int
	Behind      int
	DirtyFiles  int
	Additions   int
	Deletions   int
	Err         string
}

func (s gitStatusState) String() string {
	if s.Err != "" {
		return "git status unavailable: " + s.Err
	}
	branch := emptyFallback(s.Branch, "unknown")
	server := "server none"
	if s.HasUpstream {
		server = fmt.Sprintf("server ↑%d ↓%d", s.Ahead, s.Behind)
	}
	dirty := "clean"
	if s.DirtyFiles > 0 {
		dirty = fmt.Sprintf("dirty %d files +%d -%d", s.DirtyFiles, s.Additions, s.Deletions)
	}
	return fmt.Sprintf("%s | %s | %s", branch, server, dirty)
}

func loadGitStatus(root string) gitStatusState {
	state := gitStatusState{Branch: git.Branch(root)}
	if err := fillAheadBehind(root, &state); err != nil {
		state.HasUpstream = false
	}
	if err := fillDirtyStats(root, &state); err != nil {
		state.Err = err.Error()
	}
	return state
}

func fillAheadBehind(root string, state *gitStatusState) error {
	out, err := git.Run(root, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if err != nil {
		return err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return fmt.Errorf("unexpected upstream count: %q", strings.TrimSpace(out))
	}
	behind, err := strconv.Atoi(fields[0])
	if err != nil {
		return err
	}
	ahead, err := strconv.Atoi(fields[1])
	if err != nil {
		return err
	}
	state.HasUpstream = true
	state.Ahead = ahead
	state.Behind = behind
	return nil
}

func fillDirtyStats(root string, state *gitStatusState) error {
	paths, err := git.StatusPaths(root)
	if err != nil {
		return err
	}
	state.DirtyFiles = len(paths)
	if state.DirtyFiles == 0 {
		return nil
	}
	if err := fillTrackedLineStats(root, state); err != nil {
		return err
	}
	return fillUntrackedLineStats(root, state)
}

func fillTrackedLineStats(root string, state *gitStatusState) error {
	out, err := git.Run(root, "diff", "--numstat", "HEAD", "--")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		additions, addOK := parseNumstatCount(fields[0])
		deletions, delOK := parseNumstatCount(fields[1])
		if addOK {
			state.Additions += additions
		}
		if delOK {
			state.Deletions += deletions
		}
	}
	return nil
}

func fillUntrackedLineStats(root string, state *gitStatusState) error {
	out, err := git.RunBytes(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	for _, path := range bytes.Split(out, []byte{0}) {
		if len(path) == 0 {
			continue
		}
		lines, ok := countTextFileLines(filepath.Join(root, string(path)))
		if ok {
			state.Additions += lines
		}
	}
	return nil
}

func parseNumstatCount(value string) (int, bool) {
	if value == "-" {
		return 0, false
	}
	count, err := strconv.Atoi(value)
	return count, err == nil
}

func countTextFileLines(path string) (int, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 5*1024*1024 {
		return 0, false
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		return 0, false
	}
	return lines, true
}
