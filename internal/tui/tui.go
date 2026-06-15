package tui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	modeDirectories
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
	root       string
	branch     string
	head       string
	limit      int
	commits    []git.Commit
	links      map[string][]store.LinkedRequest
	files      []string
	diffLines  []string
	fullLines  []string
	dirEntries []directoryEntry
	fileCache  map[string][]string
	diffCache  map[string][]string
	fullCache  map[string][]string
	mode       mode
	diffMode   diffMode
	commitIdx  int
	dirIdx     int
	fileIdx    int
	scroll     int
	width      int
	height     int
	err        error
}

type imageOpenMsg struct {
	err error
}

type directoryEntry struct {
	Path          string
	DisplayName   string
	Depth         int
	IsDir         bool
	CommitIndexes []int
	FileCount     int
}

type directoryStats struct {
	path          string
	isDir         bool
	commitIndexes map[int]bool
	files         map[string]bool
}

var (
	hashStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	providerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	requestStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	markerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	fileStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	addLineStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Background(lipgloss.Color("22"))
	delLineStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Background(lipgloss.Color("52"))
	hunkStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	cursorStyle    = lipgloss.NewStyle().Reverse(true)
	titleStyle     = lipgloss.NewStyle().Bold(true)
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8")).Padding(0, 1)
	contextStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("0")).Padding(0, 1)
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8")).Padding(0, 1)
	statusStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14")).Padding(0, 1)
	statusAltStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("13")).Padding(0, 1)
	keyStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11")).Padding(0, 1)
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8"))
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func Run(root string, limit int) error {
	commits, err := git.CommitsWithUncommitted(root, limit)
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
		branch:    git.Branch(root),
		head:      git.ShortHead(root),
		limit:     limit,
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
				requestStyle.Render(requestPreviewMessage(req.Message))
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
		case "up":
			m.move(-1)
		case "down":
			m.move(1)
		case "pgup":
			m.page(-1)
		case "pgdown":
			m.page(1)
		case "right":
			return m, m.enter(false)
		case "enter":
			return m, m.enter(true)
		case "tab":
			m.toggleTopLevelView()
		case "left", "backspace":
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
			m.refresh()
		case "v":
			m.toggleRequestFull()
		case "n":
			m.jumpHunk(1)
		case "p":
			m.jumpHunk(-1)
		}
	case imageOpenMsg:
		if msg.err != nil {
			m.err = msg.err
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
		return titleStyle.Render(m.headerTitleLine()) + "\n" + m.contextLine(120) + "\n\n" + content
	}
	return m.viewFrame(content, focusLine)
}

func (m model) contentView() (string, int) {
	switch m.mode {
	case modeCommits:
		return m.viewCommitsList(m.width), m.commitFocusLine()
	case modeDirectories:
		return m.viewDirectoryList(m.width), m.directoryFocusLine()
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
	} else if m.mode == modeDirectories {
		staticTop = m.viewDirectoryDetailsPreview()
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
	title := headerStyle.Width(m.width).Render(truncateVisible(m.headerTitleLine(), max(0, m.width-2)))
	context := contextStyle.Width(m.width).Render(truncateVisible(m.contextLine(max(0, m.width-2)), max(0, m.width-2)))
	return title + "\n" + context
}

func (m model) headerTitleLine() string {
	return fmt.Sprintf("agentgit  view:%s  diff:%s  %s", m.modeName(), m.diffModeName(), m.selectionTitle())
}

func (m model) contextLine(width int) string {
	base := compactPath(m.root, contextPathWidth(width))
	return fmt.Sprintf("base %s  branch %s  head %s  commits %d  dirty %d",
		base,
		emptyFallback(m.branch, "unknown"),
		emptyFallback(m.head, "unknown"),
		len(m.visibleCommits()),
		m.dirtyFileCount(),
	)
}

func (m model) selectionTitle() string {
	switch m.mode {
	case modeCommits, modeRequest:
		if len(m.commits) == 0 || m.commitIdx < 0 || m.commitIdx >= len(m.commits) {
			return "no commits"
		}
		commit := m.commits[m.commitIdx]
		return truncateVisible(commit.ShortHash+" "+commit.Subject, 80)
	case modeDirectories:
		if len(m.dirEntries) == 0 || m.dirIdx < 0 || m.dirIdx >= len(m.dirEntries) {
			return "no paths"
		}
		entry := m.dirEntries[m.dirIdx]
		path := entry.Path
		if entry.IsDir {
			path += "/"
		}
		return truncateVisible(path, 80)
	case modeFiles, modeDiff, modeFullFile:
		if len(m.files) == 0 || m.fileIdx < 0 || m.fileIdx >= len(m.files) {
			return "no files"
		}
		return truncateVisible(m.files[m.fileIdx], 80)
	default:
		return ""
	}
}

func (m model) visibleCommits() []git.Commit {
	commits := m.commits
	if len(commits) > 0 && commits[0].Hash == git.UncommittedHash {
		return commits[1:]
	}
	return commits
}

func (m model) dirtyFileCount() int {
	if len(m.commits) == 0 || m.commits[0].Hash != git.UncommittedHash {
		return 0
	}
	files := m.fileCache[git.UncommittedHash]
	if len(files) > 0 {
		return len(files)
	}
	return 1
}

func compactPath(path string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(path) <= width {
		return path
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if rel, relErr := filepath.Rel(home, path); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			path = "~/" + filepath.ToSlash(rel)
		}
	}
	if lipgloss.Width(path) <= width {
		return path
	}
	if width <= 3 {
		return takeVisible(path, width)
	}
	return "..." + takeVisibleFromEnd(path, width-3)
}

func contextPathWidth(width int) int {
	if width <= 0 {
		return 40
	}
	return clamp(width/2, 24, 80)
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
			msg := requestStyle.Render(requestPreviewMessage(req.Message))
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

	return style.Render(m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), "  ... press v for full request"))
}

func (m model) viewFileDetailsPreview() string {
	if len(m.commits) == 0 || len(m.files) == 0 {
		return ""
	}
	file := m.files[m.fileIdx]
	var b strings.Builder
	b.WriteString(titleStyle.Render("File Preview: ") + fileStyle.Render(file))
	b.WriteString("\n\n")
	if m.selectedFileIsImage() {
		b.WriteString(markerStyle.Render("Image file"))
		b.WriteByte('\n')
		b.WriteString(mutedStyle.Render("  press Enter to open image, Right for diff"))
		b.WriteString("\n\n")
	} else {
		b.WriteString(mutedStyle.Render("  press Enter/Right for diff"))
		b.WriteString("\n\n")
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

func (m model) selectedFileIsImage() bool {
	if len(m.files) == 0 || m.fileIdx < 0 || m.fileIdx >= len(m.files) {
		return false
	}
	return isImagePath(m.files[m.fileIdx])
}

func isImagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".avif", ".bmp", ".gif", ".heic", ".heif", ".ico", ".jpeg", ".jpg", ".png", ".tif", ".tiff", ".webp":
		return true
	default:
		return false
	}
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
		return "up/down move  enter/right files  tab directories  v request  r refresh  q quit"
	case modeDirectories:
		return "up/down move  enter/right latest files  tab commits  r refresh  q quit"
	case modeFiles:
		if m.selectedFileIsImage() {
			return "up/down move  enter image  right diff  left/backspace back  r refresh  q quit"
		}
		return "up/down move  enter/right diff  left/backspace back  r refresh  q quit"
	case modeDiff:
		return "up/down scroll  pgup/pgdown page  n/p hunk  m split/unified  f full  left/backspace back  r refresh  q quit"
	case modeFullFile:
		return "up/down scroll  pgup/pgdown page  f diff  left/backspace back  r refresh  q quit"
	case modeRequest:
		return "up/down scroll  pgup/pgdown page  v back  left/backspace back  r refresh  q quit"
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
			{"up/down", "move"},
			{"enter/right", "files"},
			{"tab", "dirs"},
			{"v", "request"},
			{"r", "refresh"},
			{"q", "quit"},
		}
	case modeDirectories:
		return []shortcut{
			{"up/down", "move"},
			{"enter/right", "files"},
			{"tab", "commits"},
			{"r", "refresh"},
			{"q", "quit"},
		}
	case modeFiles:
		items := []shortcut{{"up/down", "move"}}
		if m.selectedFileIsImage() {
			items = append(items, shortcut{"enter", "image"}, shortcut{"right", "diff"})
		} else {
			items = append(items, shortcut{"enter/right", "diff"})
		}
		return append(items, shortcut{"left", "back"}, shortcut{"r", "refresh"}, shortcut{"q", "quit"})
	case modeDiff:
		return []shortcut{
			{"up/down", "scroll"},
			{"pgup/pgdn", "page"},
			{"n/p", "hunk"},
			{"m", "mode"},
			{"f", "full"},
			{"left", "back"},
			{"r", "refresh"},
			{"q", "quit"},
		}
	case modeFullFile:
		return []shortcut{
			{"up/down", "scroll"},
			{"pgup/pgdn", "page"},
			{"f", "diff"},
			{"left", "back"},
			{"r", "refresh"},
			{"q", "quit"},
		}
	case modeRequest:
		return []shortcut{
			{"up/down", "scroll"},
			{"pgup/pgdn", "page"},
			{"v", "back"},
			{"left", "back"},
			{"r", "refresh"},
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
	case modeDirectories:
		m.dirIdx = clamp(m.dirIdx+delta, 0, len(m.dirEntries)-1)
	case modeFiles:
		m.fileIdx = clamp(m.fileIdx+delta, 0, len(m.files)-1)
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
	headerHeight := 2
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

func (m *model) enter(openImages bool) tea.Cmd {
	switch m.mode {
	case modeCommits:
		if len(m.commits) == 0 {
			return nil
		}
		m.loadCommitFiles()
		if m.err != nil {
			return nil
		}
		m.files = append([]string(nil), m.fileCache[m.commits[m.commitIdx].Hash]...)
		m.fileIdx = 0
		m.mode = modeFiles
	case modeDirectories:
		m.enterDirectoryEntry()
	case modeFiles:
		if len(m.files) == 0 {
			return nil
		}
		if openImages && m.selectedFileIsImage() {
			return m.openSelectedImage()
		}
		m.loadSelectedDiff()
		if m.err != nil {
			return nil
		}
		m.scroll = 0
		m.mode = modeDiff
	}
	return nil
}

func (m *model) back() {
	switch m.mode {
	case modeDiff, modeFullFile, modeRequest:
		m.mode = modeFiles
	case modeFiles:
		m.mode = modeCommits
	}
}

func (m *model) toggleTopLevelView() {
	if m.mode == modeDirectories {
		m.mode = modeCommits
		m.scroll = 0
		return
	}
	m.loadDirectoryEntries()
	if m.err != nil {
		return
	}
	m.mode = modeDirectories
	m.scroll = 0
}

func (m *model) refresh() {
	selectedCommit := ""
	if len(m.commits) > 0 && m.commitIdx >= 0 && m.commitIdx < len(m.commits) {
		selectedCommit = m.commits[m.commitIdx].Hash
	}
	selectedDirectory := ""
	if len(m.dirEntries) > 0 && m.dirIdx >= 0 && m.dirIdx < len(m.dirEntries) {
		selectedDirectory = m.dirEntries[m.dirIdx].Path
	}
	selectedFile := ""
	if len(m.files) > 0 && m.fileIdx >= 0 && m.fileIdx < len(m.files) {
		selectedFile = m.files[m.fileIdx]
	}

	commits, err := git.CommitsWithUncommitted(m.root, m.limit)
	if err != nil {
		m.err = err
		return
	}
	links, err := store.RequestsByCommit(m.root)
	if err != nil {
		m.err = err
		return
	}
	m.branch = git.Branch(m.root)
	m.head = git.ShortHead(m.root)
	m.commits = commits
	m.links = links
	m.fileCache = map[string][]string{}
	m.diffCache = map[string][]string{}
	m.fullCache = map[string][]string{}
	m.dirEntries = nil
	m.diffLines = nil
	m.fullLines = nil
	m.scroll = 0
	m.err = nil

	m.commitIdx = 0
	for i, commit := range m.commits {
		if commit.Hash == selectedCommit {
			m.commitIdx = i
			break
		}
	}
	m.loadCommitFiles()
	if m.mode == modeDirectories {
		m.loadDirectoryEntries()
		m.dirIdx = 0
		for i, entry := range m.dirEntries {
			if entry.Path == selectedDirectory {
				m.dirIdx = i
				break
			}
		}
	}
	if len(m.commits) == 0 {
		m.files = nil
		m.fileIdx = 0
		m.mode = modeCommits
		return
	}
	if m.mode != modeCommits && m.mode != modeDirectories && m.mode != modeRequest {
		m.files = append([]string(nil), m.fileCache[m.commits[m.commitIdx].Hash]...)
		m.fileIdx = 0
		for i, file := range m.files {
			if file == selectedFile {
				m.fileIdx = i
				break
			}
		}
		if len(m.files) == 0 {
			m.mode = modeCommits
			return
		}
		if m.mode == modeDiff {
			m.loadSelectedDiff()
		} else if m.mode == modeFullFile {
			m.loadFullFile()
		}
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

func (m model) directoryFocusLine() int {
	return m.dirIdx
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
		if summary := requestSummaryLine(m.links[commit.Hash]); summary != "" {
			b.WriteString(markerStyle.Render("  ● "))
			b.WriteString(truncateVisible(summary, max(0, width-4)))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m model) viewDirectoryList(width int) string {
	if len(m.dirEntries) == 0 {
		return mutedStyle.Render("no changed directories")
	}
	var b strings.Builder
	for i, entry := range m.dirEntries {
		kind := "[f]"
		name := entry.DisplayName
		if entry.IsDir {
			kind = "[d]"
			name += "/"
		}
		indent := strings.Repeat("  ", entry.Depth)
		detail := fmt.Sprintf("  %d commits", len(entry.CommitIndexes))
		if entry.IsDir {
			detail = fmt.Sprintf("  %d commits, %d files", len(entry.CommitIndexes), entry.FileCount)
		}
		line := indent + mutedStyle.Render(kind) + " " + fileStyle.Render(name) + mutedStyle.Render(detail)
		line = truncateVisible(line, width)
		if i == m.dirIdx {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) viewDirectoryDetailsPreview() string {
	if len(m.dirEntries) == 0 || m.dirIdx < 0 || m.dirIdx >= len(m.dirEntries) {
		return ""
	}
	entry := m.dirEntries[m.dirIdx]
	var b strings.Builder
	title := "File Details"
	path := entry.Path
	if entry.IsDir {
		title = "Directory Details"
		path += "/"
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")
	b.WriteString(fileStyle.Render(path))
	b.WriteString(mutedStyle.Render(fmt.Sprintf("  %d commits", len(entry.CommitIndexes))))
	if entry.IsDir {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  %d files", entry.FileCount)))
	}
	b.WriteString("\n\n")
	count := min(len(entry.CommitIndexes), 4)
	for i := 0; i < count; i++ {
		commit := m.commits[entry.CommitIndexes[i]]
		b.WriteString(fmt.Sprintf("  %s %s  %s\n", hashStyle.Render(commit.ShortHash), mutedStyle.Render(commit.Date), commit.Subject))
	}
	if len(entry.CommitIndexes) > count {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  ...and %d more commits\n", len(entry.CommitIndexes)-count)))
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(m.width)

	return style.Render(m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), "  ... press enter for latest files"))
}

func requestSummaryLine(requests []store.LinkedRequest) string {
	if len(requests) == 0 {
		return ""
	}
	req := requests[0]
	summary := providerStyle.Render(fmt.Sprintf("[%s %s]", req.AgentName, req.Model)) + " " + requestStyle.Render(requestPreviewMessage(req.Message))
	if len(requests) > 1 {
		summary += mutedStyle.Render(fmt.Sprintf(" (+%d)", len(requests)-1))
	}
	return summary
}

func requestPreviewMessage(message string) string {
	for _, line := range strings.Split(message, "\n") {
		if preview := strings.Join(strings.Fields(line), " "); preview != "" {
			return preview
		}
	}
	return ""
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
		b.WriteString(renderVisibleDiffLine(line, width, m.diffMode == diffSplit))
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
		b.WriteString(renderVisibleDiffLine(line, m.width, m.diffMode == diffSplit))
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

func (m *model) openSelectedImage() tea.Cmd {
	if len(m.commits) == 0 || len(m.files) == 0 {
		return nil
	}
	hash := m.commits[m.commitIdx].Hash
	path := m.files[m.fileIdx]
	data, err := git.CatFileBytes(m.root, hash, path)
	if err != nil {
		m.err = err
		return nil
	}
	temp, err := os.CreateTemp("", "agentgit-"+shortHash(hash)+"-*"+strings.ToLower(filepath.Ext(path)))
	if err != nil {
		m.err = err
		return nil
	}
	tempPath := temp.Name()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		m.err = err
		return nil
	}
	if err := temp.Close(); err != nil {
		m.err = err
		return nil
	}
	cmd, err := imageOpenCommand(tempPath)
	if err != nil {
		m.err = err
		return nil
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return imageOpenMsg{err: fmt.Errorf("open image: %w", err)}
		}
		return imageOpenMsg{}
	})
}

func imageOpenCommand(path string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path), nil
	case "linux":
		return exec.Command("xdg-open", path), nil
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path), nil
	default:
		return nil, fmt.Errorf("opening images is not supported on %s", runtime.GOOS)
	}
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func (m *model) loadCommitFiles() {
	if len(m.commits) == 0 {
		return
	}
	m.loadCommitFilesAt(m.commitIdx)
}

func (m *model) loadCommitFilesAt(index int) {
	if index < 0 || index >= len(m.commits) {
		return
	}
	if m.fileCache == nil {
		m.fileCache = map[string][]string{}
	}
	hash := m.commits[index].Hash
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

func (m *model) loadDirectoryEntries() {
	if len(m.commits) == 0 {
		m.dirEntries = nil
		return
	}
	if m.fileCache == nil {
		m.fileCache = map[string][]string{}
	}
	stats := map[string]*directoryStats{}
	for i, commit := range m.commits {
		m.loadCommitFilesAt(i)
		if m.err != nil {
			return
		}
		for _, file := range m.fileCache[commit.Hash] {
			addDirectoryStats(stats, file, i)
		}
	}
	entries := make([]directoryEntry, 0, len(stats))
	for _, stat := range stats {
		indexes := make([]int, 0, len(stat.commitIndexes))
		for index := range stat.commitIndexes {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		entry := directoryEntry{
			Path:          stat.path,
			DisplayName:   filepath.Base(filepath.FromSlash(stat.path)),
			Depth:         strings.Count(stat.path, "/"),
			IsDir:         stat.isDir,
			CommitIndexes: indexes,
			FileCount:     len(stat.files),
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return directoryEntrySortKey(entries[i]) < directoryEntrySortKey(entries[j])
	})
	m.dirEntries = entries
	m.dirIdx = clamp(m.dirIdx, 0, len(m.dirEntries)-1)
}

func addDirectoryStats(stats map[string]*directoryStats, file string, commitIndex int) {
	file = strings.Trim(file, "/")
	if file == "" {
		return
	}
	ensureDirectoryStats(stats, file, false).commitIndexes[commitIndex] = true
	for dir := pathDirectory(file); dir != ""; dir = pathDirectory(dir) {
		stat := ensureDirectoryStats(stats, dir, true)
		stat.commitIndexes[commitIndex] = true
		stat.files[file] = true
	}
}

func ensureDirectoryStats(stats map[string]*directoryStats, path string, isDir bool) *directoryStats {
	if stat, ok := stats[path]; ok {
		if isDir {
			stat.isDir = true
		}
		return stat
	}
	stat := &directoryStats{
		path:          path,
		isDir:         isDir,
		commitIndexes: map[int]bool{},
		files:         map[string]bool{},
	}
	if !isDir {
		stat.files[path] = true
	}
	stats[path] = stat
	return stat
}

func pathDirectory(path string) string {
	dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(path)))
	if dir == "." {
		return ""
	}
	return dir
}

func directoryEntrySortKey(entry directoryEntry) string {
	if entry.IsDir {
		return entry.Path + "/"
	}
	return entry.Path
}

func (m *model) enterDirectoryEntry() {
	if len(m.dirEntries) == 0 || m.dirIdx < 0 || m.dirIdx >= len(m.dirEntries) {
		return
	}
	entry := m.dirEntries[m.dirIdx]
	if len(entry.CommitIndexes) == 0 {
		return
	}
	m.commitIdx = entry.CommitIndexes[0]
	m.loadCommitFiles()
	if m.err != nil {
		return
	}
	hash := m.commits[m.commitIdx].Hash
	files := m.filesForDirectoryEntry(entry, m.fileCache[hash])
	if len(files) == 0 {
		return
	}
	m.files = files
	m.fileIdx = 0
	m.scroll = 0
	m.mode = modeFiles
}

func (m model) filesForDirectoryEntry(entry directoryEntry, files []string) []string {
	if !entry.IsDir {
		for _, file := range files {
			if file == entry.Path {
				return []string{file}
			}
		}
		return nil
	}
	prefix := entry.Path + "/"
	var filtered []string
	for _, file := range files {
		if strings.HasPrefix(file, prefix) {
			filtered = append(filtered, file)
		}
	}
	return filtered
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

func styleDiffLine(line string, width int) string {
	switch {
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return renderDiffBackground(addLineStyle, line, width)
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return renderDiffBackground(delLineStyle, line, width)
	case strings.HasPrefix(line, "@@"):
		return hunkStyle.Render(line)
	default:
		return line
	}
}

func truncateStyledDiffLine(line string, width int) string {
	return styleDiffLine(truncateVisible(line, width), width)
}

func renderVisibleDiffLine(line string, width int, split bool) string {
	if !split {
		return truncateStyledDiffLine(line, width)
	}
	switch {
	case strings.HasPrefix(line, "@@"):
		return hunkStyle.Render(truncateVisible(line, width))
	case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
		return truncateVisible(line, width)
	default:
		return line
	}
}

func renderDiffBackground(style lipgloss.Style, line string, width int) string {
	line = ansi.Strip(line)
	if width <= 0 {
		return style.Render(line)
	}
	return style.Render(padPlain(line, width))
}

func splitDiff(lines []string, width int) []string {
	leftWidth, rightWidth := splitColumnWidths(width)
	var out []string
	var pending *string
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "@@"):
			if pending != nil {
				out = append(out, formatSplit(*pending, "", leftWidth, rightWidth, true, false))
				pending = nil
			}
			out = append(out, line)
		case strings.HasPrefix(line, "-"):
			if pending != nil {
				out = append(out, formatSplit(*pending, "", leftWidth, rightWidth, true, false))
			}
			text := strings.TrimPrefix(line, "-")
			pending = &text
		case strings.HasPrefix(line, "+"):
			text := strings.TrimPrefix(line, "+")
			if pending != nil {
				out = append(out, formatSplit(*pending, text, leftWidth, rightWidth, true, true))
				pending = nil
			} else {
				out = append(out, formatSplit("", text, leftWidth, rightWidth, false, true))
			}
		default:
			if pending != nil {
				out = append(out, formatSplit(*pending, "", leftWidth, rightWidth, true, false))
				pending = nil
			}
			text := strings.TrimPrefix(line, " ")
			out = append(out, formatSplit(text, text, leftWidth, rightWidth, false, false))
		}
	}
	if pending != nil {
		out = append(out, formatSplit(*pending, "", leftWidth, rightWidth, true, false))
	}
	return out
}

func splitColumnWidths(width int) (int, int) {
	if width <= 0 {
		width = 120
	}
	if width <= 4 {
		return max(1, width), 0
	}
	usable := width - 3
	leftWidth := max(1, usable/2)
	rightWidth := max(1, usable-leftWidth)
	return leftWidth, rightWidth
}

func formatSplit(left, right string, leftWidth, rightWidth int, leftChanged, rightChanged bool) string {
	if leftChanged {
		left = ansi.Strip(left)
	}
	if rightChanged {
		right = ansi.Strip(right)
	}
	leftCell := padPlain(truncateVisible(left, leftWidth), leftWidth)
	rightCell := padPlain(truncateVisible(right, rightWidth), rightWidth)
	if leftChanged {
		leftCell = delLineStyle.Render(leftCell)
	}
	if rightChanged {
		rightCell = addLineStyle.Render(rightCell)
	}
	if rightWidth <= 0 {
		return leftCell
	}
	return leftCell + " │ " + rightCell
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

func takeVisibleFromEnd(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	var out []rune
	var visibleWidth int
	for i := len(runes) - 1; i >= 0; i-- {
		charWidth := lipgloss.Width(string(runes[i]))
		if visibleWidth+charWidth > width {
			break
		}
		out = append(out, runes[i])
		visibleWidth += charWidth
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func previewLineLimit(height int) int {
	if height <= 0 {
		return 40
	}
	return max(5, height-7)
}

func (m model) modeName() string {
	switch m.mode {
	case modeDirectories:
		return "directories"
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
