package store

import "testing"

func TestFindRequestIsScopedByRepo(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")

	if _, err := CreateRequest("codex", "codex", "gpt", "one", "/repo/one", "session", "turn", "head", nil); err != nil {
		t.Fatal(err)
	}
	secondID, err := CreateRequest("codex", "codex", "gpt", "two", "/repo/two", "session", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}

	req, ok, err := FindRequest("/repo/two", "codex", "session", "turn")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("FindRequest did not find repo-scoped request")
	}
	if req.ID != secondID {
		t.Fatalf("FindRequest returned id %d, want %d", req.ID, secondID)
	}
}

func TestFindSingleActiveRequestRequiresExactlyOneActiveRequest(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")

	firstID, err := CreateRequest("codex", "codex", "gpt", "one", "/repo", "session-1", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateRequest("gemini", "gemini", "gemini", "two", "/repo", "session-2", "turn", "head", nil); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := FindSingleActiveRequest("/repo"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("FindSingleActiveRequest matched despite multiple active requests")
	}

	if err := FinishRequest(firstID); err != nil {
		t.Fatal(err)
	}
	req, ok, err := FindSingleActiveRequest("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("FindSingleActiveRequest did not match the only active request")
	}
	if req.SessionID != "session-2" {
		t.Fatalf("FindSingleActiveRequest session = %q, want session-2", req.SessionID)
	}
}

func TestFindActiveRequestBySession(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")

	if _, err := CreateRequest("codex", "codex", "gpt", "one", "/repo", "session-1", "turn", "head", nil); err != nil {
		t.Fatal(err)
	}
	wantID, err := CreateRequest("gemini", "gemini", "gemini", "two", "/repo", "session-2", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}

	req, ok, err := FindActiveRequestBySession("/repo", "session-2")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("FindActiveRequestBySession did not find active request")
	}
	if req.ID != wantID {
		t.Fatalf("FindActiveRequestBySession id = %d, want %d", req.ID, wantID)
	}
}

func TestMoveCommitLinksMovesAllRequestsToNewCommit(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")

	firstID, err := CreateRequest("codex", "codex", "gpt", "one", "/repo", "session-1", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := CreateRequest("gemini", "gemini", "gemini", "two", "/repo", "session-2", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := LinkCommit(firstID, "old-a", "/repo"); err != nil {
		t.Fatal(err)
	}
	if err := LinkCommit(secondID, "old-b", "/repo"); err != nil {
		t.Fatal(err)
	}

	if err := MoveCommitLinks("/repo", []string{"old-a", "old-b"}, "new"); err != nil {
		t.Fatal(err)
	}

	links, err := RequestsByCommit("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(links["old-a"]) != 0 || len(links["old-b"]) != 0 {
		t.Fatalf("old commit links were retained: %+v", links)
	}
	got := links["new"]
	if len(got) != 2 {
		t.Fatalf("new commit links = %+v, want 2 requests", got)
	}
	gotIDs := map[int64]bool{got[0].RequestID: true, got[1].RequestID: true}
	if !gotIDs[firstID] || !gotIDs[secondID] {
		t.Fatalf("new commit linked request ids = %+v, want %d and %d", gotIDs, firstID, secondID)
	}
}

func TestDeleteCommitLinksRemovesOnlySelectedHashes(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")

	reqID, err := CreateRequest("codex", "codex", "gpt", "one", "/repo", "session", "turn", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"remove", "keep"} {
		if err := LinkCommit(reqID, hash, "/repo"); err != nil {
			t.Fatal(err)
		}
	}

	if err := DeleteCommitLinks("/repo", []string{"remove"}); err != nil {
		t.Fatal(err)
	}

	links, err := RequestsByCommit("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(links["remove"]) != 0 {
		t.Fatalf("removed hash still linked: %+v", links["remove"])
	}
	if len(links["keep"]) != 1 {
		t.Fatalf("kept hash links = %+v, want 1", links["keep"])
	}
}

func TestRequestsByRepoKeepsLatestOrderAndCommitRefs(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")

	oldID, err := CreateRequest("codex", "codex", "gpt-5", "older", "/repo", "session-1", "turn-1", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := FinishRequest(oldID); err != nil {
		t.Fatal(err)
	}
	newID, err := CreateRequest("claude", "claude", "sonnet", "newer", "/repo", "session-2", "turn-2", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := LinkCommit(oldID, "abc111", "/repo"); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"def222", "ghi333"} {
		if err := LinkCommit(newID, hash, "/repo"); err != nil {
			t.Fatal(err)
		}
	}

	got, err := RequestsByRepo("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("RequestsByRepo len = %d, want 2", len(got))
	}
	if got[0].ID != newID || got[0].AgentName != "claude" || got[0].Message != "newer" {
		t.Fatalf("latest request = %+v, want newer claude request", got[0])
	}
	if len(got[0].CommitRefs) != 2 || got[0].CommitRefs[0] != "def222" || got[0].CommitRefs[1] != "ghi333" {
		t.Fatalf("latest commit refs = %+v, want def222 ghi333", got[0].CommitRefs)
	}
	if got[1].ID != oldID || len(got[1].CommitRefs) != 1 || got[1].CommitRefs[0] != "abc111" {
		t.Fatalf("older request = %+v, want one abc111 ref", got[1])
	}
}

func TestRequestsByRepoIncludesRequestsWithoutLinkedCommits(t *testing.T) {
	t.Setenv("AGENTGIT_DB", t.TempDir()+"/agentgit.sqlite3")

	linkedID, err := CreateRequest("codex", "codex", "gpt-5", "linked", "/repo", "session-1", "turn-1", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	unlinkedID, err := CreateRequest("claude", "claude", "sonnet", "unlinked", "/repo", "session-2", "turn-2", "head", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := LinkCommit(linkedID, "abc111", "/repo"); err != nil {
		t.Fatal(err)
	}

	got, err := RequestsByRepo("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("RequestsByRepo len = %d, want 2", len(got))
	}
	if got[0].ID != unlinkedID || got[0].Message != "unlinked" || len(got[0].CommitRefs) != 0 {
		t.Fatalf("RequestsByRepo[0] = %+v, want unlinked request without commit refs", got[0])
	}
	if got[1].ID != linkedID || got[1].Message != "linked" || len(got[1].CommitRefs) != 1 || got[1].CommitRefs[0] != "abc111" {
		t.Fatalf("RequestsByRepo[1] = %+v, want linked request with abc111", got[1])
	}
}
