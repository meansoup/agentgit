package linker

import (
	"fmt"
	"sort"
	"time"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/log"
	"github.com/minkuik/agentgit/internal/model"
)

// LinkRequestsToChangesets links user requests to git commits and working tree
func LinkRequestsToChangesets(gitRoot string, commitCount int) ([]model.ChangeSet, error) {
	// Load commits
	commits, err := git.LoadCommits(commitCount)
	if err != nil {
		return nil, fmt.Errorf("failed to load commits: %w", err)
	}

	// Load requests from all providers
	claudeRequests, _ := log.LoadClaudeRequests(gitRoot)
	geminiRequests, _ := log.LoadGeminiRequests(gitRoot)
	codexRequests, _ := log.LoadCodexRequests(gitRoot)

	allRequests := append(claudeRequests, geminiRequests...)
	allRequests = append(allRequests, codexRequests...)

	// Sort commits by time (oldest first) for chronological linking
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].Timestamp.Before(commits[j].Timestamp)
	})

	// Sort requests by time (oldest first) for chronological linking
	sort.Slice(allRequests, func(i, j int) bool {
		return allRequests[i].Timestamp.Before(allRequests[j].Timestamp)
	})

	var changeSets []model.ChangeSet
	usedRequestIDs := make(map[string]bool)

	// 1. Link requests to commits chronologically
	for i, commit := range commits {
		var linkedRequests []model.LinkedRequest

		// Determine the time range for this commit:
		// - start: after the previous commit (or from beginning if first commit)
		// - end: before this commit + 10-minute buffer (for clock skew)
		var startTime time.Time
		if i > 0 {
			startTime = commits[i-1].Timestamp
		} else {
			startTime = time.Time{} // epoch, effectively no lower bound
		}
		endTime := commit.Timestamp.Add(10 * time.Minute)

		for _, req := range allRequests {
			// A request belongs to this commit if:
			// - it hasn't been used yet
			// - it's after the previous commit
			// - it's before this commit (+ buffer)
			if !usedRequestIDs[req.ID] &&
				req.Timestamp.After(startTime) &&
				!req.Timestamp.After(endTime) {
				linkedRequests = append(linkedRequests, req)
				usedRequestIDs[req.ID] = true
			}
		}

		additions, deletions, _ := git.GetCommitChanges(commit.Hash)
		fileCount := countFilesInCommit(commit.Hash)

		changeSets = append(changeSets, model.ChangeSet{
			ID:         commit.Hash[:8],
			Type:       "commit",
			Title:      commit.Title,
			CommitHash: commit.Hash,
			Author:     commit.Author,
			Timestamp:  commit.Timestamp,
			Summary:    fmt.Sprintf("%d files: +%d -%d", fileCount, additions, deletions),
			FileCount:  fileCount,
			Requests:   linkedRequests,
		})
	}

	// 2. Handle working tree changes (remaining requests after the latest commit)
	wtFiles, _ := git.WorkingTreeFiles()
	var wtRequests []model.LinkedRequest
	for _, req := range allRequests {
		if !usedRequestIDs[req.ID] {
			wtRequests = append(wtRequests, req)
			usedRequestIDs[req.ID] = true
		}
	}

	if len(wtFiles) > 0 || len(wtRequests) > 0 {
		changeSets = append(changeSets, model.ChangeSet{
			ID:        "working-tree",
			Type:      "uncommitted",
			Title:     "Working Tree Changes",
			Timestamp: time.Now().UTC(),
			FileCount: len(wtFiles),
			Requests:  wtRequests,
		})
	}

	// 3. Reverse for newest-first display
	for i := 0; i < len(changeSets)/2; i++ {
		j := len(changeSets) - 1 - i
		changeSets[i], changeSets[j] = changeSets[j], changeSets[i]
	}

	return changeSets, nil
}

func countFilesInCommit(hash string) int {
	files, err := git.CommitFiles(hash)
	if err != nil {
		return 0
	}
	return len(files)
}

func isLinked(changeSets []model.ChangeSet, commitHash string) bool {
	for _, cs := range changeSets {
		if cs.CommitHash == commitHash {
			return true
		}
	}
	return false
}
