package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/store"
	"github.com/minkuik/agentgit/internal/transcript"

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

type searchKind int

const (
	searchKindFiles searchKind = iota
	searchKindCurrentFile
	searchKindWorktreeContent
)

type model struct {
	root               string
	branch             string
	head               string
	limit              int
	commits            []git.Commit
	requests           []transcript.Request
	requestsLoading    bool
	requestsLoadSeq    int
	requestsCmdContext context.Context
	requestsCmdCancel  context.CancelFunc
	requestsUpdatedAt  time.Time
	requestsCache      *transcript.Cache
	files              []string
	diffLines          []string
	fullLines          []string
	dirEntries         []directoryEntry
	expanded           map[string]bool
	fileCache          map[string][]string
	fileStatusCache    map[string]map[string]string
	diffCache          map[string][]string
	diffCacheKeys      []string
	fullCache          map[string][]string
	fullCacheKeys      []string
	selected           map[string]bool
	mode               mode
	pending            selectAction
	fileReturn         mode
	requestReturn      mode
	requestDrawer      bool
	worktreeFile       bool
	diffMode           diffMode
	commitIdx          int
	dirIdx             int
	requestIdx         int
	fileIdx            int
	scroll             int
	width              int
	height             int
	err                error
	notice             string
	helpOpen           bool
	lineNums           bool
	wrapLines          bool
	searchOpen         bool
	searchKind         searchKind
	searchText         string
	searchIdx          int
	searchFiles        []string
	searchResults      []fileSearchResult
}

const renderedFileCacheLimit = 50

type imageOpenMsg struct {
	err error
}

type requestsLoadedMsg struct {
	seq      int
	requests []transcript.Request
	err      error
	at       time.Time
}

type fileSearchResult struct {
	Path      string
	Line      int
	Text      string
	Positions []int
	Score     int
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

type selectAction int

const (
	selectActionNone selectAction = iota
	selectActionRemove
	selectActionSquash
)

var (
	hashStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	providerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	requestStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	markerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	fileStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	dirStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	addLineStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Background(lipgloss.Color("22"))
	delLineStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Background(lipgloss.Color("52"))
	hunkStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	cursorStyle    = lipgloss.NewStyle().Reverse(true)
	titleStyle     = lipgloss.NewStyle().Bold(true)
	commandStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("7"))
	commandLabel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11")).Padding(0, 1)
	statusAltStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("13")).Padding(0, 1)
	keyStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11")).Padding(0, 1)
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

var (
	highlightStyle     = firstChromaStyle("monokai")
	highlightFormatter = firstChromaFormatter("terminal256")
	highlightLexers    = map[string]chroma.Lexer{}
	highlightLexersMu  sync.RWMutex
)

func Run(root string, limit int) error {
	commits, err := git.CommitsWithUncommitted(root, limit)
	if err != nil {
		return err
	}
	if _, err := store.Init(); err != nil {
		return err
	}
	if !isTTY(os.Stdout) || !isTTY(os.Stdin) {
		return PrintStatic(os.Stdout, commits)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := model{
		root:               root,
		branch:             git.Branch(root),
		head:               git.ShortHead(root),
		limit:              limit,
		commits:            commits,
		requestsLoading:    true,
		requestsLoadSeq:    1,
		requestsCmdContext: ctx,
		requestsCmdCancel:  cancel,
		requestsCache:      transcript.NewCache(),
		fileCache:          map[string][]string{},
		fileStatusCache:    map[string]map[string]string{},
		diffCache:          map[string][]string{},
		fullCache:          map[string][]string{},
		expanded:           map[string]bool{},
		selected:           map[string]bool{},
	}
	m.loadCommitFiles()
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func Highlight(filename, code string) []string {
	lexer := cachedLexer(filename)
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return strings.Split(code, "\n")
	}
	var buf strings.Builder
	if err := highlightFormatter.Format(&buf, highlightStyle, iterator); err != nil {
		return strings.Split(code, "\n")
	}
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func cachedLexer(filename string) chroma.Lexer {
	key := lexerCacheKey(filename)
	highlightLexersMu.RLock()
	lexer := highlightLexers[key]
	highlightLexersMu.RUnlock()
	if lexer != nil {
		return lexer
	}
	lexer = lexers.Get(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	highlightLexersMu.Lock()
	if existing := highlightLexers[key]; existing != nil {
		lexer = existing
	} else {
		highlightLexers[key] = lexer
	}
	highlightLexersMu.Unlock()
	return lexer
}

func lexerCacheKey(filename string) string {
	base := strings.ToLower(filepath.Base(filename))
	if ext := strings.ToLower(filepath.Ext(base)); ext != "" {
		return ext
	}
	return base
}

func firstChromaStyle(name string) *chroma.Style {
	style := styles.Get(name)
	if style == nil {
		return styles.Fallback
	}
	return style
}

func firstChromaFormatter(name string) chroma.Formatter {
	formatter := formatters.Get(name)
	if formatter == nil {
		return formatters.Fallback
	}
	return formatter
}

func PrintStatic(w io.Writer, commits []git.Commit) error {
	for _, commit := range commits {
		if _, err := fmt.Fprintf(w, "%s %s  %s\n", hashStyle.Render(commit.ShortHash), commit.Date, commit.Subject); err != nil {
			return err
		}
	}
	return nil
}

func (m model) Init() tea.Cmd {
	if m.requestsLoading && m.requestsCmdContext != nil {
		return m.loadRequestsCmd(m.requestsCmdContext, m.requestsLoadSeq)
	}
	return nil
}

func (m model) loadRequestsCmd(ctx context.Context, seq int) tea.Cmd {
	root := m.root
	cache := m.requestsCache
	return func() tea.Msg {
		requests, err := transcript.RequestsByRepoContext(ctx, root, cache)
		if errors.Is(err, context.Canceled) {
			return requestsLoadedMsg{seq: seq, err: err, at: time.Now()}
		}
		return requestsLoadedMsg{seq: seq, requests: requests, err: err, at: time.Now()}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.searchOpen {
			return m.updateSearch(msg)
		}
		if m.helpOpen {
			switch msg.String() {
			case "?", "esc", "enter":
				m.helpOpen = false
				return m, nil
			case "ctrl+c":
				return m, m.quitCmd()
			default:
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c":
			return m, m.quitCmd()
		case "esc":
			if m.mode == modeSelect && m.pending != selectActionNone {
				m.cancelPendingSelectAction()
				return m, nil
			}
			if m.requestDrawer && m.mode != modeRequest {
				m.requestDrawer = false
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
			if m.mode == modeRequest {
				m.back()
			} else if m.requestDrawer {
				m.requestDrawer = false
			} else if m.mode == modeDirectories {
				m.collapseDirectoryEntry()
			} else {
				m.back()
			}
		case "backspace":
			m.clearNotice()
			if m.mode == modeRequest || !m.requestDrawer {
				m.back()
			} else {
				m.requestDrawer = false
			}
		case "m":
			if m.mode == modeSelect {
				m.requestSelectAction(selectActionSquash)
			} else if m.mode == modeDiff && m.wrapLines {
				m.wrapLines = false
				m.diffMode = diffSplit
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
		case "w":
			if m.mode == modeCommits || m.mode == modeRequest || m.mode == modeDiff || m.mode == modeFullFile {
				m.wrapLines = !m.wrapLines
				m.scroll = 0
				if m.wrapLines && m.mode == modeDiff {
					m.diffMode = diffUnified
				}
			}
		case "r":
			m.clearNotice()
			return m, m.refresh()
		case "v":
			m.toggleRequestDrawer()
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
				return m, m.confirmSelectAction()
			}
		case "?":
			m.helpOpen = true
		case "ctrl+p":
			m.openSearch()
		case "/":
			m.openCurrentFileSearch()
		case "ctrl+f":
			m.openCurrentFileSearch()
		case "ctrl+shift+f", "alt+/":
			m.openWorktreeContentSearch()
		}
	case imageOpenMsg:
		if msg.err != nil {
			m.err = msg.err
		}
	case requestsLoadedMsg:
		if msg.seq != m.requestsLoadSeq {
			return m, nil
		}
		m.requestsLoading = false
		m.requestsCmdContext = nil
		m.requestsCmdCancel = nil
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		if msg.err != nil {
			m.notice = "request load failed: " + msg.err.Error()
			return m, nil
		}
		selectedRequestID := ""
		if len(m.requests) > 0 && m.requestIdx >= 0 && m.requestIdx < len(m.requests) {
			selectedRequestID = m.requests[m.requestIdx].ID
		}
		m.requests = msg.requests
		m.requestsUpdatedAt = msg.at
		m.requestIdx = 0
		for i, req := range m.requests {
			if req.ID == selectedRequestID {
				m.requestIdx = i
				break
			}
		}
	}
	return m, nil
}

func (m *model) quitCmd() tea.Cmd {
	if m.requestsCmdCancel != nil {
		m.requestsCmdCancel()
		m.requestsCmdCancel = nil
		m.requestsCmdContext = nil
	}
	return tea.Quit
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, m.quitCmd()
	case "esc":
		m.closeSearch()
	case "enter":
		m.selectSearchResult()
	case "up":
		m.searchIdx = clamp(m.searchIdx-1, 0, len(m.searchResults)-1)
		m.syncCurrentSearchScroll()
	case "down":
		m.searchIdx = clamp(m.searchIdx+1, 0, len(m.searchResults)-1)
		m.syncCurrentSearchScroll()
	case "backspace":
		runes := []rune(m.searchText)
		if len(runes) > 0 {
			m.searchText = string(runes[:len(runes)-1])
			m.updateSearchResults()
		}
	case "ctrl+u":
		m.searchText = ""
		m.updateSearchResults()
	case " ":
		m.searchText += " "
		m.updateSearchResults()
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			m.searchText += string(msg.Runes)
			m.updateSearchResults()
		}
	}
	return m, nil
}

func (m *model) openSearch() {
	files, err := git.WorktreeFiles(m.root)
	if err != nil {
		m.err = err
		return
	}
	m.searchOpen = true
	m.searchKind = searchKindFiles
	m.searchText = ""
	m.searchIdx = 0
	m.searchFiles = files
	m.updateSearchResults()
}

func (m *model) openCurrentFileSearch() {
	if !m.currentFileSearchAvailable() {
		m.notice = "open a diff or full file before searching within a file"
		return
	}
	m.searchOpen = true
	m.searchKind = searchKindCurrentFile
	m.searchText = ""
	m.searchIdx = 0
	m.searchFiles = nil
	m.updateSearchResults()
}

func (m *model) openWorktreeContentSearch() {
	files, err := git.WorktreeFiles(m.root)
	if err != nil {
		m.err = err
		return
	}
	m.searchOpen = true
	m.searchKind = searchKindWorktreeContent
	m.searchText = ""
	m.searchIdx = 0
	m.searchFiles = files
	m.updateSearchResults()
}

func (m *model) closeSearch() {
	m.searchOpen = false
	m.searchKind = searchKindFiles
	m.searchText = ""
	m.searchIdx = 0
	m.searchFiles = nil
	m.searchResults = nil
}

func (m *model) updateSearchResults() {
	switch m.searchKind {
	case searchKindCurrentFile:
		m.searchResults = m.currentFileContentMatches(m.searchText)
	case searchKindWorktreeContent:
		m.searchResults = m.worktreeContentMatches(m.searchText)
	default:
		m.searchResults = fuzzyFileMatches(m.searchFiles, m.searchText)
	}
	m.searchIdx = clamp(m.searchIdx, 0, len(m.searchResults)-1)
	m.syncCurrentSearchScroll()
}

func (m *model) syncCurrentSearchScroll() {
	if !m.searchOpen || m.searchKind != searchKindCurrentFile || len(m.searchResults) == 0 || m.searchIdx < 0 || m.searchIdx >= len(m.searchResults) {
		return
	}
	m.scroll = max(0, m.searchResults[m.searchIdx].Line-4)
}

func (m *model) selectSearchResult() {
	if len(m.searchResults) == 0 || m.searchIdx < 0 || m.searchIdx >= len(m.searchResults) {
		return
	}
	result := m.searchResults[m.searchIdx]
	if m.searchKind == searchKindCurrentFile {
		m.scroll = max(0, result.Line-1)
		m.closeSearch()
		return
	}
	if m.searchKind == searchKindWorktreeContent {
		m.openWorktreeSearchResult(result)
		return
	}
	path := result.Path
	m.closeSearch()
	m.loadDirectoryEntries()
	if m.err != nil {
		return
	}
	m.expanded = map[string]bool{}
	for dir := pathDirectory(path); dir != ""; dir = pathDirectory(dir) {
		m.expanded[dir] = true
	}
	m.mode = modeDirectories
	m.scroll = 0
	for i, entry := range m.visibleDirectoryEntries() {
		if entry.Path == path {
			m.dirIdx = i
			return
		}
	}
	m.dirIdx = clamp(m.dirIdx, 0, len(m.visibleDirectoryEntries())-1)
}

func (m model) View() string {
	if m.err != nil {
		return "agentgit: " + m.err.Error() + "\n"
	}
	content, focusLine := m.contentView()
	if m.width <= 0 || m.height <= 0 {
		base := content
		if status := m.viewStatusBarAtWidth(120); status != "" {
			base = strings.TrimRight(base, "\n") + "\n" + status
		}
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
	width := m.frameInnerWidth()
	switch m.mode {
	case modeCommits:
		return m.viewCommitsList(width), m.commitFocusLine()
	case modeSelect:
		return m.viewSelectList(width), m.commitFocusLine()
	case modeDirectories:
		return m.viewDirectoryList(width), m.directoryFocusLine()
	case modeFiles:
		return "", m.fileFocusLine()
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

func (m model) currentFileSearchAvailable() bool {
	if m.mode != modeDiff && m.mode != modeFullFile {
		return false
	}
	return len(m.currentSearchLines()) > 0
}

func (m model) currentSearchPath() string {
	if len(m.files) == 0 || m.fileIdx < 0 || m.fileIdx >= len(m.files) {
		return ""
	}
	return m.files[m.fileIdx]
}

func (m model) currentSearchLines() []string {
	switch m.mode {
	case modeDiff:
		return m.visibleDiffLines()
	case modeFullFile:
		return m.fullLines
	default:
		return nil
	}
}

func (m model) currentFileContentMatches(query string) []fileSearchResult {
	path := m.currentSearchPath()
	if path == "" {
		return nil
	}
	return contentLineMatches(path, m.currentSearchLines(), query)
}

func (m model) worktreeContentMatches(query string) []fileSearchResult {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	var results []fileSearchResult
	for _, path := range m.searchFiles {
		data, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(path)))
		if err != nil || bytes.Contains(data, []byte{0}) {
			continue
		}
		results = append(results, contentLineMatches(path, splitContentLines(string(data)), query)...)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		return results[i].Line < results[j].Line
	})
	return results
}

func (m *model) openWorktreeSearchResult(result fileSearchResult) {
	m.closeSearch()
	m.files = []string{result.Path}
	m.fileIdx = 0
	m.mode = modeFullFile
	m.fileReturn = modeDirectories
	m.worktreeFile = true
	m.scroll = max(0, result.Line-1)
	m.loadWorktreeFile()
}

func contentLineMatches(path string, lines []string, query string) []fileSearchResult {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	lowerQuery := []rune(strings.ToLower(query))
	results := make([]fileSearchResult, 0)
	for i, line := range lines {
		plain := ansi.Strip(line)
		lowerLine := []rune(strings.ToLower(plain))
		index := runeSliceIndex(lowerLine, lowerQuery)
		if index < 0 {
			continue
		}
		results = append(results, fileSearchResult{
			Path:      path,
			Line:      i + 1,
			Text:      plain,
			Positions: contiguousPositions(index, len(lowerQuery)),
			Score:     1,
		})
	}
	return results
}

func runeSliceIndex(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		matched := true
		for j, char := range needle {
			if haystack[i+j] != char {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

func splitContentLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func contiguousPositions(start, length int) []int {
	positions := make([]int, 0, length)
	for i := 0; i < length; i++ {
		positions = append(positions, start+i)
	}
	return positions
}

func renderContentSearchText(text string, positions []int) string {
	return renderMatchedText(text, positions)
}

func renderMatchedText(text string, positions []int) string {
	if len(positions) == 0 {
		return text
	}
	matched := make(map[int]bool, len(positions))
	for _, position := range positions {
		matched[position] = true
	}
	var b strings.Builder
	for i, char := range []rune(text) {
		value := string(char)
		if matched[i] {
			b.WriteString(markerStyle.Bold(true).Render(value))
		} else {
			b.WriteString(value)
		}
	}
	return b.String()
}

func (m model) currentFileSearchPositions(line int) []int {
	if !m.searchOpen || m.searchKind != searchKindCurrentFile {
		return nil
	}
	if m.searchText == "" {
		return nil
	}
	for _, result := range m.searchResults {
		if result.Line == line {
			return result.Positions
		}
	}
	return nil
}

func (m model) highlightCurrentFileSearchLine(line string, lineNumber int) string {
	positions := m.currentFileSearchPositions(lineNumber)
	if len(positions) == 0 {
		return line
	}
	plain := ansi.Strip(line)
	if !positionsMatchQuery(plain, positions, m.searchText) {
		index := runeSliceIndex([]rune(strings.ToLower(plain)), []rune(strings.ToLower(m.searchText)))
		if index < 0 {
			return line
		}
		positions = contiguousPositions(index, len([]rune(m.searchText)))
	}
	return renderMatchedText(plain, positions)
}

func positionsMatchQuery(text string, positions []int, query string) bool {
	textRunes := []rune(strings.ToLower(text))
	queryRunes := []rune(strings.ToLower(query))
	if len(positions) != len(queryRunes) {
		return false
	}
	for i, position := range positions {
		if position < 0 || position >= len(textRunes) || textRunes[position] != queryRunes[i] {
			return false
		}
	}
	return true
}

func (m model) viewFrame(content string, focusLine int) string {
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
	statusHeight := 1
	frameHeight := max(0, m.height-statusHeight)
	drawerHeight := 0
	if m.requestDrawer && m.mode != modeRequest {
		drawerHeight = m.requestDrawerHeight(frameHeight)
	}
	bodyHeight := max(0, frameHeight-2-topHeight-drawerHeight)
	var body string
	if m.searchOpen {
		searchHeight := m.searchOverlayHeight(bodyHeight)
		contentHeight := max(0, bodyHeight-searchHeight)
		searchBody := m.viewSearchBody(searchHeight)
		var contentBody string
		if m.mode == modeFiles {
			contentBody = m.viewFilesBody(contentHeight)
		} else {
			contentBody = m.viewBody(content, contentHeight, focusLine)
		}
		body = strings.TrimRight(searchBody, "\n")
		if contentHeight > 0 {
			body += "\n" + contentBody
		}
	} else if m.mode == modeFiles {
		body = m.viewFilesBody(bodyHeight)
	} else {
		body = m.viewBody(content, bodyHeight, focusLine)
	}
	if drawerHeight > 0 {
		body = strings.TrimRight(body, "\n")
		if body != "" {
			body += "\n"
		}
		body += m.viewRequestDrawer(drawerHeight)
	}
	return m.renderPanelFrame(staticTop, body, frameHeight) + "\n" + m.viewStatusBar()
}

func (m model) searchOverlayHeight(available int) int {
	if available <= 0 {
		return 0
	}
	if available < 5 {
		return available
	}
	maxHeight := 10
	if m.searchKind == searchKindCurrentFile {
		maxHeight = 7
	}
	return clamp(available/3, 5, min(maxHeight, available))
}

func (m model) viewStatusBar() string {
	return m.viewStatusBarAtWidth(m.width)
}

func (m model) viewStatusBarAtWidth(width int) string {
	if width <= 0 {
		width = 120
	}
	left := m.viewContextLine()
	target := m.targetContextLine()
	if target != "" {
		left += "  " + target
	}
	center := fmt.Sprintf("%s  %s", emptyFallback(m.branch, "unknown"), emptyFallback(m.head, "unknown"))
	if dirty := m.dirtyFileCount(); dirty > 0 {
		center += fmt.Sprintf("  dirty %d", dirty)
	}
	right := m.commandContextLine()
	return renderStatusBar(width, left, center, right)
}

func (m model) viewSearchBody(height int) string {
	if height <= 0 {
		return ""
	}
	frameWidth := m.frameInnerWidth()
	width := max(1, frameWidth-2)
	if frameWidth < 48 {
		width = frameWidth
	}
	dialogHeight := clamp(height-2, 3, 10)
	dialog := m.viewSearchDialog(width, dialogHeight)
	return lipgloss.Place(m.frameInnerWidth(), height, lipgloss.Center, lipgloss.Top, dialog)
}

func (m model) viewSearchDialog(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	innerWidth := max(1, width-4)
	rows := []string{
		truncateVisible(m.searchPrompt()+" "+emptyFallback(m.searchText, ""), innerWidth),
		mutedStyle.Render(truncateVisible(fmt.Sprintf("matches %d", len(m.searchResults)), innerWidth)),
		mutedStyle.Render(strings.Repeat("─", innerWidth)),
	}
	listHeight := max(1, height-len(rows)-2)
	if len(m.searchResults) == 0 {
		rows = append(rows, mutedStyle.Render("No matching files"))
		for len(rows) < height {
			rows = append(rows, "")
		}
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("13")).
			Padding(0, 1).
			Width(width).
			Render(strings.Join(rows, "\n"))
	}
	start := 0
	if len(m.searchResults) > listHeight {
		start = clamp(m.searchIdx-listHeight/2, 0, len(m.searchResults)-listHeight)
	}
	end := min(len(m.searchResults), start+listHeight)
	for i := start; i < end; i++ {
		result := m.searchResults[i]
		line := m.renderSearchResult(result)
		if i == m.searchIdx {
			line = cursorStyle.Render(line)
		}
		rows = append(rows, truncateVisible(line, innerWidth))
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("13")).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(rows, "\n"))
}

func (m model) searchPrompt() string {
	switch m.searchKind {
	case searchKindCurrentFile:
		return "/"
	case searchKindWorktreeContent:
		return "alt+/"
	default:
		return "/"
	}
}

func (m model) searchTitle() string {
	switch m.searchKind {
	case searchKindCurrentFile:
		return "file search"
	case searchKindWorktreeContent:
		return "content search"
	default:
		return "file picker"
	}
}

func (m model) searchContextTitle() string {
	if m.searchKind == searchKindFiles {
		return "search"
	}
	return m.searchTitle()
}

func (m model) emptySearchHint() string {
	switch m.searchKind {
	case searchKindCurrentFile:
		return "type to search this file"
	case searchKindWorktreeContent:
		return "type to search all files"
	default:
		return "type to filter files"
	}
}

func (m model) renderSearchResult(result fileSearchResult) string {
	if result.Line <= 0 {
		return renderFuzzyPath(result)
	}
	if m.searchKind == searchKindWorktreeContent {
		return m.renderWorktreeContentSearchResult(result)
	}
	if m.searchKind == searchKindCurrentFile {
		return m.renderCurrentFileSearchResult(result)
	}
	location := fileStyle.Render(fmt.Sprintf("%s:%d", result.Path, result.Line))
	text := trimSearchResultText(result.Text)
	if text == "" {
		return location
	}
	return location + mutedStyle.Render("  ") + renderContentSearchText(text, result.Positions)
}

func (m model) renderCurrentFileSearchResult(result fileSearchResult) string {
	line := fileStyle.Render(fmt.Sprintf("line %d", result.Line))
	text := trimSearchResultText(result.Text)
	if text == "" {
		return line
	}
	width := max(1, m.frameInnerWidth()-8)
	lineWidth := clamp(len(fmt.Sprintf("line %d", result.Line)), 6, 12)
	textWidth := max(1, width-lineWidth-3)
	return padPlain(line, lineWidth) + mutedStyle.Render("  ") + truncateVisible(renderContentSearchText(text, result.Positions), textWidth)
}

func (m model) renderWorktreeContentSearchResult(result fileSearchResult) string {
	width := max(1, m.frameInnerWidth()-8)
	nameWidth := clamp(width/3, 12, 28)
	textWidth := max(1, width-nameWidth-3)
	name := filepath.Base(filepath.FromSlash(result.Path))
	location := fileStyle.Render(padPlain(truncateVisible(name, nameWidth), nameWidth))
	text := trimSearchResultText(result.Text)
	if text == "" {
		return location
	}
	return location + mutedStyle.Render("  ") + truncateVisible(renderContentSearchText(text, result.Positions), textWidth)
}

func trimSearchResultText(text string) string {
	return strings.TrimRight(text, " \t\r\n")
}

func fuzzyFileMatches(files []string, query string) []fileSearchResult {
	results := make([]fileSearchResult, 0, len(files))
	for _, file := range files {
		positions, score, ok := fuzzyPathMatch(file, query)
		if !ok {
			continue
		}
		results = append(results, fileSearchResult{
			Path:      file,
			Positions: positions,
			Score:     score,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if query == "" {
			return results[i].Path < results[j].Path
		}
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if len(results[i].Path) != len(results[j].Path) {
			return len(results[i].Path) < len(results[j].Path)
		}
		return results[i].Path < results[j].Path
	})
	return results
}

func fuzzyPathMatch(candidate, query string) ([]int, int, bool) {
	if query == "" {
		return nil, 0, true
	}
	candidateRunes := []rune(candidate)
	queryRunes := []rune(strings.ToLower(query))
	lowerCandidate := []rune(strings.ToLower(candidate))
	basenameStart := 0
	for i, char := range candidateRunes {
		if char == '/' {
			basenameStart = i + 1
		}
	}
	positions := make([]int, 0, len(queryRunes))
	searchFrom := 0
	score := 0
	lastPosition := -2
	for _, target := range queryRunes {
		position := -1
		for i := searchFrom; i < len(lowerCandidate); i++ {
			if lowerCandidate[i] == target {
				position = i
				break
			}
		}
		if position < 0 {
			return nil, 0, false
		}
		positions = append(positions, position)
		score += 10
		if position == lastPosition+1 {
			score += 18
		}
		if position == 0 || candidateRunes[position-1] == '/' || candidateRunes[position-1] == '-' || candidateRunes[position-1] == '_' || candidateRunes[position-1] == '.' {
			score += 14
		}
		if position >= basenameStart {
			score += 5
		}
		lastPosition = position
		searchFrom = position + 1
	}
	lowerQuery := strings.ToLower(query)
	lowerBase := strings.ToLower(filepath.Base(filepath.FromSlash(candidate)))
	if strings.Contains(lowerBase, lowerQuery) {
		score += 100
	}
	if lowerBase == lowerQuery {
		score += 100
	}
	score -= len(candidateRunes) / 4
	return positions, score, true
}

func renderFuzzyPath(result fileSearchResult) string {
	if len(result.Positions) == 0 {
		return fileStyle.Render(result.Path)
	}
	matched := make(map[int]bool, len(result.Positions))
	for _, position := range result.Positions {
		matched[position] = true
	}
	var b strings.Builder
	for i, char := range []rune(result.Path) {
		text := string(char)
		if matched[i] {
			b.WriteString(markerStyle.Bold(true).Render(text))
		} else {
			b.WriteString(fileStyle.Render(text))
		}
	}
	return b.String()
}

func renderStatusBar(width int, left, center, right string) string {
	if width <= 0 {
		return strings.Join(nonEmptyParts([]string{left, center, right}), "  ")
	}
	if width < 40 {
		return commandStyle.Width(width).Render(truncateVisible(left, width))
	}
	leftWidth := max(8, width/4)
	rightWidth := max(12, width/2)
	centerWidth := max(0, width-leftWidth-rightWidth)
	line := truncateVisible(left, leftWidth)
	line = padPlain(line, leftWidth)
	line += truncateVisible(center, centerWidth)
	line = padPlain(line, leftWidth+centerWidth)
	line += truncateVisible(right, rightWidth)
	return commandStyle.Width(width).Render(frameLine(line, width))
}

func nonEmptyParts(parts []string) []string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

func (m model) repositoryContextLine(width int) string {
	return compactPath(m.root, max(1, width/2-10))
}

func (m model) gitContextLine() string {
	return fmt.Sprintf("branch %s  head %s  commits %d  dirty %d",
		emptyFallback(m.branch, "unknown"),
		emptyFallback(m.head, "unknown"),
		len(m.visibleCommits()),
		m.dirtyFileCount(),
	)
}

func (m model) viewContextLine() string {
	if m.searchOpen {
		query := m.searchText
		if query == "" {
			query = m.emptySearchHint()
		}
		return fmt.Sprintf("%s  query %s  matches %d", m.searchContextTitle(), query, len(m.searchResults))
	}
	wrap := ""
	if m.mode == modeDiff || m.mode == modeFullFile || m.mode == modeRequest {
		wrap = fmt.Sprintf("  wrap %s", onOff(m.wrapLines))
	}
	return fmt.Sprintf("%s  diff %s%s", m.modeName(), m.diffModeName(), wrap)
}

func (m model) targetContextLine() string {
	if m.searchOpen {
		if len(m.searchResults) == 0 || m.searchIdx < 0 || m.searchIdx >= len(m.searchResults) {
			return "no matches"
		}
		result := m.searchResults[m.searchIdx]
		if result.Line > 0 {
			return fmt.Sprintf("%s:%d", result.Path, result.Line)
		}
		return result.Path
	}
	return m.selectionTitle()
}

func (m model) frameInnerWidth() int {
	if m.width <= 0 {
		return 120
	}
	return max(1, m.width-2)
}

func (m model) panelTitle() string {
	if m.searchOpen {
		return " " + titleCase(m.searchTitle()) + " "
	}
	switch m.mode {
	case modeCommits:
		return " Commit View "
	case modeSelect:
		return " Select Commits "
	case modeDirectories:
		return " Directory View "
	case modeFiles:
		return " Files "
	case modeDiff:
		return " Diff View "
	case modeFullFile:
		return " File View "
	case modeRequest:
		return " Request View "
	default:
		return " View "
	}
}

func (m model) renderPanelFrame(staticTop, body string, height int) string {
	if m.width <= 0 || height <= 0 {
		if staticTop == "" {
			return body
		}
		return staticTop + "\n" + body
	}
	innerWidth := max(1, m.width-2)
	innerHeight := max(1, height-2)
	lines := make([]string, 0, innerHeight)
	if staticTop != "" {
		lines = append(lines, splitViewLines(staticTop)...)
		lines = append(lines, mutedStyle.Render(strings.Repeat("─", innerWidth)))
	}
	if body != "" {
		lines = append(lines, splitViewLines(body)...)
	}
	if len(lines) > innerHeight {
		lines = lines[:innerHeight]
	}
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = frameLine(line, innerWidth)
	}
	title := centerTitle(m.panelTitle(), innerWidth)
	var b strings.Builder
	b.WriteString(mutedStyle.Render("╭"))
	b.WriteString(title)
	b.WriteString(mutedStyle.Render("╮"))
	b.WriteByte('\n')
	for _, line := range lines {
		b.WriteString(mutedStyle.Render("│"))
		b.WriteString(line)
		b.WriteString(mutedStyle.Render("│"))
		b.WriteByte('\n')
	}
	b.WriteString(mutedStyle.Render("╰"))
	b.WriteString(mutedStyle.Render(strings.Repeat("─", innerWidth)))
	b.WriteString(mutedStyle.Render("╯"))
	return b.String()
}

func centerTitle(title string, width int) string {
	title = truncateVisible(title, width)
	titleWidth := ansi.StringWidth(ansi.Strip(title))
	if titleWidth >= width {
		return title
	}
	left := max(0, (width-titleWidth)/2)
	right := max(0, width-titleWidth-left)
	return mutedStyle.Render(strings.Repeat("─", left)) + titleStyle.Render(title) + mutedStyle.Render(strings.Repeat("─", right))
}

func (m model) commandContextLine() string {
	if m.searchOpen {
		return "[Enter] Open  [Esc] Close  [Up/Down] Select  [Backspace] Delete"
	}
	switch m.mode {
	case modeCommits:
		return "[s] Select  [w] Wrap  [Ctrl+P] Files  [Alt+/] Grep  [?] Help"
	case modeSelect:
		if m.pending != selectActionNone {
			return "[y] Confirm  [n/Esc] Cancel  [?] Help"
		}
		return "[Space] Select  [x] Delete  [m] Merge  [s] Back  [?] Help"
	case modeDiff:
		return "[w] Wrap  [l] Lines  [f] Full file  [/] Find  [Ctrl+P] Files  [Alt+/] Grep"
	case modeFullFile:
		if m.worktreeFile {
			return "[w] Wrap  [l] Lines  [/] Find  [Ctrl+P] Files  [Alt+/] Grep"
		}
		return "[w] Wrap  [l] Lines  [f] Diff  [/] Find  [Ctrl+P] Files  [Alt+/] Grep"
	case modeRequest:
		return "[w] Wrap  [v] Back  [Ctrl+P] Files  [?] Help"
	}
	return "[Ctrl+P] Files  [Alt+/] Grep  [?] Help"
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (m model) selectionTitle() string {
	switch m.mode {
	case modeCommits, modeRequest, modeSelect:
		if m.mode == modeRequest {
			if req, ok := m.selectedRequest(); ok {
				return truncateVisible(fmt.Sprintf("%s %s", req.Agent, requestPreviewMessage(req.Message)), 80)
			}
			return "no requests"
		}
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

func (m model) selectedRequest() (transcript.Request, bool) {
	if len(m.requests) == 0 || m.requestIdx < 0 || m.requestIdx >= len(m.requests) {
		return transcript.Request{}, false
	}
	return m.requests[m.requestIdx], true
}

func (m model) requestReturnMode() mode {
	if m.requestReturn == 0 && m.mode == modeRequest {
		return modeCommits
	}
	return m.requestReturn
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

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func titleCase(value string) string {
	words := strings.Fields(value)
	for i, word := range words {
		runes := []rune(word)
		if len(runes) == 0 {
			continue
		}
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
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

	return m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), "  ... press v for full request")
}

func (m model) viewFileDetailsPreview() string {
	if len(m.commits) == 0 || len(m.files) == 0 {
		return ""
	}
	file := m.files[m.fileIdx]
	status := m.fileStatus(file, m.currentCommitHash())
	var b strings.Builder
	b.WriteString(titleStyle.Render("File Preview: ") + fileStyle.Render(file))
	b.WriteString("\n\n")
	if status != "" {
		b.WriteString(m.fileStatusStyle(status).Render(status))
		b.WriteString("\n\n")
	}
	if m.selectedFileIsImage() {
		b.WriteString(markerStyle.Render("Image file"))
		b.WriteByte('\n')
		b.WriteString(mutedStyle.Render("  press Enter to open image, Right for diff"))
		b.WriteString("\n\n")
	} else {
		b.WriteString(mutedStyle.Render("  press Enter/Right for diff"))
		b.WriteString("\n\n")
	}

	return m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), "")
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
		visible[i] = frameLine(line, m.frameInnerWidth())
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
	contentWidth := max(0, m.frameInnerWidth())
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
	bodyHeight := max(0, m.height-1)
	dialog := m.viewHelpDialog(m.width, bodyHeight)
	body := lipgloss.Place(m.width, bodyHeight, lipgloss.Center, lipgloss.Center, dialog)
	return body + "\n" + m.viewStatusBar()
}

func (m model) viewHelpDialog(width int, height int) string {
	dialogWidth := clamp(width-4, 48, 120)
	if width > 0 && dialogWidth > width {
		dialogWidth = width
	}
	contentWidth := max(0, dialogWidth-4)
	entries := m.helpEntries()
	keyWidth := 0
	for _, entry := range entries {
		keyWidth = max(keyWidth, lipgloss.Width(entry.keys))
	}
	keyWidth = clamp(keyWidth, 6, 20)

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
		maxContentLines := max(3, height-2)
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
		{"ctrl+p", "Files", "fuzzy find a repository file by path"},
		{"alt+/", "Grep", "search text across repository files"},
		{"?", "Close help", "return to the current screen"},
		{"ctrl+c", "Quit", "exit agentgit immediately"},
	}
	switch m.mode {
	case modeCommits:
		return append([]helpEntry{
			{"up/down", "Move cursor", "select a commit"},
			{"enter/right", "Open files", "show files changed by the selected commit"},
			{"s", "Select mode", "select latest commits for merge or delete"},
			{"tab", "Directories", "switch to directory summary"},
			{"v", "Requests", "toggle transcript request drawer"},
			{"w", "Wrap lines", "show complete commit lines"},
			{"r", "Refresh", "reload commits and transcripts"},
			{"ctrl+c", "Quit", "exit agentgit"},
		}, entries...)
	case modeSelect:
		if m.pending != selectActionNone {
			return append([]helpEntry{
				{"y", "Confirm", "rewrite the selected latest commits"},
				{"n/esc", "Cancel", "return to selecting commits"},
				{"ctrl+c", "Quit", "exit agentgit"},
			}, entries...)
		}
		return append([]helpEntry{
			{"up/down", "Move cursor", "select a commit"},
			{"space", "Toggle", "include or exclude the selected commit"},
			{"x", "Delete", "remove the selected latest commit range"},
			{"m", "Merge", "squash the selected latest commit range"},
			{"s/left", "Back", "return to commit view"},
			{"r", "Refresh", "reload commits and transcripts"},
			{"ctrl+c", "Quit", "exit agentgit"},
		}, entries...)
	case modeDirectories:
		return append([]helpEntry{
			{"up/down", "Move cursor", "select a directory or file path"},
			{"enter/right", "Toggle/open", "toggle folders or open the selected file path"},
			{"left", "Collapse", "collapse the selected depth to its parent folder"},
			{"tab", "Commits", "switch to commit list"},
			{"v", "Requests", "toggle transcript request drawer"},
			{"r", "Refresh", "reload commits and transcripts"},
			{"ctrl+c", "Quit", "exit agentgit"},
		}, entries...)
	case modeFiles:
		if m.selectedFileIsImage() {
			openDescription := "open the image with the system viewer"
			rightAction := "Open file"
			rightDescription := "show current file information"
			backDescription := "return to directories"
			if !m.worktreeFile {
				rightAction = "Open diff"
				rightDescription = "show the file diff"
				backDescription = "return to commits"
			}
			return append([]helpEntry{
				{"up/down", "Move cursor", "select a changed file"},
				{"enter", "Open image", openDescription},
				{"right", rightAction, rightDescription},
				{"left/backspace", "Back", backDescription},
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
			{"/", "Find", "search within the current diff"},
			{"ctrl+f", "Find", "same as /"},
			{"n/p", "Next/previous hunk", "jump between diff hunks"},
			{"m", "Diff layout", "toggle unified and split diff"},
			{"l", "Line numbers", "toggle old and new file line numbers"},
			{"w", "Wrap lines", "show complete long lines; switches split diff to unified"},
			{"f", "Full file", "show the full file at this revision"},
			{"left/backspace", "Back", "return to file list"},
			{"r", "Refresh", "reload current commit data"},
		}, entries...)
	case modeFullFile:
		fullEntries := []helpEntry{
			{"up/down", "Scroll", "move through file lines"},
			{"pgup/pgdn", "Page", "scroll by one page"},
			{"/", "Find", "search within the current file"},
			{"ctrl+f", "Find", "same as /"},
			{"l", "Line numbers", "toggle file line numbers"},
			{"w", "Wrap lines", "show complete long lines across multiple screen rows"},
			{"r", "Refresh", "reload current commit data"},
		}
		if m.worktreeFile {
			fullEntries = append(fullEntries, helpEntry{"left/backspace", "Back", "return to directory view"})
		} else {
			fullEntries = append(fullEntries,
				helpEntry{"f", "Diff", "return to diff view"},
				helpEntry{"left/backspace", "Back", "return to file list"},
			)
		}
		return append(fullEntries, entries...)
	case modeRequest:
		return append([]helpEntry{
			{"up/down", "Scroll", "move through request text"},
			{"pgup/pgdn", "Page", "scroll by one page"},
			{"w", "Wrap lines", "show complete long request lines"},
			{"left/backspace", "Back", "return to request drawer"},
			{"r", "Refresh", "reload transcripts"},
		}, entries...)
	default:
		return entries
	}
}

func (m *model) move(delta int) {
	if m.requestDrawer && m.mode != modeRequest {
		m.requestIdx = clamp(m.requestIdx+delta, 0, len(m.requests)-1)
		return
	}
	switch m.mode {
	case modeCommits, modeSelect:
		m.commitIdx = clamp(m.commitIdx+delta, 0, len(m.commits)-1)
		m.loadCommitFiles()
		if m.mode == modeSelect {
			m.pending = selectActionNone
		}
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
	headerHeight := 5
	contentHeaderHeight := 2
	return max(1, m.height-headerHeight-contentHeaderHeight)
}

func (m *model) jumpHunk(delta int) {
	if m.mode != modeDiff || len(m.diffLines) == 0 {
		return
	}
	lines := m.renderedDiffLines()
	var hunks []int
	for i, line := range lines {
		if strings.HasPrefix(ansi.Strip(line), "@@") {
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
	if m.requestDrawer && m.mode != modeRequest {
		if len(m.requests) == 0 {
			return nil
		}
		m.requestReturn = m.mode
		m.scroll = 0
		m.mode = modeRequest
		return nil
	}
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
		m.worktreeFile = false
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
		if m.worktreeFile {
			m.loadWorktreeFile()
			if m.err == nil {
				m.scroll = 0
				m.mode = modeFullFile
			}
			return nil
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
	case modeFullFile:
		if m.worktreeFile {
			m.mode = modeDirectories
			m.worktreeFile = false
		} else {
			m.mode = modeFiles
		}
	case modeDiff:
		m.mode = modeFiles
	case modeRequest:
		m.mode = m.requestReturnMode()
		m.requestDrawer = true
	case modeFiles:
		if m.fileReturn == modeDirectories {
			m.mode = modeDirectories
			m.worktreeFile = false
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
		m.mode = modeCommits
		m.pending = selectActionNone
		m.scroll = 0
		return
	}
	m.requestDrawer = false
	switch m.mode {
	case modeCommits:
		m.loadDirectoryEntries()
		if m.err != nil {
			return
		}
		if !(m.mode == modeFiles && m.fileReturn == modeDirectories) {
			m.expanded = map[string]bool{}
		}
		m.expandCurrentDirectoryPath()
		m.mode = modeDirectories
	case modeDirectories:
		m.mode = modeCommits
	default:
		m.mode = modeCommits
	}
	m.scroll = 0
}

func (m *model) refresh() tea.Cmd {
	selectedCommit := ""
	if len(m.commits) > 0 && m.commitIdx >= 0 && m.commitIdx < len(m.commits) {
		selectedCommit = m.commits[m.commitIdx].Hash
	}
	previousHead := currentHeadHash(m.commits)
	selectedDirectory := ""
	visibleDirectories := m.visibleDirectoryEntries()
	if len(visibleDirectories) > 0 && m.dirIdx >= 0 && m.dirIdx < len(visibleDirectories) {
		selectedDirectory = visibleDirectories[m.dirIdx].Path
	}
	selectedFile := ""
	if len(m.files) > 0 && m.fileIdx >= 0 && m.fileIdx < len(m.files) {
		selectedFile = m.files[m.fileIdx]
	}
	selectedRequestID := ""
	if len(m.requests) > 0 && m.requestIdx >= 0 && m.requestIdx < len(m.requests) {
		selectedRequestID = m.requests[m.requestIdx].ID
	}
	directoryContext := m.mode == modeDirectories ||
		((m.mode == modeFiles || m.mode == modeFullFile) && m.fileReturn == modeDirectories)

	commits, err := git.CommitsWithUncommitted(m.root, m.limit)
	if err != nil {
		m.err = err
		return nil
	}
	m.branch = git.Branch(m.root)
	m.head = git.ShortHead(m.root)
	m.commits = commits
	m.fileCache = map[string][]string{}
	m.fileStatusCache = map[string]map[string]string{}
	m.invalidateMutableRenderedCaches(previousHead)
	if !directoryContext {
		m.expanded = map[string]bool{}
	}
	m.dirEntries = nil
	m.diffLines = nil
	m.fullLines = nil
	m.scroll = 0
	m.err = nil
	m.selected = keepExistingSelections(m.selected, commits)
	m.pending = selectActionNone

	m.commitIdx = 0
	for i, commit := range m.commits {
		if commit.Hash == selectedCommit {
			m.commitIdx = i
			break
		}
	}
	m.loadCommitFiles()
	m.requestIdx = 0
	for i, req := range m.requests {
		if req.ID == selectedRequestID {
			m.requestIdx = i
			break
		}
	}
	if directoryContext {
		m.loadDirectoryEntries()
		m.dirIdx = 0
		for i, entry := range m.visibleDirectoryEntries() {
			if entry.Path == selectedDirectory {
				m.dirIdx = i
				break
			}
		}
	}
	if len(m.commits) == 0 && !m.worktreeFile {
		m.files = nil
		m.fileIdx = 0
		m.mode = modeCommits
		return m.startRequestsLoad()
	}
	if m.worktreeFile {
		m.files = []string{selectedFile}
		m.fileIdx = 0
	} else if m.mode != modeCommits && m.mode != modeDirectories && m.mode != modeRequest && m.mode != modeSelect {
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
			return m.startRequestsLoad()
		}
	}
	if m.mode == modeDiff {
		m.loadSelectedDiff()
	} else if m.mode == modeFullFile {
		if m.worktreeFile {
			m.loadWorktreeFile()
		} else {
			m.loadFullFile()
		}
	}
	return m.startRequestsLoad()
}

func (m *model) startRequestsLoad() tea.Cmd {
	if m.requestsCmdCancel != nil {
		m.requestsCmdCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.requestsLoadSeq++
	m.requestsLoading = true
	m.requestsCmdContext = ctx
	m.requestsCmdCancel = cancel
	if m.requestsCache == nil {
		m.requestsCache = transcript.NewCache()
	}
	return m.loadRequestsCmd(ctx, m.requestsLoadSeq)
}

func (m *model) invalidateMutableRenderedCaches(previousCommit string) {
	m.diffCache, m.diffCacheKeys = keepImmutableRenderedCache(m.diffCache, m.diffCacheKeys, previousCommit)
	m.fullCache, m.fullCacheKeys = keepImmutableRenderedCache(m.fullCache, m.fullCacheKeys, previousCommit)
}

func keepImmutableRenderedCache(cache map[string][]string, keys []string, previousCommit string) (map[string][]string, []string) {
	if len(cache) == 0 {
		return cache, keys
	}
	filtered := make(map[string][]string, len(cache))
	filteredKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		hash := cacheKeyCommit(key)
		if hash == "" || hash == git.UncommittedHash || hash == previousCommit {
			continue
		}
		if lines, ok := cache[key]; ok {
			filtered[key] = lines
			filteredKeys = append(filteredKeys, key)
		}
	}
	return filtered, filteredKeys
}

func cacheKeyCommit(key string) string {
	if i := strings.IndexByte(key, '\x00'); i >= 0 {
		return key[:i]
	}
	return key
}

func currentHeadHash(commits []git.Commit) string {
	for _, commit := range commits {
		if commit.Hash != git.UncommittedHash {
			return commit.Hash
		}
	}
	return ""
}

func (m *model) toggleRequestDrawer() {
	if m.mode == modeRequest {
		m.mode = m.requestReturnMode()
		m.requestDrawer = true
		m.scroll = 0
		return
	}
	m.requestDrawer = !m.requestDrawer
	m.scroll = 0
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
	if commit.Hash == git.UncommittedHash {
		m.notice = "uncommitted changes cannot be selected"
		return
	}
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
	m.notice = "this will rewrite the latest commits"
}

func (m *model) confirmSelectAction() tea.Cmd {
	if m.mode != modeSelect || m.pending == selectActionNone {
		return nil
	}
	action := m.pending
	selected, err := m.validateSelectAction(action)
	if err != nil {
		m.notice = err.Error()
		m.pending = selectActionNone
		return nil
	}
	base, err := git.Parent(m.root, selected[len(selected)-1].Hash)
	if err != nil {
		m.notice = err.Error()
		m.pending = selectActionNone
		return nil
	}
	switch action {
	case selectActionRemove:
		if err := git.ResetHard(m.root, base); err != nil {
			m.notice = "delete failed: " + err.Error()
			m.pending = selectActionNone
			return nil
		}
		m.selected = map[string]bool{}
		m.pending = selectActionNone
		return m.refreshWithNotice(fmt.Sprintf("deleted %d commits", len(selected)))
	case selectActionSquash:
		newHash, err := git.SquashSince(m.root, base, squashCommitMessage(selected))
		if err != nil {
			m.notice = "merge failed: " + err.Error()
			m.pending = selectActionNone
			return nil
		}
		m.selected = map[string]bool{}
		m.pending = selectActionNone
		return m.refreshWithNotice(fmt.Sprintf("merged %d commits into %s", len(selected), shortHash(newHash)))
	}
	return nil
}

func (m *model) validateSelectAction(action selectAction) ([]git.Commit, error) {
	selected, err := m.selectedLatestRange()
	if err != nil {
		return nil, err
	}
	if action == selectActionSquash && len(selected) < 2 {
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
}

func (m model) selectedLatestRange() ([]git.Commit, error) {
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
		return nil, errors.New("no committed commits to select")
	}
	maxSelected := -1
	for i, commit := range m.commits {
		if !m.selected[commit.Hash] {
			continue
		}
		if commit.Hash == git.UncommittedHash {
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

func (m *model) refreshWithNotice(notice string) tea.Cmd {
	cmd := m.refresh()
	if m.err == nil {
		m.notice = notice
	}
	return cmd
}

func (m model) pendingActionName() string {
	switch m.pending {
	case selectActionRemove:
		return "delete"
	case selectActionSquash:
		return "merge"
	default:
		return ""
	}
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
		if commit.Hash != git.UncommittedHash {
			exists[commit.Hash] = true
		}
	}
	kept := map[string]bool{}
	for hash := range selected {
		if exists[hash] {
			kept[hash] = true
		}
	}
	return kept
}

func (m model) viewRequestFull() string {
	var b strings.Builder
	req, ok := m.selectedRequest()
	if !ok {
		if m.requestsLoading {
			return mutedStyle.Render("Loading agent requests...")
		}
		return mutedStyle.Render("No requests")
	}
	b.WriteString(titleStyle.Render("Request Details"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s  %s", providerStyle.Render(req.Agent), providerStyle.Render(emptyFallback(req.Model, "unknown model"))))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(req.Timestamp))
	if req.SessionID != "" || req.TurnID != "" {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("session " + emptyFallback(req.SessionID, "unknown")))
		if req.TurnID != "" {
			b.WriteString(mutedStyle.Render("  turn " + req.TurnID))
		}
	}
	if req.SourcePath != "" {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render("source " + req.SourcePath))
	}
	b.WriteString("\n\n")
	if len(req.EditedFiles) > 0 {
		b.WriteString(mutedStyle.Render("Edited files"))
		b.WriteString("\n")
		for _, file := range req.EditedFiles {
			b.WriteString("  ")
			b.WriteString(fileStyle.Render(file))
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString(requestStyle.Render(req.Message))

	lines := splitViewLines(b.String())
	if m.wrapLines {
		lines = hardwrapLines(lines, m.frameInnerWidth())
	}
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
		if m.worktreeFile {
			return
		}
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
		line += m.listLineHeight(m.commitListLine(commit), m.frameInnerWidth())
	}
	return 0
}

func (m model) directoryFocusLine() int {
	return m.dirIdx
}

func (m model) requestFocusLine() int {
	line := 0
	for i, req := range m.requests {
		if i == m.requestIdx {
			return line
		}
		line += m.listLineHeight(m.requestListLine(req), m.frameInnerWidth())
	}
	return 0
}

func (m model) fileFocusLine() int {
	return m.fileIdx
}

func (m model) viewCommitsList(width int) string {
	var b strings.Builder
	for i, commit := range m.commits {
		m.renderListLine(&b, m.commitListLine(commit), width, i == m.commitIdx)
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
		if commit.Hash == git.UncommittedHash {
			box = "[-]"
		}
		line := fmt.Sprintf("%s %s %s  %s", markerStyle.Render(box), hashStyle.Render(commit.ShortHash), commit.Date, commit.Subject)
		m.renderListLine(&b, line, width, i == m.commitIdx)
	}
	return b.String()
}

func (m model) viewRequestsList(width int) string {
	if len(m.requests) == 0 {
		if m.requestsLoading {
			return mutedStyle.Render("Loading agent requests...")
		}
		return mutedStyle.Render("no requests")
	}
	var b strings.Builder
	if m.requestsLoading {
		m.renderListLine(&b, mutedStyle.Render("Loading agent requests..."), width, false)
	}
	for i, req := range m.requests {
		m.renderListLine(&b, m.requestListLine(req), width, i == m.requestIdx)
	}
	return b.String()
}

func (m model) requestDrawerHeight(frameHeight int) int {
	if frameHeight <= 0 {
		return 0
	}
	return clamp(frameHeight/3, 5, min(12, frameHeight-3))
}

func (m model) viewRequestDrawer(height int) string {
	width := m.frameInnerWidth()
	innerWidth := max(1, width-2)
	lines := []string{
		mutedStyle.Render(strings.Repeat("─", innerWidth)),
		titleStyle.Render("Requests") + mutedStyle.Render(fmt.Sprintf("  %d  enter details  left close", len(m.requests))),
	}
	listHeight := max(1, height-len(lines))
	if len(m.requests) == 0 {
		lines = append(lines, mutedStyle.Render("no transcript requests for this repo"))
	} else {
		start := 0
		if len(m.requests) > listHeight {
			start = clamp(m.requestIdx-listHeight/2, 0, len(m.requests)-listHeight)
		}
		end := min(len(m.requests), start+listHeight)
		for i := start; i < end; i++ {
			line := m.requestListLine(m.requests[i])
			if i == m.requestIdx {
				line = cursorStyle.Render(line)
			}
			lines = append(lines, truncateVisible(line, innerWidth))
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:height], "\n")
}

func (m model) commitListLine(commit git.Commit) string {
	return fmt.Sprintf("%s %s  %s", hashStyle.Render(commit.ShortHash), commit.Date, commit.Subject)
}

func (m model) requestListLine(req transcript.Request) string {
	var b strings.Builder
	b.WriteString(markerStyle.Render("●"))
	b.WriteString(" ")
	b.WriteString(mutedStyle.Render(formatRequestStartedAt(req.Timestamp)))
	b.WriteString(" ")
	b.WriteString(providerStyle.Render(fmt.Sprintf("[%s %s]", req.Agent, emptyFallback(req.Model, "unknown"))))
	b.WriteString(" ")
	b.WriteString(requestStyle.Render(requestPreviewMessage(req.Message)))
	return b.String()
}

func (m model) renderListLine(b *strings.Builder, line string, width int, selected bool) {
	lines := []string{truncateVisible(line, width)}
	if m.wrapLines {
		lines = hardwrapLine(line, max(1, width))
	}
	for _, part := range lines {
		if selected {
			part = cursorStyle.Render(part)
		}
		b.WriteString(part)
		b.WriteByte('\n')
	}
}

func (m model) listLineHeight(line string, width int) int {
	if !m.wrapLines {
		return 1
	}
	return len(hardwrapLine(line, max(1, width)))
}

func (m model) viewDirectoryList(width int) string {
	entries := m.visibleDirectoryEntries()
	if len(entries) == 0 {
		return mutedStyle.Render("no repository files")
	}
	var b strings.Builder
	for i, entry := range entries {
		name := entry.DisplayName
		nameStyle := fileStyle
		if entry.IsDir {
			name += "/"
			nameStyle = dirStyle
		}
		indent := strings.Repeat("  ", entry.Depth)
		detail := ""
		if entry.IsDir {
			detail = fmt.Sprintf("  %d files", entry.FileCount)
		}
		line := indent + nameStyle.Render(name) + mutedStyle.Render(detail)
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

	return m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), "  ... enter toggles folders or opens files")
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
		b.WriteString(m.renderFileLabel(file, commit.Hash, width, false))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) viewSelectDetailsPreview() string {
	var b strings.Builder
	count := m.selectedCount()
	b.WriteString(titleStyle.Render("Select Mode"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%d commits selected", count))
	if count > 0 {
		if commits, err := m.selectedLatestRange(); err == nil {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  latest %d commits", len(commits))))
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
		b.WriteString(mutedStyle.Render("space selects commits. delete/merge require a contiguous range starting at HEAD."))
		b.WriteString("\n")
	}
	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(markerStyle.Render(m.notice))
		b.WriteString("\n")
	}
	return m.fitPreviewContent(b.String(), m.commitPreviewInnerHeight(), "")
}

func (m model) viewFilesList(width int) string {
	var b strings.Builder
	if len(m.commits) == 0 {
		return ""
	}
	for i, file := range m.files {
		line := m.renderFileLabel(file, m.currentCommitHash(), width, i == m.fileIdx)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) currentCommitHash() string {
	if len(m.commits) == 0 || m.commitIdx < 0 || m.commitIdx >= len(m.commits) {
		return ""
	}
	return m.commits[m.commitIdx].Hash
}

func (m model) renderFileLabel(file string, commitHash string, width int, selected bool) string {
	line := fileStyle.Render(file)
	if status := m.fileStatus(file, commitHash); status != "" {
		line += mutedStyle.Render("  ")
		line += m.fileStatusStyle(status).Render(status)
	}
	if selected {
		line = cursorStyle.Render(line)
	}
	return truncateVisible(line, width)
}

func (m model) fileStatus(file string, commitHash string) string {
	if commitHash == "" || m.fileStatusCache == nil {
		return ""
	}
	return m.fileStatusCache[commitHash][file]
}

func (m model) fileStatusStyle(status string) lipgloss.Style {
	switch status {
	case "created":
		return addLineStyle
	case "deleted":
		return delLineStyle
	default:
		return hunkStyle
	}
}

func (m model) viewFilesBody(height int) string {
	if height <= 0 {
		return ""
	}
	width := m.frameInnerWidth()
	start := 0
	if len(m.files) > height {
		start = clamp(m.fileIdx-height/2, 0, len(m.files)-height)
	}
	end := min(len(m.files), start+height)
	lines := make([]string, 0, height)
	for i := start; i < end; i++ {
		line := m.renderFileLabel(m.files[i], m.currentCommitHash(), width, i == m.fileIdx)
		lines = append(lines, frameLine(line, width))
	}
	for len(lines) < height {
		lines = append(lines, frameLine("", width))
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
	lines := m.renderedDiffLines()
	if m.scroll >= len(lines) {
		m.scroll = max(0, len(lines)-1)
	}
	for _, line := range lines[m.scroll:] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) renderedDiffLines() []string {
	lines := m.visibleDiffLines()
	width := m.frameInnerWidth()
	if m.lineNums && m.diffMode == diffUnified {
		numbered := numberUnifiedDiffLines(lines, width, m.wrapLines)
		for i := range numbered {
			numbered[i] = m.highlightCurrentFileSearchLine(numbered[i], i+1)
		}
		return numbered
	}
	rendered := make([]string, 0, len(lines))
	for i, line := range lines {
		if m.wrapLines {
			renderedLine := styleDiffLine(line, width)
			renderedLine = m.highlightCurrentFileSearchLine(renderedLine, i+1)
			rendered = append(rendered, hardwrapLine(renderedLine, width)...)
		} else {
			renderedLine := renderVisibleDiffLine(line, width, m.diffMode == diffSplit)
			rendered = append(rendered, m.highlightCurrentFileSearchLine(renderedLine, i+1))
		}
	}
	return rendered
}

func (m model) viewFullFile() string {
	var b strings.Builder
	if len(m.files) == 0 || (!m.worktreeFile && len(m.commits) == 0) {
		return ""
	}
	if m.worktreeFile {
		b.WriteString(markerStyle.Render("working tree"))
	} else {
		b.WriteString(hashStyle.Render(m.commits[m.commitIdx].ShortHash))
	}
	b.WriteByte(' ')
	b.WriteString(fileStyle.Render(m.files[m.fileIdx]))
	b.WriteString(" (Full File)\n\n")
	lines := m.renderedFullFileLines()
	if m.scroll >= len(lines) {
		m.scroll = max(0, len(lines)-1)
	}
	for _, line := range lines[m.scroll:] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) renderedFullFileLines() []string {
	width := m.frameInnerWidth()
	numberWidth := len(fmt.Sprint(max(1, len(m.fullLines))))
	rendered := make([]string, 0, len(m.fullLines))
	for i, line := range m.fullLines {
		line = m.highlightCurrentFileSearchLine(line, i+1)
		prefix := ""
		if m.lineNums {
			prefix = mutedStyle.Render(fmt.Sprintf("%*d │ ", numberWidth, i+1))
			if width > 0 {
				prefix = truncateVisible(prefix, width)
			}
		}
		contentWidth := width - lipgloss.Width(prefix)
		if !m.wrapLines {
			if width > 0 {
				line = truncateVisible(line, max(1, contentWidth))
			}
			rendered = append(rendered, prefix+line)
			continue
		}
		contentWidth = max(1, contentWidth)
		parts := hardwrapLine(line, contentWidth)
		for partIndex, part := range parts {
			if partIndex == 0 {
				rendered = append(rendered, prefix+part)
			} else {
				rendered = append(rendered, strings.Repeat(" ", lipgloss.Width(prefix))+part)
			}
		}
	}
	return rendered
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
	m.storeFullCache(key, lines)
	m.fullLines = lines
}

func (m *model) loadWorktreeFile() {
	if len(m.files) == 0 || m.fileIdx < 0 || m.fileIdx >= len(m.files) {
		m.fullLines = nil
		return
	}
	path := m.files[m.fileIdx]
	data, err := os.ReadFile(filepath.Join(m.root, filepath.FromSlash(path)))
	if err != nil {
		m.err = err
		return
	}
	if bytes.Contains(data, []byte{0}) {
		m.fullLines = []string{"Binary file: " + path}
		return
	}
	m.fullLines = Highlight(path, string(data))
}

func (m *model) openSelectedImage() tea.Cmd {
	if len(m.files) == 0 {
		return nil
	}
	if m.worktreeFile {
		path := filepath.Join(m.root, filepath.FromSlash(m.files[m.fileIdx]))
		cmd, err := imageOpenCommand(path)
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
	if len(m.commits) == 0 {
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
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}

func formatRequestStartedAt(startedAt string) string {
	parsed, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return startedAt
	}
	return parsed.Format("01-02 15:04")
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
	if m.fileStatusCache == nil {
		m.fileStatusCache = map[string]map[string]string{}
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
	statuses, err := git.ChangedFileStatuses(m.root, hash)
	if err == nil {
		m.fileStatusCache[hash] = statuses
	}
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
	m.files = []string{entry.Path}
	m.fileIdx = 0
	m.scroll = 0
	m.fileReturn = modeDirectories
	m.worktreeFile = true
	if m.selectedFileIsImage() {
		m.mode = modeFiles
		return
	}
	m.loadWorktreeFile()
	if m.err != nil {
		return
	}
	m.mode = modeFullFile
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

	m.storeDiffCache(key, highlighted)
	m.diffLines = highlighted
}

func (m *model) storeDiffCache(key string, lines []string) {
	if m.diffCache == nil {
		m.diffCache = map[string][]string{}
	}
	m.diffCacheKeys = appendCacheKey(m.diffCacheKeys, key)
	m.diffCache[key] = lines
	m.diffCacheKeys = trimStringSliceCache(m.diffCache, m.diffCacheKeys, renderedFileCacheLimit)
}

func (m *model) storeFullCache(key string, lines []string) {
	if m.fullCache == nil {
		m.fullCache = map[string][]string{}
	}
	m.fullCacheKeys = appendCacheKey(m.fullCacheKeys, key)
	m.fullCache[key] = lines
	m.fullCacheKeys = trimStringSliceCache(m.fullCache, m.fullCacheKeys, renderedFileCacheLimit)
}

func appendCacheKey(keys []string, key string) []string {
	for i, existing := range keys {
		if existing == key {
			copy(keys[i:], keys[i+1:])
			keys[len(keys)-1] = key
			return keys
		}
	}
	return append(keys, key)
}

func trimStringSliceCache(cache map[string][]string, keys []string, limit int) []string {
	if limit <= 0 {
		for key := range cache {
			delete(cache, key)
		}
		return nil
	}
	for len(keys) > limit {
		delete(cache, keys[0])
		copy(keys, keys[1:])
		keys = keys[:len(keys)-1]
	}
	return keys
}

func highlightDiff(filename string, lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	result := append([]string(nil), lines...)
	indexes := make([]int, 0, len(lines))
	prefixes := make([]byte, 0, len(lines))
	codeLines := make([]string, 0, len(lines))
	flush := func() {
		if len(codeLines) == 0 {
			return
		}
		highlighted := Highlight(filename, strings.Join(codeLines, "\n"))
		for i, index := range indexes {
			if i < len(highlighted) {
				result[index] = string(prefixes[i]) + highlighted[i]
			}
		}
		indexes = indexes[:0]
		prefixes = prefixes[:0]
		codeLines = codeLines[:0]
	}
	for i, line := range lines {
		if !isHighlightableDiffLine(line) {
			flush()
			continue
		}
		indexes = append(indexes, i)
		prefixes = append(prefixes, line[0])
		codeLines = append(codeLines, line[1:])
	}
	flush()
	return result
}

func isHighlightableDiffLine(line string) bool {
	if line == "" {
		return false
	}
	if strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, `\`) {
		return false
	}
	return line[0] == '+' || line[0] == '-' || line[0] == ' '
}

func (m model) visibleDiffLines() []string {
	if m.diffMode == diffSplit {
		width := 120
		if m.frameInnerWidth() > 0 {
			width = m.frameInnerWidth()
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

func numberUnifiedDiffLines(lines []string, width int, wrap bool) []string {
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
			rendered := renderVisibleDiffLine(line, width, false)
			if wrap {
				rendered = styleDiffLine(line, width)
				numbered = append(numbered, hardwrapLine(rendered, width)...)
			} else {
				numbered = append(numbered, rendered)
			}
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
		contentWidth := max(1, width-lipgloss.Width(prefix))
		if !wrap {
			numbered = append(numbered, prefix+renderVisibleDiffLine(line, contentWidth, false))
			continue
		}
		wrapped := hardwrapLine(styleDiffLine(line, contentWidth), contentWidth)
		for i, part := range wrapped {
			if i == 0 {
				numbered = append(numbered, prefix+part)
			} else {
				numbered = append(numbered, strings.Repeat(" ", lipgloss.Width(prefix))+part)
			}
		}
	}
	return numbered
}

func hardwrapLines(lines []string, width int) []string {
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, hardwrapLine(line, width)...)
	}
	return wrapped
}

func hardwrapLine(line string, width int) []string {
	if width <= 0 || ansi.StringWidth(line) <= width {
		return []string{line}
	}
	return strings.Split(ansi.Hardwrap(line, width, true), "\n")
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
