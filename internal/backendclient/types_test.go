package backendclient

import (
	"encoding/json"
	"testing"
)

func TestCodeTaskDetailsExtraction(t *testing.T) {
	t.Parallel()

	const withDiff = `{
		"current_user_turn": {
			"input_items": [
				{"type": "message", "role": "user", "content": [
					{"content_type": "text", "text": "First line"},
					{"content_type": "text", "text": "Second line"}
				]}
			]
		},
		"current_diff_task_turn": {
			"output_items": [
				{"type": "output_diff", "diff": "diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b\n"}
			]
		},
		"current_assistant_turn": {
			"output_items": [
				{"type": "message", "content": [
					{"content_type": "text", "text": "Assistant response"}
				]}
			]
		}
	}`

	const withError = `{
		"current_assistant_turn": {
			"output_items": [
				{"type": "pr", "output_diff": {"diff": "diff --git a/lib.rs b/lib.rs\n@@ -1 +1 @@\n-a\n+b\n"}}
			],
			"error": {"code": "APPLY_FAILED", "message": "Patch could not be applied"}
		}
	}`

	tests := []struct {
		name        string
		body        string
		wantDiff    string
		wantMsgs    []string
		wantPrompt  string
		wantErr     string
		wantHasDiff bool
		wantHasErr  bool
	}{
		{
			name:        "diff_prefers_diff_turn",
			body:        withDiff,
			wantDiff:    "diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b\n",
			wantMsgs:    []string{"Assistant response"},
			wantPrompt:  "First line\n\nSecond line",
			wantHasDiff: true,
		},
		{
			name:        "diff_from_pr_and_error",
			body:        withError,
			wantHasDiff: true,
			wantErr:     "APPLY_FAILED: Patch could not be applied",
			wantHasErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var details CodeTaskDetailsResponse
			if err := json.Unmarshal([]byte(tt.body), &details); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			diff, hasDiff := details.UnifiedDiff()
			if hasDiff != tt.wantHasDiff {
				t.Fatalf("hasDiff = %v, want %v", hasDiff, tt.wantHasDiff)
			}
			if tt.wantDiff != "" && diff != tt.wantDiff {
				t.Errorf("diff = %q, want %q", diff, tt.wantDiff)
			}
			if tt.name == "diff_from_pr_and_error" {
				if !contains(diff, "lib.rs") {
					t.Errorf("diff should mention lib.rs, got %q", diff)
				}
			}
			if tt.wantMsgs != nil {
				msgs := details.AssistantTextMessages()
				if len(msgs) != len(tt.wantMsgs) || (len(msgs) > 0 && msgs[0] != tt.wantMsgs[0]) {
					t.Errorf("messages = %v, want %v", msgs, tt.wantMsgs)
				}
			}
			if tt.wantPrompt != "" {
				prompt, ok := details.UserTextPrompt()
				if !ok || prompt != tt.wantPrompt {
					t.Errorf("prompt = %q (ok=%v), want %q", prompt, ok, tt.wantPrompt)
				}
			}
			if tt.wantHasErr {
				errMsg, ok := details.AssistantErrorMessage()
				if !ok || errMsg != tt.wantErr {
					t.Errorf("errorMessage = %q (ok=%v), want %q", errMsg, ok, tt.wantErr)
				}
			}
		})
	}
}

func TestContentFragmentUntagged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "bare_string", raw: `"hello"`, want: "hello"},
		{name: "structured_text", raw: `{"content_type":"text","text":"hi"}`, want: "hi"},
		{name: "structured_non_text", raw: `{"content_type":"image","text":"x"}`, want: ""},
		{name: "empty_string", raw: `"   "`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var frag ContentFragment
			if err := json.Unmarshal([]byte(tt.raw), &frag); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := frag.text(); got != tt.want {
				t.Errorf("text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
