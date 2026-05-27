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

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

type mode int

const (
	modeCommits mode = iota
	modeFiles
	modeDiff
	modeFullFile
	modeRequest
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
	fullLines []string
	fileCache map[string][]string
	diffCache map[string][]string
	fullCache map[string][]string
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
	hashStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	providerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	requestStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	markerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	fileStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	addStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	delStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	hunkStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	cursorStyle    = lipgloss.NewStyle().Reverse(true)
	titleStyle     = lipgloss.NewStyle().Bold(true)
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8")).Padding(0, 1)
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8")).Padding(0, 1)
	statusStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14")).Padding(0, 1)
	statusAltStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("13")).Padding(0, 1)
	keyStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11")).Padding(0, 1)
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8"))
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
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
	m := model{
		root:      root,
		commits:   commits,
		links:     links,
		fileCache: map[string][]string{},
		diffCache: map[string][]string{},
		fullCache: map[string][]string{},
	}
	m.loadCommitFiles()
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func Highlight(filename, code string) []string {
	lexer := lexers.Get(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return strings.Split(code, "\n")
	}
	var buf strings.Builder
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return strings.Split(code, "\n")
	}
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
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
		case "pgup":
			m.page(-1)
		case "pgdown":
			m.page(1)
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
		case "f":
			m.toggleFullFile()
		case "r":
			m.toggleRequestFull()
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
		return titleStyle.Render(fmt.Sprintf("agentgit %s  diff:%s  f:full  n/p:hunk  q:quit", m.modeName(), m.diffModeName())) + "\n\n" + content
	}
	return m.viewFrame(content, focusLine)
}

func (m model) contentView() (string, int) {
	switch m.mode {
	case modeCommits:
		return m.viewCommitsList(m.width), m.commitFocusLine()
	case modeFiles:
		return m.viewFilesList(m.width), m.fileFocusLine()
	case modeDiff:
		return m.viewDiff(), 2
	case modeFullFile:
		return m.viewFullFile(), 2
	case modeRequest:
		return m.viewRequestFull(), 2
	default:
		return "", 0
	}
}

func (m model) viewFrame(content string, focusLine int) string {
	header := m.viewHeader()
	footer := m.viewFooter()

	var staticTop string
	if m.mode == modeCommits {
		staticTop = m.viewCommitDetailsPreview()
	} else if m.mode == modeFiles {
		staticTop = m.viewFileDetailsPreview()
	}

	topHeight := lipgloss.Height(staticTop)
	headerHeight := lipgloss.Height(header)
	footerHeight := lipgloss.Height(footer)

	bodyHeight := max(0, m.height-headerHeight-footerHeight-topHeight)
	body := m.viewBody(content, bodyHeight, focusLine)

	res := header + "\n"
	if staticTop != "" {
		res += staticTop + "\n"
	}
	res += body + "\n" + footer
	return res
}

func (m model) viewHeader() string {
	return headerStyle.Width(m.width).Render(fmt.Sprintf("agentgit  %s  diff:%s", m.modeName(), m.diffModeName()))
}

func (m model) viewCommitDetailsPreview() string {
	if len(m.commits) == 0 {
		return ""
	}
	commit := m.commits[m.commitIdx]
	var b strings.Builder

	// Commit Header
	b.WriteString(titleStyle.Render("Commit Details"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s %s  %s", hashStyle.Render(commit.Hash), mutedStyle.Render(commit.Date), commit.Subject))
	b.WriteString("\n\n")

	// Request Messages (Full)
	requests := m.links[commit.Hash]
	if len(requests) > 0 {
		b.WriteString(markerStyle.Render("Requests:"))
		b.WriteString("\n")
		for _, req := range requests {
			// Wrap message to width
			msg := requestStyle.Render(req.Message)
			b.WriteString(fmt.Sprintf("  ● [%s %s] %s\n", providerStyle.Render(req.AgentName), providerStyle.Render(req.Model), msg))
		}
		b.WriteString("\n")
	}

	// Changed Files Summary
	files := m.fileCache[commit.Hash]
	if len(files) > 0 {
		b.WriteString(markerStyle.Render(fmt.Sprintf("Files (%d):", len(files))))
		count := min(len(files), 3)
		for i := 0; i < count; i++ {
			b.WriteString(" " + fileStyle.Render(files[i]))
		}
		if len(files) > count {
			b.WriteString(mutedStyle.Render(fmt.Sprintf(" ...and %d more", len(files)-count)))
		}
		b.WriteString("\n")
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(m.width)

	return style.Render(m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), "  ... press r for full request"))
}

func (m model) viewFileDetailsPreview() string {
	if len(m.commits) == 0 || len(m.files) == 0 {
		return ""
	}
	file := m.files[m.fileIdx]
	var b strings.Builder
	b.WriteString(titleStyle.Render("File Preview: ") + fileStyle.Render(file))
	b.WriteString("\n\n")

	// Show a snippet of the diff
	lines := m.diffLines
	if m.diffMode == diffSplit {
		lines = splitDiff(lines, m.width)
	}

	previewLines := 5
	for i := 0; i < min(len(lines), previewLines); i++ {
		b.WriteString(truncateStyledDiffLine(lines[i], m.width-4))
		b.WriteByte('\n')
	}
	if len(lines) > previewLines {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  ... %d more lines (press Enter/l for full diff)", len(lines)-previewLines)))
		b.WriteByte('\n')
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(m.width)

	return style.Render(b.String())
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

func (m model) commitPreviewInnerHeight() int {
	if m.height <= 0 {
		return 8
	}
	return clamp(m.height/4, 5, 10)
}

func (m model) fitPreviewContent(content string, height int, overflow string) string {
	lines := splitViewLines(content)
	contentWidth := max(0, m.width-2)
	overflowLine := mutedStyle.Render(overflow)
	if len(lines) > height && height > 0 {
		lines = append([]string(nil), lines[:height]...)
		lines[height-1] = overflowLine
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = padPlain(truncateVisible(line, contentWidth), contentWidth)
	}
	return strings.Join(lines, "\n")
}

func (m model) helpText() string {
	switch m.mode {
	case modeCommits:
		return "j/k move  enter/l files  r request  q quit"
	case modeFiles:
		return "j/k move  enter/l diff  h back  q quit"
	case modeDiff:
		return "j/k scroll  pgup/pgdown page  n/p hunk  m split/unified  f full  h back  q quit"
	case modeFullFile:
		return "j/k scroll  pgup/pgdown page  f diff  h back  q quit"
	case modeRequest:
		return "j/k scroll  pgup/pgdown page  r back  h back  q quit"
	default:
		return "q quit"
	}
}

type shortcut struct {
	keys   string
	action string
}

func (m model) viewFooter() string {
	status := lipgloss.JoinHorizontal(
		lipgloss.Top,
		statusStyle.Render(strings.ToUpper(m.modeName())),
		statusAltStyle.Render("diff:"+m.diffModeName()),
	)
	line := status
	for _, item := range m.shortcuts() {
		line += "  " + keyStyle.Render(item.keys) + " " + helpStyle.Render(item.action)
	}
	return footerStyle.Width(m.width).Render(truncateVisible(line, max(0, m.width-2)))
}

func (m model) shortcuts() []shortcut {
	switch m.mode {
	case modeCommits:
		return []shortcut{
			{"j/k", "move"},
			{"enter/l", "files"},
			{"r", "request"},
			{"q", "quit"},
		}
	case modeFiles:
		return []shortcut{
			{"j/k", "move"},
			{"enter/l", "diff"},
			{"h", "back"},
			{"q", "quit"},
		}
	case modeDiff:
		return []shortcut{
			{"j/k", "scroll"},
			{"pgup/pgdn", "page"},
			{"n/p", "hunk"},
			{"m", "mode"},
			{"f", "full"},
			{"h", "back"},
			{"q", "quit"},
		}
	case modeFullFile:
		return []shortcut{
			{"j/k", "scroll"},
			{"pgup/pgdn", "page"},
			{"f", "diff"},
			{"h", "back"},
			{"q", "quit"},
		}
	case modeRequest:
		return []shortcut{
			{"j/k", "scroll"},
			{"pgup/pgdn", "page"},
			{"r", "back"},
			{"h", "back"},
			{"q", "quit"},
		}
	default:
		return []shortcut{{"q", "quit"}}
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
	case modeDiff, modeFullFile, modeRequest:
		m.scroll = max(0, m.scroll+delta)
	}
}

func (m *model) page(delta int) {
	switch m.mode {
	case modeDiff, modeFullFile, modeRequest:
		m.scroll = max(0, m.scroll+delta*m.pageSize())
	}
}

func (m model) pageSize() int {
	if m.height <= 0 {
		return 20
	}
	headerHeight := 1
	footerHeight := 1
	contentHeaderHeight := 2
	return max(1, m.height-headerHeight-footerHeight-contentHeaderHeight)
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
	case modeDiff, modeFullFile, modeRequest:
		m.mode = modeFiles
	case modeFiles:
		m.mode = modeCommits
	}
}

func (m *model) toggleRequestFull() {
	if m.mode == modeCommits {
		m.mode = modeRequest
		m.scroll = 0
	} else if m.mode == modeRequest {
		m.mode = modeCommits
		m.scroll = 0
	}
}

func (m model) viewRequestFull() string {
	if len(m.commits) == 0 {
		return ""
	}
	commit := m.commits[m.commitIdx]
	var b strings.Builder

	b.WriteString(titleStyle.Render("Full Request Details"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s %s  %s", hashStyle.Render(commit.Hash), mutedStyle.Render(commit.Date), commit.Subject))
	b.WriteString("\n\n")

	requests := m.links[commit.Hash]
	if len(requests) == 0 {
		b.WriteString(mutedStyle.Render("No requests found for this commit."))
	} else {
		for i, req := range requests {
			if i > 0 {
				b.WriteString("\n" + mutedStyle.Render(strings.Repeat("─", m.width-4)) + "\n\n")
			}
			b.WriteString(markerStyle.Render(fmt.Sprintf("Request %d:", i+1)))
			b.WriteString("\n")
			b.WriteString(fmt.Sprintf("  %s: %s\n", mutedStyle.Render("Agent"), providerStyle.Render(req.AgentName)))
			b.WriteString(fmt.Sprintf("  %s: %s\n", mutedStyle.Render("Model"), providerStyle.Render(req.Model)))
			b.WriteString("\n")
			b.WriteString(requestStyle.Render(req.Message))
			b.WriteString("\n")
		}
	}

	lines := splitViewLines(b.String())
	if m.scroll >= len(lines) {
		m.scroll = max(0, len(lines)-1)
	}
	return strings.Join(lines[m.scroll:], "\n")
}

func (m *model) toggleFullFile() {
	if m.mode == modeDiff {
		m.loadFullFile()
		if m.err == nil {
			m.mode = modeFullFile
			m.scroll = 0
		}
	} else if m.mode == modeFullFile {
		m.mode = modeDiff
		m.scroll = 0
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

func (m model) viewCommitsList(width int) string {
	var b strings.Builder
	for i, commit := range m.commits {
		line := fmt.Sprintf("%s %s  %s", hashStyle.Render(commit.ShortHash), commit.Date, commit.Subject)
		line = truncateVisible(line, width)
		if i == m.commitIdx {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
		// In list view, we still show a short marker if it has requests
		if len(m.links[commit.Hash]) > 0 {
			b.WriteString(markerStyle.Render("  ● "))
			b.WriteString(mutedStyle.Render(fmt.Sprintf("%d requests", len(m.links[commit.Hash]))))
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

func (m model) viewFilesList(width int) string {
	var b strings.Builder
	if len(m.commits) == 0 {
		return ""
	}
	for i, file := range m.files {
		line := fileStyle.Render(file)
		if i == m.fileIdx {
			line = cursorStyle.Render(line)
		}
		b.WriteString(truncateVisible(line, width))
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

func (m model) viewFullFile() string {
	var b strings.Builder
	if len(m.commits) == 0 || len(m.files) == 0 {
		return ""
	}
	b.WriteString(hashStyle.Render(m.commits[m.commitIdx].ShortHash))
	b.WriteByte(' ')
	b.WriteString(fileStyle.Render(m.files[m.fileIdx]))
	b.WriteString(" (Full File)\n\n")
	if m.scroll >= len(m.fullLines) {
		m.scroll = max(0, len(m.fullLines)-1)
	}
	for _, line := range m.fullLines[m.scroll:] {
		if m.width > 0 {
			line = truncateVisible(line, m.width)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *model) loadFullFile() {
	if len(m.commits) == 0 || len(m.files) == 0 {
		m.fullLines = nil
		return
	}
	hash := m.commits[m.commitIdx].Hash
	path := m.files[m.fileIdx]
	key := hash + "\x00" + path
	if lines, ok := m.fullCache[key]; ok {
		m.fullLines = lines
		return
	}
	content, err := git.CatFile(m.root, hash, path)
	if err != nil {
		m.err = err
		return
	}
	lines := Highlight(path, content)
	m.fullCache[key] = lines
	m.fullLines = lines
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

	// Apply syntax highlighting to diff lines
	highlighted := highlightDiff(path, lines)

	m.diffCache[key] = highlighted
	m.diffLines = highlighted
}

func highlightDiff(filename string, lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	// To highlight a diff properly, we should ideally highlight the whole file
	// or try to highlight blocks. For simplicity, we'll highlight line by line
	// if it's code. But chroma works better on blocks.
	// Let's try to identify code lines and highlight them.

	result := make([]string, len(lines))
	for i, line := range lines {
		if len(line) > 0 && (line[0] == '+' || line[0] == '-' || line[0] == ' ') {
			prefix := line[0]
			content := line[1:]
			hLines := Highlight(filename, content)
			if len(hLines) > 0 {
				result[i] = string(prefix) + hLines[0]
			} else {
				result[i] = line
			}
		} else {
			result[i] = line
		}
	}
	return result
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
		// Strictly enforce leftWidth for perfect vertical alignment
		left = truncateVisible(left, leftWidth)
		b.WriteString(padPlain(left, leftWidth))
		b.WriteString(mutedStyle.Render(" │ "))
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
	tail := "..."
	tailWidth := lipgloss.Width(tail)
	if width <= tailWidth {
		return takeVisible(s, width)
	}
	return takeVisible(s, width-tailWidth) + tail
}

func takeVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	// We need to handle ANSI codes. A simple way is to use lipgloss's internal
	// or similar logic, but we can iterate and check width at each step.
	// Note: This is a bit slow but correct for small strings.
	var b strings.Builder
	var visibleWidth int

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		// If we encounter an escape sequence, we should include it without counting its width.
		if runes[i] == '\x1b' {
			b.WriteRune(runes[i])
			for i+1 < len(runes) && runes[i] != 'm' {
				i++
				b.WriteRune(runes[i])
			}
			continue
		}

		charWidth := lipgloss.Width(string(runes[i]))
		if visibleWidth+charWidth > width {
			break
		}
		b.WriteRune(runes[i])
		visibleWidth += charWidth
	}
	return b.String()
}

func previewLineLimit(height int) int {
	if height <= 0 {
		return 40
	}
	return max(5, height-7)
}

func (m model) modeName() string {
	switch m.mode {
	case modeFiles:
		return "files"
	case modeDiff:
		return "diff"
	case modeFullFile:
		return "full"
	case modeRequest:
		return "request"
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
