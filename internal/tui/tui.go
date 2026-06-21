package tui

import (
	"errors"
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
	modeSelect
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
	expanded   map[string]bool
	fileCache  map[string][]string
	diffCache  map[string][]string
	fullCache  map[string][]string
	selected   map[string]bool
	mode       mode
	fileReturn mode
	diffMode   diffMode
	pending    selectAction
	commitIdx  int
	dirIdx     int
	fileIdx    int
	scroll     int
	width      int
	height     int
	err        error
	notice     string
	helpOpen   bool
	lineNums   bool
}

type selectAction int

const (
	selectActionNone selectAction = iota
	selectActionRemove
	selectActionSquash
)

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
	contextStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("0"))
	viewStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8"))
	commandStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("7"))
	contextLabel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("14")).Padding(0, 1)
	viewLabel      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("13")).Padding(0, 1)
	commandLabel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11")).Padding(0, 1)
	statusAltStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("13")).Padding(0, 1)
	keyStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11")).Padding(0, 1)
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
		selected:  map[string]bool{},
		expanded:  map[string]bool{},
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
		if m.helpOpen {
			switch msg.String() {
			case "?", "esc", "enter":
				m.helpOpen = false
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			default:
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.mode == modeSelect && m.pending != selectActionNone {
				m.cancelPendingSelectAction()
				return m, nil
			}
		case "up":
			m.clearNotice()
			m.move(-1)
		case "down":
			m.clearNotice()
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
			m.clearNotice()
			m.toggleTopLevelView()
		case "left":
			m.clearNotice()
			if m.mode == modeDirectories {
				m.collapseDirectoryEntry()
			} else {
				m.back()
			}
		case "backspace":
			m.clearNotice()
			m.back()
		case "m":
			if m.mode == modeSelect {
				m.requestSelectAction(selectActionSquash)
			} else if m.diffMode == diffUnified {
				m.diffMode = diffSplit
			} else {
				m.diffMode = diffUnified
			}
		case "f":
			m.toggleFullFile()
		case "l":
			if m.mode == modeDiff || m.mode == modeFullFile {
				m.lineNums = !m.lineNums
			}
		case "r":
			m.clearNotice()
			m.refresh()
		case "v":
			m.toggleRequestFull()
		case "n":
			if m.mode == modeSelect && m.pending != selectActionNone {
				m.cancelPendingSelectAction()
			} else {
				m.jumpHunk(1)
			}
		case "p":
			m.jumpHunk(-1)
		case "s":
			m.clearNotice()
			m.toggleSelectMode()
		case " ":
			if m.mode == modeSelect {
				m.toggleSelectedCommit()
			}
		case "x":
			if m.mode == modeSelect {
				m.requestSelectAction(selectActionRemove)
			}
		case "y":
			if m.mode == modeSelect {
				m.confirmSelectAction()
			}
		case "?":
			m.helpOpen = true
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
		base := m.viewHeaderAtWidth(120) + "\n\n" + content
		if m.helpOpen {
			return base + "\n\n" + m.viewHelpDialog(120, 0)
		}
		return base
	}
	if m.helpOpen {
		return m.viewHelpFrame()
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
		return "", m.fileFocusLine()
	case modeDiff:
		return m.viewDiff(), 2
	case modeFullFile:
		return m.viewFullFile(), 2
	case modeRequest:
		return m.viewRequestFull(), 2
	case modeSelect:
		return m.viewSelectList(m.width), m.commitFocusLine()
	default:
		return "", 0
	}
}

func (m model) viewFrame(content string, focusLine int) string {
	header := m.viewHeader()

	var staticTop string
	if m.mode == modeCommits {
		staticTop = m.viewCommitDetailsPreview()
	} else if m.mode == modeSelect {
		staticTop = m.viewSelectDetailsPreview()
	} else if m.mode == modeDirectories {
		staticTop = m.viewDirectoryDetailsPreview()
	} else if m.mode == modeFiles {
		staticTop = m.viewFileDetailsPreview()
	}

	topHeight := lipgloss.Height(staticTop)
	headerHeight := lipgloss.Height(header)

	bodyHeight := max(0, m.height-headerHeight-topHeight)
	var body string
	if m.mode == modeFiles {
		body = m.viewFilesBody(bodyHeight)
	} else {
		body = m.viewBody(content, bodyHeight, focusLine)
	}

	res := header + "\n"
	if staticTop != "" {
		res += staticTop + "\n"
	}
	res += body + "\n"
	return res
}

func (m model) viewHeader() string {
	return m.viewHeaderAtWidth(m.width)
}

func (m model) viewHeaderAtWidth(width int) string {
	return strings.Join([]string{
		renderHeaderRow(width, "CONTEXT", m.contextLine(width), contextStyle, contextLabel),
		renderHeaderRow(width, "VIEW", m.viewContextLine(), viewStyle, viewLabel),
		renderHeaderRow(width, "COMMANDS", "[?] Help", commandStyle, commandLabel),
	}, "\n")
}

func renderHeaderRow(width int, label, content string, rowStyle, labelStyle lipgloss.Style) string {
	if width <= 0 {
		return label + "  " + content
	}
	labelCell := labelStyle.Width(10).Render(label)
	gap := 1
	contentWidth := max(0, width-lipgloss.Width(labelCell)-gap)
	line := labelCell
	if contentWidth > 0 {
		line += " " + truncateVisible(content, contentWidth)
	}
	return rowStyle.Width(width).Render(line)
}

func (m model) contextLine(width int) string {
	base := compactPath(m.root, contextPathWidth(width))
	return fmt.Sprintf("repo %s  branch %s  head %s  commits %d  dirty %d",
		base,
		emptyFallback(m.branch, "unknown"),
		emptyFallback(m.head, "unknown"),
		len(m.visibleCommits()),
		m.dirtyFileCount(),
	)
}

func (m model) viewContextLine() string {
	return fmt.Sprintf("view: %s  diff: %s  target: %s", m.modeName(), m.diffModeName(), m.selectionTitle())
}

func (m model) selectionTitle() string {
	switch m.mode {
	case modeCommits, modeRequest, modeSelect:
		if len(m.commits) == 0 || m.commitIdx < 0 || m.commitIdx >= len(m.commits) {
			return "no commits"
		}
		commit := m.commits[m.commitIdx]
		return truncateVisible(commit.ShortHash+" "+commit.Subject, 80)
	case modeDirectories:
		entries := m.visibleDirectoryEntries()
		if len(entries) == 0 || m.dirIdx < 0 || m.dirIdx >= len(entries) {
			return "no paths"
		}
		entry := entries[m.dirIdx]
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
		visible[i] = frameLine(line, m.width)
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

type helpEntry struct {
	keys        string
	action      string
	description string
}

func (m model) viewHelpFrame() string {
	header := m.viewHeader()
	headerHeight := lipgloss.Height(header)
	bodyHeight := max(0, m.height-headerHeight)
	dialog := m.viewHelpDialog(m.width, bodyHeight)
	body := lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, dialog)
	return header + "\n" + body + "\n"
}

func (m model) viewHelpDialog(width int, height int) string {
	dialogWidth := clamp(width-8, 36, 76)
	if width > 0 && dialogWidth > width {
		dialogWidth = width
	}
	contentWidth := max(0, dialogWidth-4)
	entries := m.helpEntries()
	keyWidth := 0
	for _, entry := range entries {
		keyWidth = max(keyWidth, lipgloss.Width(entry.keys))
	}
	keyWidth = clamp(keyWidth, 6, 16)

	var lines []string
	lines = append(lines, titleStyle.Render("Help"))
	lines = append(lines, mutedStyle.Render("view: "+m.modeName()+"  close: ?, esc, enter"))
	lines = append(lines, strings.Repeat("─", contentWidth))
	for _, entry := range entries {
		keyCell := keyStyle.Render(padPlain(entry.keys, keyWidth))
		textWidth := max(0, contentWidth-keyWidth-3)
		text := entry.action
		if entry.description != "" {
			text += "  " + mutedStyle.Render(entry.description)
		}
		lines = append(lines, keyCell+"  "+truncateVisible(text, textWidth))
	}
	lines = append(lines, strings.Repeat("─", contentWidth))
	lines = append(lines, mutedStyle.Render("Press ? to return."))

	if height > 0 {
		maxContentLines := max(3, height-4)
		if len(lines) > maxContentLines {
			lines = append([]string(nil), lines[:maxContentLines]...)
			lines[len(lines)-1] = mutedStyle.Render("... resize the terminal to see more")
		}
	}
	for i, line := range lines {
		lines[i] = truncateVisible(line, contentWidth)
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("14")).
		Padding(0, 1).
		Width(dialogWidth)
	return style.Render(strings.Join(lines, "\n"))
}

func (m model) helpEntries() []helpEntry {
	entries := []helpEntry{
		{"?", "Close help", "return to the current screen"},
		{"ctrl+c", "Quit", "exit agentgit immediately"},
	}
	switch m.mode {
	case modeCommits:
		return append([]helpEntry{
			{"up/down", "Move cursor", "select a commit"},
			{"enter/right", "Open files", "show files changed by the selected commit"},
			{"tab", "Directories", "switch to directory summary"},
			{"s", "Select mode", "choose latest commits for remove or merge"},
			{"v", "Request details", "show full linked request text"},
			{"r", "Refresh", "reload commits and request links"},
			{"ctrl+c", "Quit", "exit agentgit"},
		}, entries...)
	case modeSelect:
		if m.pending != selectActionNone {
			return append([]helpEntry{
				{"y", "Confirm", "rewrite the selected latest commits"},
				{"n/esc", "Cancel", "return to select mode without rewriting"},
				{"ctrl+c", "Quit", "exit agentgit"},
			}, entries...)
		}
		return append([]helpEntry{
			{"up/down", "Move cursor", "select a commit row"},
			{"space/enter", "Toggle selection", "only latest contiguous ranges can be applied"},
			{"x", "Remove", "reset selected latest commits and delete their request links"},
			{"m", "Merge", "squash selected latest commits and move request links"},
			{"s/left", "Back", "return to commit list"},
			{"r", "Refresh", "reload commits and keep valid selections"},
		}, entries...)
	case modeDirectories:
		return append([]helpEntry{
			{"up/down", "Move cursor", "select a directory or file path"},
			{"enter/right", "Toggle/open", "toggle folders or open the selected file path"},
			{"left", "Collapse", "collapse the selected depth to its parent folder"},
			{"tab", "Commits", "switch back to commit list"},
			{"r", "Refresh", "reload commits and request links"},
			{"ctrl+c", "Quit", "exit agentgit"},
		}, entries...)
	case modeFiles:
		if m.selectedFileIsImage() {
			return append([]helpEntry{
				{"up/down", "Move cursor", "select a changed file"},
				{"enter", "Open image", "open the image with the system viewer"},
				{"right", "Open diff", "show the file diff"},
				{"left/backspace", "Back", "return to commits"},
				{"r", "Refresh", "reload commits and files"},
			}, entries...)
		}
		return append([]helpEntry{
			{"up/down", "Move cursor", "select a changed file"},
			{"enter/right", "Open diff", "show the file diff"},
			{"left/backspace", "Back", "return to commits"},
			{"r", "Refresh", "reload commits and files"},
		}, entries...)
	case modeDiff:
		return append([]helpEntry{
			{"up/down", "Scroll", "move through diff lines"},
			{"pgup/pgdn", "Page", "scroll by one page"},
			{"n/p", "Next/previous hunk", "jump between diff hunks"},
			{"m", "Diff layout", "toggle unified and split diff"},
			{"l", "Line numbers", "toggle old and new file line numbers"},
			{"f", "Full file", "show the full file at this revision"},
			{"left/backspace", "Back", "return to file list"},
			{"r", "Refresh", "reload current commit data"},
		}, entries...)
	case modeFullFile:
		return append([]helpEntry{
			{"up/down", "Scroll", "move through file lines"},
			{"pgup/pgdn", "Page", "scroll by one page"},
			{"l", "Line numbers", "toggle file line numbers"},
			{"f", "Diff", "return to diff view"},
			{"left/backspace", "Back", "return to file list"},
			{"r", "Refresh", "reload current commit data"},
		}, entries...)
	case modeRequest:
		return append([]helpEntry{
			{"up/down", "Scroll", "move through request text"},
			{"pgup/pgdn", "Page", "scroll by one page"},
			{"v", "Back", "return to commit list"},
			{"left/backspace", "Back", "return to file list"},
			{"r", "Refresh", "reload request links"},
		}, entries...)
	default:
		return entries
	}
}

func (m *model) move(delta int) {
	switch m.mode {
	case modeCommits:
		m.commitIdx = clamp(m.commitIdx+delta, 0, len(m.commits)-1)
		m.loadCommitFiles()
	case modeSelect:
		m.commitIdx = clamp(m.commitIdx+delta, 0, len(m.commits)-1)
		m.pending = selectActionNone
	case modeDirectories:
		m.dirIdx = clamp(m.dirIdx+delta, 0, len(m.visibleDirectoryEntries())-1)
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
	headerHeight := 3
	contentHeaderHeight := 2
	return max(1, m.height-headerHeight-contentHeaderHeight)
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
		m.fileReturn = modeCommits
		m.mode = modeFiles
	case modeSelect:
		m.toggleSelectedCommit()
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
		if m.fileReturn == modeDirectories {
			m.mode = modeDirectories
		} else {
			m.mode = modeCommits
		}
	case modeSelect:
		m.mode = modeCommits
		m.pending = selectActionNone
	}
}

func (m *model) toggleTopLevelView() {
	if m.mode == modeSelect {
		return
	}
	if m.mode == modeDirectories {
		m.mode = modeCommits
		m.scroll = 0
		return
	}
	m.loadDirectoryEntries()
	if m.err != nil {
		return
	}
	if m.mode != modeDirectories && !(m.mode == modeFiles && m.fileReturn == modeDirectories) {
		m.expanded = map[string]bool{}
	}
	m.expandCurrentDirectoryPath()
	m.mode = modeDirectories
	m.scroll = 0
}

func (m *model) refresh() {
	selectedCommit := ""
	if len(m.commits) > 0 && m.commitIdx >= 0 && m.commitIdx < len(m.commits) {
		selectedCommit = m.commits[m.commitIdx].Hash
	}
	selectedDirectory := ""
	visibleDirectories := m.visibleDirectoryEntries()
	if len(visibleDirectories) > 0 && m.dirIdx >= 0 && m.dirIdx < len(visibleDirectories) {
		selectedDirectory = visibleDirectories[m.dirIdx].Path
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
	m.selected = keepExistingSelections(m.selected, commits)
	m.pending = selectActionNone
	if m.mode != modeDirectories && !(m.mode == modeFiles && m.fileReturn == modeDirectories) {
		m.expanded = map[string]bool{}
	}
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
	if m.mode == modeDirectories || (m.mode == modeFiles && m.fileReturn == modeDirectories) {
		m.loadDirectoryEntries()
		m.dirIdx = 0
		for i, entry := range m.visibleDirectoryEntries() {
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
	if m.mode != modeCommits && m.mode != modeDirectories && m.mode != modeRequest && m.mode != modeSelect {
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

func (m *model) toggleSelectMode() {
	if m.mode == modeCommits {
		if m.selected == nil {
			m.selected = map[string]bool{}
		}
		m.mode = modeSelect
		m.pending = selectActionNone
		m.scroll = 0
		return
	}
	if m.mode == modeSelect {
		m.mode = modeCommits
		m.pending = selectActionNone
		m.scroll = 0
	}
}

func (m *model) toggleSelectedCommit() {
	if m.mode != modeSelect || len(m.commits) == 0 || m.commitIdx < 0 || m.commitIdx >= len(m.commits) {
		return
	}
	m.pending = selectActionNone
	commit := m.commits[m.commitIdx]
	if m.selected == nil {
		m.selected = map[string]bool{}
	}
	if m.selected[commit.Hash] {
		delete(m.selected, commit.Hash)
	} else {
		m.selected[commit.Hash] = true
	}
	m.clearNotice()
}

func (m *model) requestSelectAction(action selectAction) {
	if m.mode != modeSelect {
		return
	}
	if _, err := m.validateSelectAction(action); err != nil {
		m.notice = err.Error()
		m.pending = selectActionNone
		return
	}
	m.pending = action
	if m.selected[git.UncommittedHash] {
		m.notice = "this will discard uncommitted changes"
	} else {
		m.notice = "this will rewrite the latest commits"
	}
}

func (m *model) confirmSelectAction() {
	if m.mode != modeSelect || m.pending == selectActionNone {
		return
	}
	action := m.pending
	selected, err := m.validateSelectAction(action)
	if err != nil {
		m.notice = err.Error()
		m.pending = selectActionNone
		return
	}
	hashes := commitHashes(selected)
	switch action {
	case selectActionRemove:
		removeUncommitted := m.selected[git.UncommittedHash]
		if m.selected[git.UncommittedHash] {
			if err := git.DiscardUncommitted(m.root); err != nil {
				m.notice = "remove failed: " + err.Error()
				m.pending = selectActionNone
				return
			}
		}
		if len(selected) > 0 {
			base, err := git.Parent(m.root, selected[len(selected)-1].Hash)
			if err != nil {
				m.notice = err.Error()
				m.pending = selectActionNone
				return
			}
			if err := git.ResetHard(m.root, base); err != nil {
				m.notice = "remove failed: " + err.Error()
				m.pending = selectActionNone
				return
			}
		}
		if err := store.DeleteCommitLinks(m.root, hashes); err != nil {
			m.notice = "removed commits, but db cleanup failed: " + err.Error()
			m.pending = selectActionNone
			return
		}
		m.selected = map[string]bool{}
		m.pending = selectActionNone
		m.refreshWithNotice(removeNotice(len(selected), removeUncommitted))
	case selectActionSquash:
		base, err := git.Parent(m.root, selected[len(selected)-1].Hash)
		if err != nil {
			m.notice = err.Error()
			m.pending = selectActionNone
			return
		}
		newHash, err := git.SquashSince(m.root, base, squashCommitMessage(selected))
		if err != nil {
			m.notice = "merge failed: " + err.Error()
			m.pending = selectActionNone
			return
		}
		if err := store.MoveCommitLinks(m.root, hashes, newHash); err != nil {
			m.notice = "merged commits, but db link update failed: " + err.Error()
			m.pending = selectActionNone
			return
		}
		m.selected = map[string]bool{}
		m.pending = selectActionNone
		m.refreshWithNotice(fmt.Sprintf("merged %d commits into %s", len(selected), shortHash(newHash)))
	}
}

func (m *model) validateSelectAction(action selectAction) ([]git.Commit, error) {
	switch action {
	case selectActionRemove:
		selected, err := m.selectedLatestRange(true)
		if err != nil {
			return nil, err
		}
		if !m.selected[git.UncommittedHash] {
			clean, err := git.IsWorkingTreeClean(m.root)
			if err != nil {
				return nil, err
			}
			if !clean {
				return nil, errors.New("select uncommitted changes or clean the working tree")
			}
		}
		if len(selected) > 0 {
			if _, err := git.Parent(m.root, selected[len(selected)-1].Hash); err != nil {
				return nil, err
			}
		}
		return selected, nil
	case selectActionSquash:
		if m.selected[git.UncommittedHash] {
			return nil, errors.New("uncommitted changes cannot be merged")
		}
		selected, err := m.selectedLatestRange(false)
		if err != nil {
			return nil, err
		}
		if len(selected) < 2 {
			return nil, errors.New("select at least 2 latest commits to merge")
		}
		clean, err := git.IsWorkingTreeClean(m.root)
		if err != nil {
			return nil, err
		}
		if !clean {
			return nil, errors.New("working tree must be clean")
		}
		if _, err := git.Parent(m.root, selected[len(selected)-1].Hash); err != nil {
			return nil, err
		}
		return selected, nil
	default:
		return nil, errors.New("unknown select action")
	}
}

func (m model) selectedLatestRange(allowUncommitted bool) ([]git.Commit, error) {
	if m.selectedCount() == 0 {
		return nil, errors.New("select one or more commits")
	}
	firstReal := -1
	for i, commit := range m.commits {
		if commit.Hash != git.UncommittedHash {
			firstReal = i
			break
		}
	}
	if firstReal < 0 {
		if allowUncommitted && m.selected[git.UncommittedHash] {
			return nil, nil
		}
		return nil, errors.New("no committed commits to select")
	}
	maxSelected := -1
	for i, commit := range m.commits {
		if !m.selected[commit.Hash] {
			continue
		}
		if commit.Hash == git.UncommittedHash {
			if allowUncommitted {
				continue
			}
			return nil, errors.New("uncommitted changes cannot be selected")
		}
		if i < firstReal {
			return nil, errors.New("selection must start at HEAD")
		}
		if i > maxSelected {
			maxSelected = i
		}
	}
	if maxSelected < firstReal {
		if allowUncommitted && m.selected[git.UncommittedHash] {
			return nil, nil
		}
		return nil, errors.New("selection must start at HEAD")
	}
	for i := firstReal; i <= maxSelected; i++ {
		if !m.selected[m.commits[i].Hash] {
			return nil, errors.New("selection must include every latest commit up to the oldest selected commit")
		}
	}
	return append([]git.Commit(nil), m.commits[firstReal:maxSelected+1]...), nil
}

func (m model) selectedCount() int {
	count := 0
	for _, selected := range m.selected {
		if selected {
			count++
		}
	}
	return count
}

func (m *model) cancelPendingSelectAction() {
	m.pending = selectActionNone
	m.notice = "cancelled"
}

func (m *model) clearNotice() {
	m.notice = ""
}

func (m *model) refreshWithNotice(notice string) {
	m.refresh()
	if m.err == nil {
		m.notice = notice
	}
}

func (m model) pendingActionName() string {
	switch m.pending {
	case selectActionRemove:
		return "remove"
	case selectActionSquash:
		return "merge"
	default:
		return ""
	}
}

func commitHashes(commits []git.Commit) []string {
	hashes := make([]string, 0, len(commits))
	for _, commit := range commits {
		hashes = append(hashes, commit.Hash)
	}
	return hashes
}

func squashCommitMessage(commits []git.Commit) []string {
	if len(commits) == 0 {
		return []string{"Squash selected commits"}
	}
	oldest := commits[len(commits)-1]
	subject := strings.TrimSpace(oldest.Subject)
	if subject == "" {
		subject = "Squash selected commits"
	}
	var body strings.Builder
	body.WriteString(fmt.Sprintf("Squashed %d commits:", len(commits)))
	for i := len(commits) - 1; i >= 0; i-- {
		body.WriteString("\n\n- ")
		body.WriteString(commits[i].ShortHash)
		body.WriteString(" ")
		body.WriteString(commits[i].Subject)
	}
	return []string{subject, body.String()}
}

func keepExistingSelections(selected map[string]bool, commits []git.Commit) map[string]bool {
	if len(selected) == 0 {
		return map[string]bool{}
	}
	exists := map[string]bool{}
	for _, commit := range commits {
		exists[commit.Hash] = true
	}
	kept := map[string]bool{}
	for hash := range selected {
		if exists[hash] {
			kept[hash] = true
		}
	}
	return kept
}

func removeNotice(commitCount int, removedUncommitted bool) string {
	switch {
	case commitCount > 0 && removedUncommitted:
		return fmt.Sprintf("removed %d commits and discarded uncommitted changes", commitCount)
	case commitCount > 0:
		return fmt.Sprintf("removed %d commits", commitCount)
	case removedUncommitted:
		return "discarded uncommitted changes"
	default:
		return "nothing removed"
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
	return m.fileIdx
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

func (m model) viewSelectList(width int) string {
	if len(m.commits) == 0 {
		return mutedStyle.Render("no commits")
	}
	var b strings.Builder
	for i, commit := range m.commits {
		box := "[ ]"
		if m.selected[commit.Hash] {
			box = "[x]"
		}
		line := fmt.Sprintf("%s %s %s  %s", markerStyle.Render(box), hashStyle.Render(commit.ShortHash), commit.Date, commit.Subject)
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

func (m model) viewSelectDetailsPreview() string {
	var b strings.Builder
	count := m.selectedCount()
	b.WriteString(titleStyle.Render("Select Mode"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%d items selected", count))
	if count > 0 {
		if commits, err := m.selectedLatestRange(true); err == nil {
			if len(commits) > 0 {
				b.WriteString(mutedStyle.Render(fmt.Sprintf("  latest %d commits", len(commits))))
			}
			if m.selected[git.UncommittedHash] {
				b.WriteString(mutedStyle.Render("  uncommitted"))
			}
		} else {
			b.WriteString("  " + mutedStyle.Render(err.Error()))
		}
	}
	b.WriteString("\n\n")
	if m.pending != selectActionNone {
		b.WriteString(statusAltStyle.Render("Confirm " + m.pendingActionName()))
		b.WriteString(" ")
		b.WriteString(mutedStyle.Render("press y to continue or n/esc to cancel"))
		b.WriteString("\n")
	} else {
		b.WriteString(mutedStyle.Render("space selects items. remove can discard uncommitted changes; merge requires latest commits."))
		b.WriteString("\n")
	}
	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(markerStyle.Render(m.notice))
		b.WriteString("\n")
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(m.width)

	return style.Render(m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), ""))
}

func (m model) viewDirectoryList(width int) string {
	entries := m.visibleDirectoryEntries()
	if len(entries) == 0 {
		return mutedStyle.Render("no repository files")
	}
	var b strings.Builder
	for i, entry := range entries {
		kind := "[f]"
		name := entry.DisplayName
		if entry.IsDir {
			if m.expanded[entry.Path] {
				kind = "[-]"
			} else {
				kind = "[+]"
			}
			name += "/"
		}
		indent := strings.Repeat("  ", entry.Depth)
		detail := ""
		if entry.IsDir {
			detail = fmt.Sprintf("  %d files", entry.FileCount)
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
	entries := m.visibleDirectoryEntries()
	if len(entries) == 0 || m.dirIdx < 0 || m.dirIdx >= len(entries) {
		return ""
	}
	entry := entries[m.dirIdx]
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
	if entry.IsDir {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  %d files", entry.FileCount)))
	}
	b.WriteString("\n\n")
	if len(entry.CommitIndexes) > 0 {
		b.WriteString(mutedStyle.Render("Recent changes"))
		b.WriteString("\n")
	}
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

	return style.Render(m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), "  ... enter toggles folders or opens files"))
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

func (m model) viewFilesBody(height int) string {
	if height <= 0 {
		return ""
	}
	start := 0
	if len(m.files) > height {
		start = clamp(m.fileIdx-height/2, 0, len(m.files)-height)
	}
	end := min(len(m.files), start+height)
	lines := make([]string, 0, height)
	for i := start; i < end; i++ {
		line := fileStyle.Render(m.files[i])
		if i == m.fileIdx {
			line = cursorStyle.Render(line)
		}
		lines = append(lines, frameLine(line, m.width))
	}
	for len(lines) < height {
		lines = append(lines, frameLine("", m.width))
	}
	return strings.Join(lines, "\n")
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
	if m.lineNums && m.diffMode == diffUnified {
		lines = numberUnifiedDiffLines(lines, m.width)
	}
	for _, line := range lines[m.scroll:] {
		if m.lineNums && m.diffMode == diffUnified {
			b.WriteString(line)
		} else {
			b.WriteString(renderVisibleDiffLine(line, m.width, m.diffMode == diffSplit))
		}
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
	numberWidth := len(fmt.Sprint(max(1, len(m.fullLines))))
	for i, line := range m.fullLines[m.scroll:] {
		prefix := ""
		if m.lineNums {
			prefix = mutedStyle.Render(fmt.Sprintf("%*d │ ", numberWidth, m.scroll+i+1))
			if m.width > 0 {
				prefix = truncateVisible(prefix, m.width)
			}
		}
		if m.width > 0 {
			line = truncateVisible(line, max(1, m.width-lipgloss.Width(prefix)))
		}
		b.WriteString(prefix)
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
	files := currentDirectoryFilesFromCache(m.fileCache)
	if m.root != "" {
		var err error
		files, err = git.WorktreeFiles(m.root)
		if err != nil {
			m.err = err
			return
		}
	}
	if m.fileCache == nil {
		m.fileCache = map[string][]string{}
	}
	stats := map[string]*directoryStats{}
	currentFiles := make(map[string]bool, len(files))
	for _, file := range files {
		currentFiles[file] = true
		addDirectoryFile(stats, file)
	}
	for i, commit := range m.commits {
		m.loadCommitFilesAt(i)
		if m.err != nil {
			return
		}
		for _, file := range m.fileCache[commit.Hash] {
			if currentFiles[file] {
				addDirectoryCommit(stats, file, i)
			}
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
	m.dirIdx = clamp(m.dirIdx, 0, len(m.visibleDirectoryEntries())-1)
}

func currentDirectoryFilesFromCache(fileCache map[string][]string) []string {
	seen := map[string]bool{}
	var files []string
	for _, cached := range fileCache {
		for _, file := range cached {
			if !seen[file] {
				seen[file] = true
				files = append(files, file)
			}
		}
	}
	sort.Strings(files)
	return files
}

func addDirectoryFile(stats map[string]*directoryStats, file string) {
	file = strings.Trim(file, "/")
	if file == "" {
		return
	}
	ensureDirectoryStats(stats, file, false)
	for dir := pathDirectory(file); dir != ""; dir = pathDirectory(dir) {
		stat := ensureDirectoryStats(stats, dir, true)
		stat.files[file] = true
	}
}

func addDirectoryCommit(stats map[string]*directoryStats, file string, commitIndex int) {
	for path := file; path != ""; path = pathDirectory(path) {
		if stat, ok := stats[path]; ok {
			stat.commitIndexes[commitIndex] = true
		}
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

func (m model) visibleDirectoryEntries() []directoryEntry {
	if len(m.dirEntries) == 0 {
		return nil
	}
	var entries []directoryEntry
	for _, entry := range m.dirEntries {
		if m.directoryEntryVisible(entry) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (m model) directoryEntryVisible(entry directoryEntry) bool {
	parent := pathDirectory(entry.Path)
	for parent != "" {
		if !m.expanded[parent] {
			return false
		}
		parent = pathDirectory(parent)
	}
	return true
}

func (m *model) expandCurrentDirectoryPath() {
	if m.expanded == nil {
		m.expanded = map[string]bool{}
	}
	for _, dir := range m.currentDirectoryPathAncestors() {
		m.expanded[dir] = true
	}
	target := m.currentDirectoryTargetPath()
	if target == "" {
		m.dirIdx = clamp(m.dirIdx, 0, len(m.visibleDirectoryEntries())-1)
		return
	}
	for i, entry := range m.visibleDirectoryEntries() {
		if entry.Path == target {
			m.dirIdx = i
			return
		}
	}
	m.dirIdx = clamp(m.dirIdx, 0, len(m.visibleDirectoryEntries())-1)
}

func (m model) currentDirectoryPathAncestors() []string {
	target := m.currentDirectoryTargetPath()
	if target == "" {
		return nil
	}
	var dirs []string
	for dir := pathDirectory(target); dir != ""; dir = pathDirectory(dir) {
		dirs = append(dirs, dir)
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

func (m model) currentDirectoryTargetPath() string {
	if len(m.files) > 0 && m.fileIdx >= 0 && m.fileIdx < len(m.files) {
		return m.files[m.fileIdx]
	}
	if len(m.commits) > 0 && m.commitIdx >= 0 && m.commitIdx < len(m.commits) {
		files := m.fileCache[m.commits[m.commitIdx].Hash]
		if len(files) > 0 {
			return files[0]
		}
	}
	return ""
}

func (m *model) enterDirectoryEntry() {
	entries := m.visibleDirectoryEntries()
	if len(entries) == 0 || m.dirIdx < 0 || m.dirIdx >= len(entries) {
		return
	}
	entry := entries[m.dirIdx]
	if entry.IsDir {
		if m.expanded == nil {
			m.expanded = map[string]bool{}
		}
		if m.expanded[entry.Path] {
			delete(m.expanded, entry.Path)
		} else {
			m.expanded[entry.Path] = true
		}
		m.dirIdx = clamp(m.dirIdx, 0, len(m.visibleDirectoryEntries())-1)
		return
	}
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
	m.fileReturn = modeDirectories
	m.mode = modeFiles
}

func (m *model) collapseDirectoryEntry() {
	entries := m.visibleDirectoryEntries()
	if len(entries) == 0 || m.dirIdx < 0 || m.dirIdx >= len(entries) {
		return
	}
	entry := entries[m.dirIdx]
	if entry.IsDir && m.expanded[entry.Path] {
		delete(m.expanded, entry.Path)
		m.dirIdx = clamp(m.dirIdx, 0, len(m.visibleDirectoryEntries())-1)
		return
	}
	parent := pathDirectory(entry.Path)
	if parent == "" {
		return
	}
	delete(m.expanded, parent)
	for i, visible := range m.visibleDirectoryEntries() {
		if visible.Path == parent {
			m.dirIdx = i
			return
		}
	}
	m.dirIdx = clamp(m.dirIdx, 0, len(m.visibleDirectoryEntries())-1)
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
		return splitDiffWithLineNumbers(m.diffLines, width, m.lineNums)
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
	return splitDiffWithLineNumbers(lines, width, false)
}

type numberedDiffLine struct {
	text   string
	number int
}

func splitDiffWithLineNumbers(lines []string, width int, showNumbers bool) []string {
	leftWidth, rightWidth := splitColumnWidths(width)
	var out []string
	var pending *numberedDiffLine
	oldLine, newLine := 0, 0
	numberWidth := diffLineNumberWidth(lines)
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "@@") || strings.HasPrefix(line, `\`):
			if pending != nil {
				out = append(out, formatSplitNumbered(pending.text, "", pending.number, 0, leftWidth, rightWidth, true, false, showNumbers, numberWidth))
				pending = nil
			}
			out = append(out, truncateVisible(line, width))
			if strings.HasPrefix(line, "@@") {
				oldLine, newLine = parseHunkStarts(line)
			}
		case strings.HasPrefix(line, "-"):
			if pending != nil {
				out = append(out, formatSplitNumbered(pending.text, "", pending.number, 0, leftWidth, rightWidth, true, false, showNumbers, numberWidth))
			}
			text := strings.TrimPrefix(line, "-")
			pending = &numberedDiffLine{text: text, number: oldLine}
			oldLine++
		case strings.HasPrefix(line, "+"):
			text := strings.TrimPrefix(line, "+")
			if pending != nil {
				out = append(out, formatSplitNumbered(pending.text, text, pending.number, newLine, leftWidth, rightWidth, true, true, showNumbers, numberWidth))
				pending = nil
			} else {
				out = append(out, formatSplitNumbered("", text, 0, newLine, leftWidth, rightWidth, false, true, showNumbers, numberWidth))
			}
			newLine++
		default:
			if pending != nil {
				out = append(out, formatSplitNumbered(pending.text, "", pending.number, 0, leftWidth, rightWidth, true, false, showNumbers, numberWidth))
				pending = nil
			}
			text := strings.TrimPrefix(line, " ")
			out = append(out, formatSplitNumbered(text, text, oldLine, newLine, leftWidth, rightWidth, false, false, showNumbers, numberWidth))
			oldLine++
			newLine++
		}
	}
	if pending != nil {
		out = append(out, formatSplitNumbered(pending.text, "", pending.number, 0, leftWidth, rightWidth, true, false, showNumbers, numberWidth))
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
	return formatSplitNumbered(left, right, 0, 0, leftWidth, rightWidth, leftChanged, rightChanged, false, 0)
}

func formatSplitNumbered(left, right string, leftNumber, rightNumber, leftWidth, rightWidth int, leftChanged, rightChanged, showNumbers bool, numberWidth int) string {
	if leftChanged {
		left = ansi.Strip(left)
	}
	if rightChanged {
		right = ansi.Strip(right)
	}
	leftCell := formatNumberedCell(left, leftNumber, leftWidth, showNumbers, numberWidth)
	rightCell := formatNumberedCell(right, rightNumber, rightWidth, showNumbers, numberWidth)
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

func formatNumberedCell(content string, number, width int, showNumbers bool, numberWidth int) string {
	prefix := ""
	if showNumbers {
		value := ""
		if number > 0 {
			value = fmt.Sprint(number)
		}
		prefix = fmt.Sprintf("%*s ", numberWidth, value)
		if lipgloss.Width(prefix) > width {
			prefix = truncateVisible(prefix, width)
		}
	}
	contentWidth := max(0, width-lipgloss.Width(prefix))
	return prefix + padPlain(truncateVisible(content, contentWidth), contentWidth)
}

func numberUnifiedDiffLines(lines []string, width int) []string {
	numberWidth := diffLineNumberWidth(lines)
	oldLine, newLine := 0, 0
	numbered := make([]string, 0, len(lines))
	for _, line := range lines {
		oldNumber, newNumber := 0, 0
		switch {
		case strings.HasPrefix(line, "@@"):
			oldLine, newLine = parseHunkStarts(line)
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, `\`):
		case strings.HasPrefix(line, "-"):
			oldNumber = oldLine
			oldLine++
		case strings.HasPrefix(line, "+"):
			newNumber = newLine
			newLine++
		default:
			oldNumber, newNumber = oldLine, newLine
			oldLine++
			newLine++
		}
		if oldNumber == 0 && newNumber == 0 {
			numbered = append(numbered, renderVisibleDiffLine(line, width, false))
			continue
		}
		oldText, newText := "", ""
		if oldNumber > 0 {
			oldText = fmt.Sprint(oldNumber)
		}
		if newNumber > 0 {
			newText = fmt.Sprint(newNumber)
		}
		prefix := mutedStyle.Render(fmt.Sprintf("%*s %*s │ ", numberWidth, oldText, numberWidth, newText))
		numbered = append(numbered, prefix+renderVisibleDiffLine(line, max(1, width-lipgloss.Width(prefix)), false))
	}
	return numbered
}

func diffLineNumberWidth(lines []string) int {
	maxLine := 1
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			oldLine, newLine := parseHunkStarts(line)
			maxLine = max(maxLine, max(oldLine, newLine))
		}
	}
	return len(fmt.Sprint(maxLine + len(lines)))
}

func parseHunkStarts(line string) (int, int) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, 0
	}
	return parseDiffRangeStart(fields[1]), parseDiffRangeStart(fields[2])
}

func parseDiffRangeStart(value string) int {
	value = strings.TrimLeft(value, "+-")
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		value = value[:comma]
	}
	var start int
	_, _ = fmt.Sscanf(value, "%d", &start)
	return start
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

func frameLine(s string, width int) string {
	if width <= 0 {
		return s + "\x1b[K"
	}
	return padPlain(truncateVisible(s, width), width) + "\x1b[K"
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
	case modeSelect:
		return "select"
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
