package protocol

import (
	"encoding/json"
	"testing"
)

// The four EventMsg variants added by upstream 0.147 (spec 50 D0.6) must
// round-trip through the snake_case tag with their Rust field names.
func TestEventMsg0147VariantsRoundTrip(t *testing.T) {
	usage := &TokenUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7}
	faster := "gpt-fast"
	path, err := NewAgentPath("/root/researcher")
	if err != nil {
		t.Fatalf("agent path: %v", err)
	}
	cases := []struct {
		name string
		msg  EventMsg
		tag  string
		key  string
	}{
		{
			name: "turn_moderation_metadata",
			msg:  EventMsg{Type: EventMsgKindTurnModerationMetadata, TurnModerationMetadata: &TurnModerationMetadataEvent{Metadata: json.RawMessage(`{"flag":true}`)}},
			tag:  "turn_moderation_metadata", key: "metadata",
		},
		{
			name: "safety_buffering",
			msg:  EventMsg{Type: EventMsgKindSafetyBuffering, SafetyBuffering: &SafetyBufferingEvent{Model: "m", UseCases: []string{"a"}, Reasons: []string{"r"}, ShowBufferingUI: true, FasterModel: &faster}},
			tag:  "safety_buffering", key: "show_buffering_ui",
		},
		{
			name: "raw_response_completed",
			msg:  EventMsg{Type: EventMsgKindRawResponseCompleted, RawResponseCompleted: &RawResponseCompletedEvent{ResponseID: "resp_1", TokenUsage: usage}},
			tag:  "raw_response_completed", key: "response_id",
		},
		{
			name: "sub_agent_activity",
			msg:  EventMsg{Type: EventMsgKindSubAgentActivity, SubAgentActivity: &SubAgentActivityEvent{EventID: "e1", OccurredAtMS: 5, AgentThreadID: NewThreadID("t1"), AgentPath: path, Kind: SubAgentActivityInterrupted}},
			tag:  "sub_agent_activity", key: "agent_path",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.msg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var probe map[string]json.RawMessage
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Fatalf("probe: %v", err)
			}
			if string(probe["type"]) != `"`+tc.tag+`"` {
				t.Fatalf("type = %s, want %q", probe["type"], tc.tag)
			}
			if _, ok := probe[tc.key]; !ok {
				t.Fatalf("payload missing %q: %s", tc.key, raw)
			}
			var back EventMsg
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			again, err := json.Marshal(back)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(again) != string(raw) {
				t.Fatalf("round trip drift:\n got %s\nwant %s", again, raw)
			}
		})
	}
	// Sub-agent activity kinds use the snake_case wire spelling.
	if SubAgentActivityInterrupted != "interrupted" || SubAgentActivityStarted != "started" || SubAgentActivityInteracted != "interacted" {
		t.Fatalf("SubAgentActivityKind wire values drifted")
	}
}
