package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
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

// TestMcpResultTextIncludesAllBlocks verifies the model receives EVERY text
// block — including user-addressed reports. In a model-driven turn the model
// needs the full tool data to summarize it for the user; audience annotations
// only steer the deterministic slash render path, not what the model sees.
func TestMcpResultTextIncludesAllBlocks(t *testing.T) {
	result := protocol.CallToolResult{Content: []json.RawMessage{
		mkContent("FULL USER REPORT", "user"),         // user-only -> now kept
		mkContent("terse digest", "assistant"),        // assistant -> kept
		mkContent("shared note", "user", "assistant"), // both -> kept
		mkContent("legacy untagged"),                  // no audience -> kept
	}}
	got := mcpResultText(result)

	for _, want := range []string{"FULL USER REPORT", "terse digest", "shared note", "legacy untagged"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in model text, got %q", want, got)
		}
	}
}
