package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/linker"
	"github.com/minkuik/agentgit/internal/model"
)

const (
	screenGraph = iota
	screenFiles
	screenDiff
)

type App struct {
	screen          int
	gitRoot         string
	changeSets      []model.ChangeSet
	selectedIdx     int
	selectedFile    int
	showAllRequests bool
	isLoading       bool
	status          string

	// Sub-models
	graph *Graph
	files *Files
	diff  *Diff

	width  int
	height int
}

type (
	initialCommitsMsg []model.ChangeSet
	fullChangeSetsMsg []model.ChangeSet
	errMsg            error
)

// NewApp creates a new TUI app
func NewApp(gitRoot string) (*App, error) {
	if !git.IsGitRepository() {
		return nil, fmt.Errorf("not a git repository")
	}

	app := &App{
		screen:          screenGraph,
		gitRoot:         gitRoot,
		changeSets:      nil, // Start empty
		showAllRequests: true,
		isLoading:       true,
		status:          "Loading commits...",
	}

	app.graph = NewGraph(app)
	app.files = NewFiles(app)
	app.diff = NewDiff(app)

	return app, nil
}

// Init initializes the app
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			cs, err := linker.LinkCommitsOnly(a.gitRoot, 50)
			if err != nil {
				return errMsg(err)
			}
			return initialCommitsMsg(cs)
		},
		func() tea.Msg {
			cs, err := linker.LinkRequestsToChangesets(a.gitRoot, 50)
			if err != nil {
				return errMsg(err)
			}
			return fullChangeSetsMsg(cs)
		},
	)
}

// Update handles input
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case initialCommitsMsg:
		if a.changeSets == nil {
			a.changeSets = msg
		}
		a.status = "Loading requests..."
		return a, nil

	case fullChangeSetsMsg:
		a.changeSets = msg
		a.isLoading = false
		a.status = "Ready"
		return a, nil

	case errMsg:
		a.status = fmt.Sprintf("Error: %v", msg)
		a.isLoading = false
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return a, tea.Quit
		case "r":
			return a.refresh()
		case "u":
			a.showAllRequests = !a.showAllRequests
			return a, nil
		}

		if a.isLoading && a.changeSets == nil {
			return a, nil // Ignore other input while initial load
		}

		switch a.screen {
		case screenGraph:
			return a.handleGraphInput(msg)
		case screenFiles:
			if msg.String() == "esc" || msg.String() == "left" {
				a.screen = screenGraph
				return a, nil
			}
			return a.handleFilesInput(msg)
		case screenDiff:
			if msg.String() == "esc" || msg.String() == "left" {
				a.screen = screenFiles
				return a, nil
			}
			return a.handleDiffInput(msg)
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
	}

	return a, nil
}

// View renders the current screen
func (a *App) View() string {
	if a.width < 20 || a.height < 10 {
		return "Terminal too small"
	}

	var header, content, footer string

	// Header
	screenName := ""
	switch a.screen {
	case screenGraph:
		screenName = "Change Sets"
	case screenFiles:
		screenName = "Files"
	case screenDiff:
		screenName = "Diff"
	}

	headerLeft := titleStyle.Render(" AgentGit ")
	
	filterStatus := " All Logs "
	if !a.showAllRequests {
		filterStatus = " User Only "
	}
	headerCenter := dimStyle.Render(fmt.Sprintf(" | %s | %s | %s ", a.gitRoot, filterStatus, a.status))
	headerRight := titleStyle.Render(fmt.Sprintf(" %s ", screenName))
	
	header = lipgloss.JoinHorizontal(lipgloss.Top, headerLeft, headerCenter, headerRight)
	header = header + "\n" + safeRepeat("─", a.width)

	// Content
	switch a.screen {
	case screenGraph:
		content = a.graph.View(a.width, a.height-4)
	case screenFiles:
		content = a.files.View(a.width, a.height-4)
	case screenDiff:
		content = a.diff.View(a.width, a.height-4)
	}

	// Footer
	footerText := " ↑/↓ move | pgup/pgdn | enter/right open | e edit | esc/left back | r refresh | u toggle filter | q quit "
	if a.screen == screenDiff {
		footerText = " ↑/↓ scroll | pgup/pgdn | e edit | esc/left back | q quit "
	}
	footer = safeRepeat("─", a.width) + "\n" + dimStyle.Render(footerText)

	// Assemble
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

func safeRepeat(s string, count int) string {
	if count <= 0 {
		return ""
	}
	// Cap count to avoid excessive memory usage
	if count > 2000 {
		count = 2000
	}
	return strings.Repeat(s, count)
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (a *App) handleGraphInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if a.selectedIdx > 0 {
			a.selectedIdx--
		}
	case "down":
		if a.selectedIdx < len(a.changeSets)-1 {
			a.selectedIdx++
		}
	case "pgup":
		a.selectedIdx -= 10
		if a.selectedIdx < 0 {
			a.selectedIdx = 0
		}
	case "pgdown":
		a.selectedIdx += 10
		if a.selectedIdx >= len(a.changeSets) {
			a.selectedIdx = len(a.changeSets) - 1
		}
	case "enter", "right":
		if a.selectedIdx < len(a.changeSets) && (a.changeSets[a.selectedIdx].Type == "commit" || a.changeSets[a.selectedIdx].FileCount > 0) {
			a.screen = screenFiles
			a.selectedFile = 0
		}
	}
	return a, nil
}

func (a *App) handleFilesInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cs := a.changeSets[a.selectedIdx]
	var files []model.ChangedFile
	if cs.Type == "commit" {
		files, _ = git.CommitFiles(cs.CommitHash)
	} else {
		files, _ = git.WorkingTreeFiles()
	}

	switch msg.String() {
	case "up":
		if a.selectedFile > 0 {
			a.selectedFile--
		}
	case "down":
		if a.selectedFile < len(files)-1 {
			a.selectedFile++
		}
	case "pgup":
		a.selectedFile -= 5
		if a.selectedFile < 0 {
			a.selectedFile = 0
		}
	case "pgdown":
		a.selectedFile += 5
		if a.selectedFile >= len(files) {
			a.selectedFile = len(files) - 1
		}
	case "e":
		if a.selectedFile < len(files) {
			fullPath := filepath.Join(a.gitRoot, files[a.selectedFile].Path)
			c := exec.Command("vi", fullPath)
			return a, tea.ExecProcess(c, func(err error) tea.Msg {
				return a.refreshCmd()
			})
		}
	case "enter", "right":
		if a.selectedFile < len(files) {
			a.screen = screenDiff
		}
	}
	return a, nil
}

func (a *App) handleDiffInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		a.diff.ScrollUp()
	case "down":
		a.diff.ScrollDown()
	case "pgup":
		a.diff.PageUp()
	case "pgdown":
		a.diff.PageDown()
	case "e":
		cs := a.changeSets[a.selectedIdx]
		var files []model.ChangedFile
		if cs.Type == "commit" {
			files, _ = git.CommitFiles(cs.CommitHash)
		} else {
			files, _ = git.WorkingTreeFiles()
		}
		if a.selectedFile < len(files) {
			fullPath := filepath.Join(a.gitRoot, files[a.selectedFile].Path)
			c := exec.Command("vi", fullPath)
			return a, tea.ExecProcess(c, func(err error) tea.Msg {
				return a.refreshCmd()
			})
		}
	}
	return a, nil
}

func (a *App) refresh() (tea.Model, tea.Cmd) {
	a.isLoading = true
	a.status = "Refreshing..."
	return a, a.refreshCmd()
}

func (a *App) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		cs, err := linker.LinkRequestsToChangesets(a.gitRoot, 50)
		if err != nil {
			return errMsg(err)
		}
		return fullChangeSetsMsg(cs)
	}
}

// Styles
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("33")).Foreground(lipgloss.Color("15"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	requestStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("147")).Italic(true)
	commitStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("142")).Bold(true)
)
