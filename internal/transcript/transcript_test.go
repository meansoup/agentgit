package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequestsByRepoScansCodexSessionDeterministically(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(filepath.Join(home, ".codex", "sessions", "2026", "07", "29"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "sessions", "2026", "07", "29", "rollout.jsonl")
	writeFile(t, path, `{"type":"session_meta","timestamp":"2026-07-29T01:00:00Z","payload":{"id":"session-a","cwd":"`+repo+`"}}
{"type":"turn_context","timestamp":"2026-07-29T01:00:01Z","payload":{"turn_id":"turn-a","model":"gpt-5","cwd":"`+repo+`"}}
{"type":"event_msg","timestamp":"2026-07-29T01:00:02Z","payload":{"type":"user_message","message":"change tui"}}
{"type":"response_item","timestamp":"2026-07-29T01:00:03Z","payload":{"type":"function_call","name":"apply_patch","arguments":"*** Begin Patch\n*** Update File: internal/tui/tui.go\n@@\n*** End Patch"}}
`)

	got, err := RequestsByRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].Agent != "codex" || got[0].Model != "gpt-5" || got[0].Message != "change tui" || got[0].SessionID != "session-a" || got[0].TurnID != "turn-a" {
		t.Fatalf("request = %+v", got[0])
	}
	if len(got[0].EditedFiles) != 1 || got[0].EditedFiles[0] != "internal/tui/tui.go" {
		t.Fatalf("edited files = %+v", got[0].EditedFiles)
	}
}

func TestRequestsByRepoScansClaudeProjectPath(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "develop", "git", "agentgit")
	dir := filepath.Join(home, ".claude", "projects", escapeClaudeProjectPath(repo))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(dir, "session.jsonl"), `{"type":"user","timestamp":"2026-07-29T02:00:00Z","cwd":"`+repo+`","sessionId":"session-b","uuid":"turn-b","message":{"role":"user","content":[{"type":"text","text":"show requests"}]}}
{"type":"assistant","timestamp":"2026-07-29T02:00:01Z","cwd":"`+repo+`","sessionId":"session-b","message":{"role":"assistant","model":"claude-sonnet","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"`+filepath.Join(repo, "README.md")+`"}}]}}
`)

	got, err := RequestsByRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].Agent != "claude" || got[0].Model != "claude-sonnet" || got[0].Message != "show requests" {
		t.Fatalf("request = %+v", got[0])
	}
	if len(got[0].EditedFiles) != 1 || got[0].EditedFiles[0] != "README.md" {
		t.Fatalf("edited files = %+v", got[0].EditedFiles)
	}
}

func TestRequestsByRepoScansGeminiProjectChat(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	chatDir := filepath.Join(home, ".gemini", "tmp", "repo", "chats")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".gemini", "projects.json"), `{"projects":{"`+repo+`":"repo"}}`)
	writeFile(t, filepath.Join(chatDir, "session.json"), `{
  "sessionId": "session-c",
  "messages": [
    {"type":"user","id":"turn-c","timestamp":"2026-07-29T03:00:00Z","content":[{"text":"add drawer"}]},
    {"type":"gemini","timestamp":"2026-07-29T03:00:01Z","model":"gemini-pro","toolCalls":[{"name":"write_file","args":{"file_path":"internal/transcript/transcript.go"}}]}
  ]
}`)

	got, err := RequestsByRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].Agent != "gemini" || got[0].Model != "gemini-pro" || got[0].Message != "add drawer" {
		t.Fatalf("request = %+v", got[0])
	}
	if len(got[0].EditedFiles) != 1 || got[0].EditedFiles[0] != "internal/transcript/transcript.go" {
		t.Fatalf("edited files = %+v", got[0].EditedFiles)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
