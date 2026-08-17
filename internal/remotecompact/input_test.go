package remotecompact

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

func strptr(s string) *string { return &s }

func userMessage(role, text string) protocol.ResponseItem {
	return protocol.ResponseItem{
		Type: protocol.ResponseItemKindMessage,
		Role: role,
		Content: []protocol.ContentItem{
			{Type: protocol.ContentItemKindInputText, Text: text},
		},
	}
}

func TestCompactionInputMarshalJSON(t *testing.T) {
	effort := protocol.ReasoningEffortLow
	reasoning := &api.Reasoning{Effort: &effort}

	tests := []struct {
		name  string
		input CompactionInput
		// wantKeys is the exact ordered set of top-level keys expected.
		wantKeys []string
		check    func(t *testing.T, m map[string]json.RawMessage)
	}{
		{
			name: "minimal omits optional fields and empty instructions",
			input: CompactionInput{
				Model: "gpt-test",
				Input: []protocol.ResponseItem{userMessage("user", "hi")},
			},
			wantKeys: []string{"model", "input", "tools", "parallel_tool_calls"},
		},
		{
			name: "full shape preserves declaration order",
			input: CompactionInput{
				Model:             "gpt-test",
				Input:             []protocol.ResponseItem{userMessage("user", "hi")},
				Instructions:      "be brief",
				Tools:             []json.RawMessage{json.RawMessage(`{"type":"function"}`)},
				ParallelToolCalls: true,
				Reasoning:         reasoning,
				ServiceTier:       strptr("flex"),
				PromptCacheKey:    strptr("key-123"),
				Text:              &api.TextControls{},
			},
			wantKeys: []string{
				"model", "input", "instructions", "tools", "parallel_tool_calls",
				"reasoning", "service_tier", "prompt_cache_key", "text",
			},
			check: func(t *testing.T, m map[string]json.RawMessage) {
				if got := string(m["instructions"]); got != `"be brief"` {
					t.Errorf("instructions = %s, want %q", got, "be brief")
				}
				if got := string(m["service_tier"]); got != `"flex"` {
					t.Errorf("service_tier = %s", got)
				}
			},
		},
		{
			name: "nil input serializes as empty array",
			input: CompactionInput{
				Model: "gpt-test",
			},
			wantKeys: []string{"model", "input", "tools", "parallel_tool_calls"},
			check: func(t *testing.T, m map[string]json.RawMessage) {
				if got := string(m["input"]); got != "[]" {
					t.Errorf("input = %s, want []", got)
				}
				if got := string(m["tools"]); got != "[]" {
					t.Errorf("tools = %s, want []", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			gotKeys := orderedKeys(t, raw)
			if !equalStrings(gotKeys, tt.wantKeys) {
				t.Fatalf("keys = %v, want %v", gotKeys, tt.wantKeys)
			}
			if tt.check != nil {
				var m map[string]json.RawMessage
				if err := json.Unmarshal(raw, &m); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				tt.check(t, m)
			}
		})
	}
}

// orderedKeys returns the top-level keys of a JSON object in serialization
// order by scanning the raw bytes (json.Unmarshal into a map loses order).
func orderedKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("decode opening token: %v", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		t.Fatalf("expected object, got %v", tok)
	}
	var keys []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("decode key: %v", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			t.Fatalf("expected string key, got %v", keyTok)
		}
		keys = append(keys, key)
		// Skip the value.
		if err := skipValue(dec); err != nil {
			t.Fatalf("skip value: %v", err)
		}
	}
	return keys
}

func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if delim, ok := tok.(json.Delim); ok && (delim == '{' || delim == '[') {
		depth := 1
		for depth > 0 {
			t2, err := dec.Token()
			if err != nil {
				return err
			}
			if d2, ok := t2.(json.Delim); ok {
				switch d2 {
				case '{', '[':
					depth++
				case '}', ']':
					depth--
				}
			}
		}
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
