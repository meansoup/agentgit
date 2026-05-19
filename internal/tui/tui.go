package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/store"
)

type mode int

const (
	modeCommits mode = iota
	modeFiles
	modeDiff
)

type diffMode int

const (
	diffUnified diffMode = iota
	diffSplit
)

type model struct {
	root      string
	commits   []git.Commit
	links     map[string][]store.LinkedRequest
	files     []string
	diffLines []string
	mode      mode
	diffMode  diffMode
	commitIdx int
	fileIdx   int
	scroll    int
	err       error
}

var (
	hashStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	providerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	requestStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	markerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	fileStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	addStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	hunkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	cursorStyle   = lipgloss.NewStyle().Reverse(true)
	titleStyle    = lipgloss.NewStyle().Bold(true)
)

func Run(root string, limit int) error {
	commits, err := git.Commits(root, limit)
	if err != nil {
		return err
	}
	links, err := store.RequestsByCommit(root)
	if err != nil {
		return err
	}
	if !isTTY(os.Stdout) || !isTTY(os.Stdin) {
		return PrintStatic(os.Stdout, commits, links)
	}
	m := model{root: root, commits: commits, links: links}
	_, err = tea.NewProgram(m).Run()
	return err
}

func PrintStatic(w io.Writer, commits []git.Commit, links map[string][]store.LinkedRequest) error {
	for _, commit := range commits {
		if _, err := fmt.Fprintf(w, "%s %s  %s\n", hashStyle.Render(commit.ShortHash), commit.Date, commit.Subject); err != nil {
			return err
		}
		for _, req := range links[commit.Hash] {
			line := markerStyle.Render("└─ ●") + " " +
				providerStyle.Render(fmt.Sprintf("[%s %s]", req.AgentName, req.Model)) + " " +
				requestStyle.Render(req.Message)
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "right", "l", "enter":
			m.enter()
		case "left", "h", "backspace":
			m.back()
		case "m":
			if m.diffMode == diffUnified {
				m.diffMode = diffSplit
			} else {
				m.diffMode = diffUnified
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return "agentgit: " + m.err.Error() + "\n"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("agentgit %s  diff:%s  q:quit", m.modeName(), m.diffModeName())))
	b.WriteString("\n\n")
	switch m.mode {
	case modeCommits:
		b.WriteString(m.viewCommits())
	case modeFiles:
		b.WriteString(m.viewFiles())
	case modeDiff:
		b.WriteString(m.viewDiff())
	}
	return b.String()
}

func (m *model) move(delta int) {
	switch m.mode {
	case modeCommits:
		m.commitIdx = clamp(m.commitIdx+delta, 0, len(m.commits)-1)
	case modeFiles:
		m.fileIdx = clamp(m.fileIdx+delta, 0, len(m.files)-1)
	case modeDiff:
		m.scroll = max(0, m.scroll+delta)
	}
}

func (m *model) enter() {
	switch m.mode {
	case modeCommits:
		if len(m.commits) == 0 {
			return
		}
		files, err := git.ChangedFiles(m.root, m.commits[m.commitIdx].Hash)
		if err != nil {
			m.err = err
			return
		}
		m.files = files
		m.fileIdx = 0
		m.mode = modeFiles
	case modeFiles:
		if len(m.files) == 0 {
			return
		}
		lines, err := git.UnifiedDiff(m.root, m.commits[m.commitIdx].Hash, m.files[m.fileIdx])
		if err != nil {
			m.err = err
			return
		}
		m.diffLines = lines
		m.scroll = 0
		m.mode = modeDiff
	}
}

func (m *model) back() {
	switch m.mode {
	case modeDiff:
		m.mode = modeFiles
	case modeFiles:
		m.mode = modeCommits
	}
}

func (m model) viewCommits() string {
	var b strings.Builder
	for i, commit := range m.commits {
		line := fmt.Sprintf("%s %s  %s", hashStyle.Render(commit.ShortHash), commit.Date, commit.Subject)
		if i == m.commitIdx {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
		for _, req := range m.links[commit.Hash] {
			b.WriteString(markerStyle.Render("└─ ●"))
			b.WriteByte(' ')
			b.WriteString(providerStyle.Render(fmt.Sprintf("[%s %s]", req.AgentName, req.Model)))
			b.WriteByte(' ')
			b.WriteString(requestStyle.Render(req.Message))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m model) viewFiles() string {
	var b strings.Builder
	if len(m.commits) == 0 {
		return ""
	}
	commit := m.commits[m.commitIdx]
	b.WriteString(hashStyle.Render(commit.ShortHash))
	b.WriteByte(' ')
	b.WriteString(commit.Subject)
	b.WriteString("\n\n")
	for i, file := range m.files {
		line := fileStyle.Render(file)
		if i == m.fileIdx {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) viewDiff() string {
	var b strings.Builder
	if len(m.commits) == 0 || len(m.files) == 0 {
		return ""
	}
	b.WriteString(hashStyle.Render(m.commits[m.commitIdx].ShortHash))
	b.WriteByte(' ')
	b.WriteString(fileStyle.Render(m.files[m.fileIdx]))
	b.WriteString("\n\n")
	lines := m.diffLines
	if m.diffMode == diffSplit {
		lines = splitDiff(lines, 120)
	}
	for _, line := range lines[m.scroll:] {
		b.WriteString(styleDiffLine(line))
		b.WriteByte('\n')
	}
	return b.String()
}

func styleDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return addStyle.Render(line)
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return delStyle.Render(line)
	case strings.HasPrefix(line, "@@"):
		return hunkStyle.Render(line)
	default:
		return line
	}
}

func splitDiff(lines []string, width int) []string {
	leftWidth := max(20, (width-3)/2)
	rightWidth := max(20, width-leftWidth-3)
	var out []string
	var pending *string
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "@@"):
			if pending != nil {
				out = append(out, formatSplit(*pending, "", leftWidth, rightWidth))
				pending = nil
			}
			out = append(out, line)
		case strings.HasPrefix(line, "-"):
			if pending != nil {
				out = append(out, formatSplit(*pending, "", leftWidth, rightWidth))
			}
			text := strings.TrimPrefix(line, "-")
			pending = &text
		case strings.HasPrefix(line, "+"):
			text := strings.TrimPrefix(line, "+")
			if pending != nil {
				out = append(out, formatSplit(*pending, text, leftWidth, rightWidth))
				pending = nil
			} else {
				out = append(out, formatSplit("", text, leftWidth, rightWidth))
			}
		default:
			if pending != nil {
				out = append(out, formatSplit(*pending, "", leftWidth, rightWidth))
				pending = nil
			}
			text := strings.TrimPrefix(line, " ")
			out = append(out, formatSplit(text, text, leftWidth, rightWidth))
		}
	}
	if pending != nil {
		out = append(out, formatSplit(*pending, "", leftWidth, rightWidth))
	}
	return out
}

func formatSplit(left, right string, leftWidth, rightWidth int) string {
	return fmt.Sprintf("%-*.*s │ %-*.*s", leftWidth, leftWidth, left, rightWidth, rightWidth, right)
}

func (m model) modeName() string {
	switch m.mode {
	case modeFiles:
		return "files"
	case modeDiff:
		return "diff"
	default:
		return "commits"
	}
}

func (m model) diffModeName() string {
	if m.diffMode == diffSplit {
		return "split"
	}
	return "unified"
}

func clamp(value, lower, upper int) int {
	if upper < lower {
		return lower
	}
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isTTY(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
