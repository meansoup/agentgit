package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/model"
)

type Diff struct {
	app    *App
	scroll int
}

// NewDiff creates a new Diff screen
func NewDiff(app *App) *Diff {
	return &Diff{app: app, scroll: 0}
}

// View renders the diff screen
func (d *Diff) View(width, height int) string {
	if d.app.changeSets == nil || d.app.selectedIdx >= len(d.app.changeSets) {
		return " Loading..."
	}
	var lines []string

	cs := d.app.changeSets[d.app.selectedIdx]

	// Get the file being shown
	var files []model.ChangedFile
	if cs.Type == "commit" {
		files, _ = git.CommitFiles(cs.CommitHash)
	} else {
		files, _ = git.WorkingTreeFiles()
	}

	if d.app.selectedFile >= len(files) {
		return " No file selected"
	}

	file := files[d.app.selectedFile]

	// Diff header
	header := fmt.Sprintf("Diff | %s | %s | %s", shortSHA(cs.ID), cs.Title, file.Path)
	lines = append(lines, titleStyle.Render(header))
	lines = append(lines, "")

	// Get the diff
	var patch string
	if cs.Type == "commit" {
		diff, _ := git.FileDiff(cs.CommitHash, file.Path)
		patch = diff.Patch
	} else {
		diff, _ := git.WorkingTreeDiff(file.Path)
		patch = diff.Patch
	}

	if patch == "" {
		lines = append(lines, " No diff content")
		return strings.Join(lines, "\n")
	}

	// Render diff with scrolling
	diffLines := strings.Split(patch, "\n")
	startIdx := d.scroll
	if startIdx > len(diffLines) {
		startIdx = len(diffLines)
	}

	contentHeight := height - 3
	if contentHeight < 0 {
		contentHeight = 0
	}
	endIdx := startIdx + contentHeight
	if endIdx > len(diffLines) {
		endIdx = len(diffLines)
	}

	for i := startIdx; i < endIdx; i++ {
		if i < len(diffLines) {
			line := diffLines[i]
			// Color diff lines
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(line)
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(line)
			} else if strings.HasPrefix(line, "@") {
				line = lipgloss.NewStyle().Foreground(lipgloss.Color("33")).Render(line)
			}
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

// ScrollUp scrolls up
func (d *Diff) ScrollUp() {
	if d.scroll > 0 {
		d.scroll--
	}
}

// ScrollDown scrolls down
func (d *Diff) ScrollDown() {
	d.scroll++
}

// PageUp scrolls up by a page
func (d *Diff) PageUp() {
	d.scroll -= 20
	if d.scroll < 0 {
		d.scroll = 0
	}
}

// PageDown scrolls down by a page
func (d *Diff) PageDown() {
	d.scroll += 20
}
