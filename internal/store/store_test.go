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
