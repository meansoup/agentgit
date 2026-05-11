package log

import (
	"encoding/json"
	"testing"
)

func TestUnmarshalCodexLog(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantType string
		wantCWD  string
		wantText string
	}{
		{
			name:     "session_meta",
			line:     `{"type":"session_meta","payload":{"cwd":"/home/minkuk/git/agentgit"}}`,
			wantType: "session_meta",
			wantCWD:  "/home/minkuk/git/agentgit",
		},
		{
			name:     "event_msg",
			line:     `{"type":"event_msg","payload":{"type":"user_message","message":"hello world"}}`,
			wantType: "event_msg",
			wantText: "hello world",
		},
		{
			name:     "response_item",
			line:     `{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"fix bug"}]}}`,
			wantType: "response_item",
			wantText: "fix bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry CodexLog
			if err := json.Unmarshal([]byte(tt.line), &entry); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if entry.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", entry.Type, tt.wantType)
			}

			// Based on the new implementation we will write
			var rawText string
			var currentCWD string

			switch entry.Type {
			case "session_meta":
				var p CodexSessionMetaPayload
				if err := json.Unmarshal(entry.Payload, &p); err == nil {
					currentCWD = p.CWD
				}
			case "event_msg":
				var p CodexEventPayload
				if err := json.Unmarshal(entry.Payload, &p); err == nil {
					if p.Type == "user_message" {
						rawText = p.Message
					}
				}
			case "response_item":
				var p CodexResponsePayload
				if err := json.Unmarshal(entry.Payload, &p); err == nil {
					if p.Role == "user" {
						rawText = extractCodexText(p.Content)
					}
				}
			}

			if tt.wantCWD != "" && currentCWD != tt.wantCWD {
				t.Errorf("CWD = %v, want %v", currentCWD, tt.wantCWD)
			}
			if tt.wantText != "" && rawText != tt.wantText {
				t.Errorf("Text = %v, want %v", rawText, tt.wantText)
			}
		})
	}
}
