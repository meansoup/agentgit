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
	fileCache map[string][]string
	diffCache map[string][]string
	mode      mode
	diffMode  diffMode
	commitIdx int
	fileIdx   int
	scroll    int
	width     int
	height    int
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
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8")).Padding(0, 1)
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8")).Padding(0, 1)
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
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
	m := model{root: root, commits: commits, links: links, fileCache: map[string][]string{}, diffCache: map[string][]string{}}
	m.loadCommitFiles()
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
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
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
		case "n":
			m.jumpHunk(1)
		case "p":
			m.jumpHunk(-1)
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return "agentgit: " + m.err.Error() + "\n"
	}
	content, focusLine := m.contentView()
	if m.width <= 0 || m.height <= 0 {
		return titleStyle.Render(fmt.Sprintf("agentgit %s  diff:%s  n/p:hunk  q:quit", m.modeName(), m.diffModeName())) + "\n\n" + content
	}
	return m.viewFrame(content, focusLine)
}

func (m model) contentView() (string, int) {
	switch m.mode {
	case modeCommits:
		return m.viewCommits(), m.commitFocusLine()
	case modeFiles:
		return m.viewFiles(), m.fileFocusLine()
	case modeDiff:
		return m.viewDiff(), 2
	default:
		return "", 0
	}
}

func (m model) viewFrame(content string, focusLine int) string {
	header := headerStyle.Width(m.width).Render(fmt.Sprintf("agentgit  %s  diff:%s", m.modeName(), m.diffModeName()))
	footer := footerStyle.Width(m.width).Render(m.helpText())
	bodyHeight := max(0, m.height-lipgloss.Height(header)-lipgloss.Height(footer))
	body := m.viewBody(content, bodyHeight, focusLine)
	if body == "" {
		return header + "\n" + footer
	}
	return header + "\n" + body + "\n" + footer
}

func (m model) viewBody(content string, height int, focusLine int) string {
	if height <= 0 {
		return ""
	}
	lines := splitViewLines(content)
	start := 0
	if len(lines) > height {
		start = clamp(focusLine-height/2, 0, len(lines)-height)
	}
	end := min(len(lines), start+height)
	visible := lines[start:end]
	for len(visible) < height {
		visible = append(visible, "")
	}
	for i, line := range visible {
		visible[i] = padPlain(line, m.width)
	}
	return strings.Join(visible, "\n")
}

func (m model) helpText() string {
	switch m.mode {
	case modeCommits:
		return "j/k move  enter/l files  q quit"
	case modeFiles:
		return "j/k move  enter/l diff  h back  q quit"
	case modeDiff:
		return "j/k scroll  n/p hunk  m split/unified  h back  q quit"
	default:
		return "q quit"
	}
}

func (m *model) move(delta int) {
	switch m.mode {
	case modeCommits:
		m.commitIdx = clamp(m.commitIdx+delta, 0, len(m.commits)-1)
		m.loadCommitFiles()
	case modeFiles:
		m.fileIdx = clamp(m.fileIdx+delta, 0, len(m.files)-1)
		m.loadSelectedDiff()
	case modeDiff:
		m.scroll = max(0, m.scroll+delta)
	}
}

func (m *model) jumpHunk(delta int) {
	if m.mode != modeDiff || len(m.diffLines) == 0 {
		return
	}
	lines := m.visibleDiffLines()
	var hunks []int
	for i, line := range lines {
		if strings.HasPrefix(line, "@@") {
			hunks = append(hunks, i)
		}
	}
	if len(hunks) == 0 {
		return
	}
	if delta > 0 {
		for _, hunk := range hunks {
			if hunk > m.scroll {
				m.scroll = hunk
				return
			}
		}
		m.scroll = hunks[len(hunks)-1]
		return
	}
	for i := len(hunks) - 1; i >= 0; i-- {
		if hunks[i] < m.scroll {
			m.scroll = hunks[i]
			return
		}
	}
	m.scroll = hunks[0]
}

func (m *model) enter() {
	switch m.mode {
	case modeCommits:
		if len(m.commits) == 0 {
			return
		}
		m.loadCommitFiles()
		if m.err != nil {
			return
		}
		m.files = append([]string(nil), m.fileCache[m.commits[m.commitIdx].Hash]...)
		m.fileIdx = 0
		m.loadSelectedDiff()
		m.mode = modeFiles
	case modeFiles:
		if len(m.files) == 0 {
			return
		}
		m.loadSelectedDiff()
		if m.err != nil {
			return
		}
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

func (m model) commitFocusLine() int {
	line := 0
	for i, commit := range m.commits {
		if i == m.commitIdx {
			return line
		}
		line += 1 + len(m.links[commit.Hash])
	}
	return 0
}

func (m model) fileFocusLine() int {
	return 2 + m.fileIdx
}

func (m model) viewCommits() string {
	if m.width >= 100 {
		return m.viewCommitsWithPreview()
	}
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

func (m model) viewCommitsWithPreview() string {
	leftWidth := max(44, m.width/2)
	rightWidth := max(30, m.width-leftWidth-3)
	leftLines := strings.Split(strings.TrimRight(m.viewCommitsList(leftWidth), "\n"), "\n")
	rightLines := strings.Split(strings.TrimRight(m.viewCommitFilePreview(rightWidth), "\n"), "\n")
	return joinColumns(leftLines, rightLines, leftWidth)
}

func (m model) viewCommitsList(width int) string {
	var b strings.Builder
	for i, commit := range m.commits {
		line := fmt.Sprintf("%s %s  %s", hashStyle.Render(commit.ShortHash), commit.Date, truncateVisible(commit.Subject, max(0, width-19)))
		if i == m.commitIdx {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
		for _, req := range m.links[commit.Hash] {
			messageWidth := max(0, width-8-len(req.AgentName)-len(req.Model))
			b.WriteString(markerStyle.Render("└─ ●"))
			b.WriteByte(' ')
			b.WriteString(providerStyle.Render(fmt.Sprintf("[%s %s]", req.AgentName, req.Model)))
			b.WriteByte(' ')
			b.WriteString(requestStyle.Render(truncateVisible(req.Message, messageWidth)))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m model) viewCommitFilePreview(width int) string {
	if len(m.commits) == 0 {
		return ""
	}
	commit := m.commits[m.commitIdx]
	files := m.fileCache[commit.Hash]
	var b strings.Builder
	b.WriteString(titleStyle.Render("Changed files"))
	b.WriteByte('\n')
	b.WriteString(hashStyle.Render(commit.ShortHash))
	b.WriteByte(' ')
	b.WriteString(truncateVisible(commit.Subject, max(0, width-10)))
	b.WriteString("\n\n")
	if len(files) == 0 {
		b.WriteString(mutedStyle.Render("no changed files"))
		b.WriteByte('\n')
		return b.String()
	}
	for _, file := range files {
		b.WriteString(fileStyle.Render(truncateVisible(file, width)))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) viewFiles() string {
	if m.width >= 100 {
		return m.viewFilesWithPreview()
	}
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

func (m model) viewFilesWithPreview() string {
	leftWidth := max(32, m.width/3)
	rightWidth := max(40, m.width-leftWidth-3)
	leftLines := strings.Split(strings.TrimRight(m.viewFilesList(leftWidth), "\n"), "\n")
	rightLines := strings.Split(strings.TrimRight(m.viewSelectedDiffPreview(rightWidth), "\n"), "\n")
	return joinColumns(leftLines, rightLines, leftWidth)
}

func (m model) viewFilesList(width int) string {
	var b strings.Builder
	if len(m.commits) == 0 {
		return ""
	}
	commit := m.commits[m.commitIdx]
	b.WriteString(hashStyle.Render(commit.ShortHash))
	b.WriteByte(' ')
	b.WriteString(truncateVisible(commit.Subject, max(0, width-10)))
	b.WriteString("\n\n")
	for i, file := range m.files {
		line := fileStyle.Render(truncateVisible(file, width))
		if i == m.fileIdx {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) viewSelectedDiffPreview(width int) string {
	var b strings.Builder
	if len(m.commits) == 0 || len(m.files) == 0 {
		return ""
	}
	b.WriteString(titleStyle.Render("Diff preview"))
	b.WriteByte('\n')
	b.WriteString(fileStyle.Render(truncateVisible(m.files[m.fileIdx], width)))
	b.WriteString("\n\n")
	lines := m.diffLines
	if m.diffMode == diffSplit {
		lines = splitDiff(lines, width)
	}
	limit := previewLineLimit(m.height)
	for i, line := range lines {
		if i >= limit {
			break
		}
		b.WriteString(truncateStyledDiffLine(line, width))
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
	lines := m.visibleDiffLines()
	if m.scroll >= len(lines) {
		m.scroll = max(0, len(lines)-1)
	}
	for _, line := range lines[m.scroll:] {
		if m.width > 0 {
			line = truncateVisible(line, m.width)
		}
		b.WriteString(styleDiffLine(line))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *model) loadCommitFiles() {
	if len(m.commits) == 0 {
		return
	}
	if m.fileCache == nil {
		m.fileCache = map[string][]string{}
	}
	hash := m.commits[m.commitIdx].Hash
	if _, ok := m.fileCache[hash]; ok {
		return
	}
	files, err := git.ChangedFiles(m.root, hash)
	if err != nil {
		m.err = err
		return
	}
	m.fileCache[hash] = files
}

func (m *model) loadSelectedDiff() {
	if len(m.commits) == 0 || len(m.files) == 0 {
		m.diffLines = nil
		return
	}
	if m.diffCache == nil {
		m.diffCache = map[string][]string{}
	}
	hash := m.commits[m.commitIdx].Hash
	path := m.files[m.fileIdx]
	key := hash + "\x00" + path
	if lines, ok := m.diffCache[key]; ok {
		m.diffLines = lines
		return
	}
	lines, err := git.UnifiedDiff(m.root, hash, path)
	if err != nil {
		m.err = err
		return
	}
	m.diffCache[key] = lines
	m.diffLines = lines
}

func (m model) visibleDiffLines() []string {
	if m.diffMode == diffSplit {
		width := 120
		if m.width > 0 {
			width = m.width
		}
		return splitDiff(m.diffLines, width)
	}
	return m.diffLines
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

func truncateStyledDiffLine(line string, width int) string {
	return styleDiffLine(truncateVisible(line, width))
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

func joinColumns(leftLines, rightLines []string, leftWidth int) string {
	maxLines := max(len(leftLines), len(rightLines))
	var b strings.Builder
	for i := 0; i < maxLines; i++ {
		var left string
		if i < len(leftLines) {
			left = leftLines[i]
		}
		var right string
		if i < len(rightLines) {
			right = rightLines[i]
		}
		b.WriteString(padPlain(left, leftWidth))
		b.WriteString(" │ ")
		b.WriteString(right)
		b.WriteByte('\n')
	}
	return b.String()
}

func splitViewLines(content string) []string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}

func padPlain(s string, width int) string {
	visibleWidth := lipgloss.Width(s)
	if visibleWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visibleWidth)
}

func truncateVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 3 {
		return takeVisible(s, width)
	}
	return takeVisible(s, width-3) + "..."
}

func previewLineLimit(height int) int {
	if height <= 0 {
		return 40
	}
	return max(5, height-7)
}

func takeVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		next := b.String() + string(r)
		if lipgloss.Width(next) > width {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
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

func min(a, b int) int {
	if a < b {
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
