package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// mkContent builds a text content block, optionally audience-annotated.
func mkContent(text string, audience ...string) json.RawMessage {
	item := map[string]any{"type": "text", "text": text}
	if len(audience) > 0 {
		item["annotations"] = map[string]any{"audience": audience}
	}
	b, _ := json.Marshal(item)
	return b
}

// TestMcpResultTextAudienceFilter verifies the model receives everything EXCEPT
// content addressed only to the user (audience=["user"]). This is what keeps the
// model from re-wording/duplicating a user-facing report block.
func TestMcpResultTextAudienceFilter(t *testing.T) {
	result := protocol.CallToolResult{Content: []json.RawMessage{
		mkContent("FULL USER REPORT", "user"),       // user-only -> skipped
		mkContent("terse digest", "assistant"),      // assistant -> kept
		mkContent("shared note", "user", "assistant"), // both -> kept
		mkContent("legacy untagged"),                 // no audience -> kept (back-compat)
	}}
	got := mcpResultText(result)

	if strings.Contains(got, "FULL USER REPORT") {
		t.Errorf("user-only content leaked to the model: %q", got)
	}
	for _, want := range []string{"terse digest", "shared note", "legacy untagged"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in model text, got %q", want, got)
		}
	}
}

func TestAudienceExcludesModel(t *testing.T) {
	cases := []struct {
		aud  []string
		want bool
	}{
		{nil, false},                       // unannotated -> goes to model
		{[]string{"user"}, true},           // user-only -> excluded
		{[]string{"assistant"}, false},     // assistant -> included
		{[]string{"user", "assistant"}, false},
	}
	for _, c := range cases {
		if got := audienceExcludesModel(c.aud); got != c.want {
			t.Errorf("audienceExcludesModel(%v)=%v want %v", c.aud, got, c.want)
		}
	}
}
