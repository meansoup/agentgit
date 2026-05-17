package linker

import (
	"fmt"
	"sort"
	"time"

	"github.com/minkuik/agentgit/internal/git"
	"github.com/minkuik/agentgit/internal/log"
	"github.com/minkuik/agentgit/internal/model"
)

// LinkCommitsOnly creates changesets from commits without linking requests (for fast initial load)
func LinkCommitsOnly(gitRoot string, commitCount int) ([]model.ChangeSet, error) {
	commits, err := git.LoadCommits(commitCount)
	if err != nil {
		return nil, fmt.Errorf("failed to load commits: %w", err)
	}

	var changeSets []model.ChangeSet
	for _, commit := range commits {
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
			Requests:   nil,
		})
	}

	wtFiles, _ := git.WorkingTreeFiles()
	if len(wtFiles) > 0 {
		changeSets = append([]model.ChangeSet{{
			ID:        "working-tree",
			Type:      "uncommitted",
			Title:     "Working Tree Changes",
			Timestamp: time.Now().UTC(),
			FileCount: len(wtFiles),
		}}, changeSets...)
	}

	return changeSets, nil
}

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

	// Sort commits by time (oldest first)
	sort.Slice(commits, func(i, j int) bool {
		return commits[i].Timestamp.Before(commits[j].Timestamp)
	})

	// Sort requests by time (oldest first)
	sort.Slice(allRequests, func(i, j int) bool {
		return allRequests[i].Timestamp.Before(allRequests[j].Timestamp)
	})

	var changeSets []model.ChangeSet
	usedRequestIDs := make(map[string]bool)

	// 1. Link requests to commits
	// A request belongs to a commit if it occurred BEFORE that commit 
	// (but after the previous commit). 
	// We add a small buffer (e.g., 2 minutes) for commits made slightly before the actual request logging.
	for i, commit := range commits {
		var linkedRequests []model.LinkedRequest
		
		var startTime time.Time
		if i > 0 {
			startTime = commits[i-1].Timestamp
		} else {
			startTime = time.Time{}
		}
		
		// Buffer: Requests occurring up to 2 mins AFTER the commit might still be related 
		// if the commit happened and then the request was logged, but usually it's the other way.
		// Let's stick to: requests BEFORE the commit but AFTER previous.
		endTime := commit.Timestamp

		for _, req := range allRequests {
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

	// 2. Handle working tree changes (all remaining requests)
	wtFiles, _ := git.WorkingTreeFiles()
	var wtRequests []model.LinkedRequest
	for _, req := range allRequests {
		if !usedRequestIDs[req.ID] {
			wtRequests = append(wtRequests, req)
			usedRequestIDs[req.ID] = true
		}
	}

	// Even if no files changed, if there are recent requests, show them as uncommitted block
	if len(wtFiles) > 0 || len(wtRequests) > 0 {
		title := "Working Tree Changes"
		if len(wtRequests) > 0 && len(wtFiles) == 0 {
			title = "Recent Requests (Not Committed)"
		}
		
		changeSets = append(changeSets, model.ChangeSet{
			ID:        "working-tree",
			Type:      "uncommitted",
			Title:     title,
			Timestamp: time.Now().UTC(),
			FileCount: len(wtFiles),
			Requests:  wtRequests,
		})
	}

	// 3. Sort changesets by time (newest first)
	// For each changeset, we consider the maximum of its own timestamp and its latest request timestamp
	sort.Slice(changeSets, func(i, j int) bool {
		ti := getEffectiveTime(changeSets[i])
		tj := getEffectiveTime(changeSets[j])
		return ti.After(tj)
	})

	return changeSets, nil
}

func getEffectiveTime(cs model.ChangeSet) time.Time {
	t := cs.Timestamp
	for _, req := range cs.Requests {
		if req.Timestamp.After(t) {
			t = req.Timestamp
		}
	}
	return t
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
