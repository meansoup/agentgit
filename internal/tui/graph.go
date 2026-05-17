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
	if g.app.changeSets == nil {
		return "\n  Loading..."
	}
	var allLines []string
	
	// Track the starting line index of each changeset
	csStartLines := make([]int, len(g.app.changeSets))

	// Generate all potential lines
	for i, cs := range g.app.changeSets {
		csStartLines[i] = len(allLines)
		
		// Render each linked request
		for _, req := range cs.Requests {
			if !g.app.showAllRequests && !req.IsUser {
				continue
			}
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

	// Selection and Scrolling Logic
	selectedTop := csStartLines[g.app.selectedIdx]
	
	// If it's the very first item (idx 0), we must ensure line 0 is visible
	if g.app.selectedIdx == 0 {
		g.scrollOffset = 0
	} else if selectedTop < g.scrollOffset {
		// If selected item's top is above current view
		g.scrollOffset = selectedTop
	} else {
		// If selected item's BOTTOM is below current view
		// Calculate the line index of the changeset itself
		numVisibleRequests := 0
		for _, req := range g.app.changeSets[g.app.selectedIdx].Requests {
			if g.app.showAllRequests || req.IsUser {
				numVisibleRequests++
			}
		}
		selectedBottom := selectedTop + numVisibleRequests
		if selectedBottom >= g.scrollOffset+height {
			g.scrollOffset = selectedBottom - height + 1
		}
	}

	// Ensure scrollOffset is within bounds
	if g.scrollOffset < 0 {
		g.scrollOffset = 0
	}
	if g.scrollOffset > numDisplayLines-height {
		g.scrollOffset = numDisplayLines - height
	}
	if g.scrollOffset < 0 {
		g.scrollOffset = 0
	}

	// Slice the lines to display only the visible portion
	start := g.scrollOffset
	end := g.scrollOffset + height
	if end > numDisplayLines {
		end = numDisplayLines
	}

	if start >= end {
		return ""
	}

	return strings.Join(allLines[start:end], "\n")
}

func (g *Graph) renderRequest(req model.LinkedRequest, isSelected bool, width int) string {
	timeStr := req.Timestamp.Format("01-02 15:04")
	providerLabel := fmt.Sprintf("[%s]", req.Provider)
	text := truncate(req.Text, width-40)

	marker := "○"
	prefix := "  "
	if isSelected {
		marker = selectedStyle.Render(marker)
		prefix = selectedStyle.Render("> ")
	}

	return fmt.Sprintf("%s%s %s  %s %s",
		prefix,
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
		hashOrLabel = "(working tree)"
	}

	text := truncate(cs.Title, width-50)

	prefix := "  └─"
	if isSelected {
		prefix = selectedStyle.Render("> └─")
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
