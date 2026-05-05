package tui

import (
	"fmt"
	"strings"

	"github.com/minkuik/agentgit/internal/model"
)

type Graph struct {
	app          *App
	scrollOffset int
}

// NewGraph creates a new Graph screen
func NewGraph(app *App) *Graph {
	return &Graph{app: app}
}

// View renders the graph screen
func (g *Graph) View(width, height int) string {
	var allLines []string

	// Generate all potential lines first to determine total height and selected line position
	for i, cs := range g.app.changeSets {
		// Render each linked request
		for _, req := range cs.Requests {
			reqLine := g.renderRequest(req, i == g.app.selectedIdx, width)
			allLines = append(allLines, reqLine)
		}

		// Render the changeset (commit or working tree)
		line := g.renderChangeSet(cs, i == g.app.selectedIdx, width)
		allLines = append(allLines, line)
	}

	numDisplayLines := len(allLines)
	if numDisplayLines == 0 {
		return ""
	}

	// Calculate the line index of the selected item
	selectedLineIndex := 0
	currentCsLineCount := 0
	for i, cs := range g.app.changeSets {
		// Count lines for requests
		currentCsLineCount += len(cs.Requests)
		// Count line for changeset itself
		currentCsLineCount++

		if i == g.app.selectedIdx {
			selectedLineIndex = currentCsLineCount - 1 // The last line of the selected changeset is what we want visible
			break
		}
	}

	// Adjust scroll offset to keep selected item in view
	if selectedLineIndex < g.scrollOffset {
		g.scrollOffset = selectedLineIndex
	} else if selectedLineIndex >= g.scrollOffset+height {
		g.scrollOffset = selectedLineIndex - height + 1
	}

	// Ensure scrollOffset is within bounds
	if g.scrollOffset < 0 {
		g.scrollOffset = 0
	}
	if g.scrollOffset > numDisplayLines-height {
		g.scrollOffset = numDisplayLines - height
	}
	if g.scrollOffset < 0 { // If height is greater than total lines
		g.scrollOffset = 0
	}

	// Slice the lines to display only the visible portion
	start := g.scrollOffset
	end := g.scrollOffset + height
	if end > numDisplayLines {
		end = numDisplayLines
	}

	if start >= end {
		return "" // Nothing to display
	}

	return strings.Join(allLines[start:end], "\n")
}

func (g *Graph) renderRequest(req model.LinkedRequest, isSelected bool, width int) string {
	timeStr := req.Timestamp.Format("01-02 15:04")
	providerLabel := fmt.Sprintf("[%s]", req.Provider)
	text := truncate(req.Text, width-40)

	marker := "○"
	if isSelected {
		marker = selectedStyle.Render(marker)
	}

	return fmt.Sprintf("%s %s  %s %s",
		marker,
		dimStyle.Render(timeStr),
		requestStyle.Render(providerLabel),
		requestStyle.Render(text),
	)
}

func (g *Graph) renderChangeSet(cs model.ChangeSet, isSelected bool, width int) string {
	marker := "●"
	if cs.Type == "uncommitted" {
		marker = "○"
	}

	var timeStr, hashOrLabel string
	if cs.Type == "commit" {
		timeStr = cs.Timestamp.Format("01-02 15:04")
		hashOrLabel = shortSHA(cs.CommitHash)
	} else {
		hashOrLabel = "(uncommitted)"
	}

	text := truncate(cs.Title, width-50)

	prefix := "└─"
	if isSelected {
		prefix = selectedStyle.Render(prefix)
		marker = selectedStyle.Render(marker)
	}

	if timeStr != "" {
		return fmt.Sprintf("%s %s %s  %s %s",
			prefix,
			marker,
			dimStyle.Render(hashOrLabel),
			dimStyle.Render(timeStr),
			commitStyle.Render(text),
		)
	} else {
		return fmt.Sprintf("%s %s %s",
			prefix,
			marker,
			commitStyle.Render(text),
		)
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) > maxLen {
		if maxLen <= 3 {
			return string(runes[:maxLen])
		}
		return string(runes[:maxLen-3]) + "..."
	}
	return s
}

func getFirstLine(s string) string {
	lines := strings.Split(s, "\n")
	return strings.TrimSpace(lines[0])
}
