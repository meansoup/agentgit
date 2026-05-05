package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/model"
)

type Files struct {
	app          *App
	scrollOffset int
}

// NewFiles creates a new Files screen
func NewFiles(app *App) *Files {
	return &Files{app: app}
}

// View renders the files screen with a preview
func (f *Files) View(width, height int) string {
	cs := f.app.changeSets[f.app.selectedIdx]
	var files []model.ChangedFile
	if cs.Type == "commit" {
		files, _ = git.CommitFiles(cs.CommitHash)
	} else {
		files, _ = git.WorkingTreeFiles()
	}

	// Split height: 40% for files list, 60% for preview
	filesHeight := (height * 40) / 100
	previewHeight := height - filesHeight - 1 // -1 for separator

	// 1. Render Files List
	var filesLines []string
	header := fmt.Sprintf("Files | %s | %s", shortSHA(cs.ID), cs.Title)
	filesLines = append(filesLines, titleStyle.Render(truncate(header, width)))
	filesLines = append(filesLines, "")

	if len(files) == 0 {
		filesLines = append(filesLines, " No changed files")
	}

	for i, file := range files {
		isSelected := i == f.app.selectedFile
		line := f.renderFile(file, isSelected)
		filesLines = append(filesLines, line)
		filesLines = append(filesLines, "")
	}

	// Scrolling for files list
	selectedLineIndex := 0
	if f.app.selectedFile >= 0 && f.app.selectedFile < len(files) {
		selectedLineIndex = 2 + (f.app.selectedFile * 2)
	}

	if selectedLineIndex < f.scrollOffset {
		f.scrollOffset = selectedLineIndex
	} else if selectedLineIndex >= f.scrollOffset+filesHeight {
		f.scrollOffset = selectedLineIndex - filesHeight + 1
	}

	// Bounds check
	if f.scrollOffset < 0 {
		f.scrollOffset = 0
	}
	maxOffset := len(filesLines) - filesHeight
	if f.scrollOffset > maxOffset {
		f.scrollOffset = maxOffset
	}
	if f.scrollOffset < 0 {
		f.scrollOffset = 0
	}

	start := f.scrollOffset
	end := start + filesHeight
	if end > len(filesLines) {
		end = len(filesLines)
	}
	filesView := strings.Join(filesLines[start:end], "\n")
	// Fill remaining height with empty lines to keep separator position fixed
	if end-start < filesHeight {
		filesView += strings.Repeat("\n", filesHeight-(end-start))
	}

	// 2. Render Preview
	previewView := ""
	if len(files) > 0 && f.app.selectedFile >= 0 && f.app.selectedFile < len(files) {
		previewView = f.renderPreview(files[f.app.selectedFile], width, previewHeight)
	}

	separator := dimStyle.Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(lipgloss.Left,
		filesView,
		separator,
		previewView,
	)
}

func (f *Files) renderPreview(file model.ChangedFile, width, height int) string {
	cs := f.app.changeSets[f.app.selectedIdx]
	var diff model.FileDiff
	var err error

	if cs.Type == "commit" {
		diff, err = git.FileDiff(cs.CommitHash, file.Path)
	} else {
		diff, err = git.WorkingTreeDiff(file.Path)
	}

	if err != nil {
		return fmt.Sprintf(" Error loading diff: %v", err)
	}

	if diff.IsBinary {
		return " Binary file - no preview available"
	}

	lines := strings.Split(diff.Patch, "\n")
	var previewLines []string
	previewLines = append(previewLines, titleStyle.Render(fmt.Sprintf(" Diff Preview: %s ", file.Path)))

	for i, line := range lines {
		if i >= height-1 {
			break
		}
		
		// Basic diff highlighting
		renderedLine := " " + truncate(line, width-2)
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			renderedLine = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render(renderedLine)
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			renderedLine = lipgloss.NewStyle().Foreground(lipgloss.Color("197")).Render(renderedLine)
		} else if strings.HasPrefix(line, "@@") {
			renderedLine = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Render(renderedLine)
		}

		previewLines = append(previewLines, renderedLine)
	}

	if len(previewLines) == 1 {
		return previewLines[0] + "\n No changes detected"
	}

	return strings.Join(previewLines, "\n")
}

func (f *Files) renderFile(file model.ChangedFile, isSelected bool) string {
	statusIcon := ""
	switch file.Status {
	case "added":
		statusIcon = "A"
	case "modified":
		statusIcon = "M"
	case "deleted":
		statusIcon = "D"
	case "renamed":
		statusIcon = "R"
	case "copied":
		statusIcon = "C"
	default:
		statusIcon = "?"
	}

	changes := ""
	if file.Additions > 0 || file.Deletions > 0 {
		changes = fmt.Sprintf("+%d -%d", file.Additions, file.Deletions)
	}

	marker := "  "
	if isSelected {
		marker = "> "
	}

	path := file.Path
	if isSelected {
		path = selectedStyle.Render(path)
	}

	line := fmt.Sprintf("%s%s\n  [%s] %s", marker, path, statusIcon, dimStyle.Render(changes))

	return line
}
