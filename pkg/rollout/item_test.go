package rollout

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// jsonEqual reports whether two JSON documents are semantically equal (after
// key-order canonicalization).
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal a: %v (%s)", err, a)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal b: %v (%s)", err, b)
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return string(ab) == string(bb)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestRolloutItemRoundTrip(t *testing.T) {
	threadID := protocol.NewThreadID("5973b6c0-94b8-487b-a530-2aeb6098ae0e")
	provider := "openai"
	base := BaseInstructions{Text: "be helpful"}

	tests := []struct {
		name string
		json string
	}{
		{
			name: "session_meta",
			json: `{"type":"session_meta","payload":{"id":"5973b6c0-94b8-487b-a530-2aeb6098ae0e","timestamp":"2025-05-07T17:24:21.000Z","cwd":"/work","originator":"codex","cli_version":"1.0.0","source":"cli","model_provider":"openai","base_instructions":{"text":"be helpful"}}}`,
		},
		{
			name: "response_item_message",
			json: `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`,
		},
		{
			name: "compacted",
			json: `{"type":"compacted","payload":{"message":"summary","replacement_history":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"kept"}]}]}}`,
		},
		{
			name: "turn_context",
			json: `{"type":"turn_context","payload":{"cwd":"/work","approval_policy":"on-request","model":"gpt-5","extra_unknown_field":42}}`,
		},
		{
			name: "event_msg_agent_message",
			json: `{"type":"event_msg","payload":{"type":"agent_message","message":"hello","phase":null,"memory_citation":null}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var item RolloutItem
			if err := json.Unmarshal([]byte(tc.json), &item); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			out := mustMarshal(t, item)
			if !jsonEqual(t, []byte(tc.json), out) {
				t.Fatalf("round-trip mismatch:\n want %s\n got  %s", tc.json, out)
			}
		})
	}

	// Avoid unused-variable warnings for shared fixtures referenced only when
	// constructing items programmatically below.
	_ = threadID
	_ = provider
	_ = base
}

func TestRolloutLineRoundTripAndOrder(t *testing.T) {
	line := `{"timestamp":"2025-05-07T17:24:21.000Z","type":"event_msg","payload":{"type":"agent_message","message":"hi","phase":null,"memory_citation":null}}`
	var rl RolloutLine
	if err := json.Unmarshal([]byte(line), &rl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rl.Timestamp != "2025-05-07T17:24:21.000Z" {
		t.Fatalf("timestamp mismatch: %q", rl.Timestamp)
	}
	if rl.Item.Kind != RolloutItemKindEventMsg {
		t.Fatalf("kind mismatch: %q", rl.Item.Kind)
	}
	out := mustMarshal(t, rl)
	if !jsonEqual(t, []byte(line), out) {
		t.Fatalf("round-trip mismatch:\n want %s\n got  %s", line, out)
	}
	// Timestamp must serialize first.
	if string(out[:13]) != `{"timestamp":` {
		t.Fatalf("timestamp not first: %s", out)
	}
}

func TestSessionMetaAlwaysEmitsProviderAndBaseInstructions(t *testing.T) {
	meta := SessionMeta{
		ID:         protocol.NewThreadID("5973b6c0-94b8-487b-a530-2aeb6098ae0e"),
		Timestamp:  "2025-05-07T17:24:21.000Z",
		Cwd:        "/work",
		Originator: "codex",
		CliVersion: "1.0.0",
		Source:     NewCliSource(),
		// ModelProvider and BaseInstructions intentionally nil.
	}
	out := mustMarshal(t, meta)
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := decoded["model_provider"]; !ok || string(v) != "null" {
		t.Fatalf("model_provider should be present and null, got %v (present=%v)", string(v), ok)
	}
	if v, ok := decoded["base_instructions"]; !ok || string(v) != "null" {
		t.Fatalf("base_instructions should be present and null, got %v (present=%v)", string(v), ok)
	}
	// Optional fields should be omitted.
	if _, ok := decoded["forked_from_id"]; ok {
		t.Fatalf("forked_from_id should be omitted when nil")
	}
	if _, ok := decoded["memory_mode"]; ok {
		t.Fatalf("memory_mode should be omitted when nil")
	}
}

func TestSessionMetaSourceDefaultsToVSCode(t *testing.T) {
	in := `{"id":"5973b6c0-94b8-487b-a530-2aeb6098ae0e","timestamp":"t","cwd":".","originator":"o","cli_version":"v"}`
	var meta SessionMeta
	if err := json.Unmarshal([]byte(in), &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if meta.Source.Kind != SessionSourceKindVSCode {
		t.Fatalf("source should default to vscode, got %q", meta.Source.Kind)
	}
}

func TestSessionMetaAgentTypeAlias(t *testing.T) {
	in := `{"id":"5973b6c0-94b8-487b-a530-2aeb6098ae0e","timestamp":"t","cwd":".","originator":"o","cli_version":"v","agent_type":"reviewer"}`
	var meta SessionMeta
	if err := json.Unmarshal([]byte(in), &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if meta.AgentRole == nil || *meta.AgentRole != "reviewer" {
		t.Fatalf("agent_type alias should populate agent_role, got %v", meta.AgentRole)
	}
}
