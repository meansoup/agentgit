package main

import (
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/tui"
)

func main() {
	// Handle target path argument
	if len(os.Args) > 1 {
		targetPath := os.Args[1]
		if err := os.Chdir(targetPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to change directory to %s: %v\n", targetPath, err)
			os.Exit(1)
		}
	}

	// Get the git root
	gitRoot, err := git.FindRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "agentgit must be run from within a git repository or pointing to one\n")
		os.Exit(1)
	}

	// Create the app
	app, err := tui.NewApp(gitRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create the Bubble Tea program
	p := tea.NewProgram(app, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
