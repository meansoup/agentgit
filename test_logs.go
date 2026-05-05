package main

import (
	"fmt"
	"github.com/minkuik/agentgit/internal/log"
	"github.com/minkuik/agentgit/internal/git"
)

func DebugLogs() {
	gitRoot, _ := git.FindRoot()
	fmt.Printf("Git Root: %s\n", gitRoot)

	claude, err := log.LoadClaudeRequests(gitRoot)
	if err != nil {
		fmt.Printf("Claude error: %v\n", err)
	}
	fmt.Printf("Claude Requests: %d\n", len(claude))
	for i, r := range claude {
		if i < 3 {
			fmt.Printf("  - [%s] %s\n", r.Timestamp, r.Text)
		}
	}

	gemini, err := log.LoadGeminiRequests(gitRoot)
	if err != nil {
		fmt.Printf("Gemini error: %v\n", err)
	}
	fmt.Printf("Gemini Requests: %d\n", len(gemini))
	for i, r := range gemini {
		if i < 3 {
			fmt.Printf("  - [%s] %s\n", r.Timestamp, r.Text)
		}
	}

	codex, err := log.LoadCodexRequests(gitRoot)
	if err != nil {
		fmt.Printf("Codex error: %v\n", err)
	}
	fmt.Printf("Codex Requests: %d\n", len(codex))
	for i, r := range codex {
		if i < 3 {
			fmt.Printf("  - [%s] %s\n", r.Timestamp, r.Text)
		}
	}
}
