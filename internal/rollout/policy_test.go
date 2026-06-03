package rollout

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func eventItem(t *testing.T, kind protocol.EventMsgKind, payload string) RolloutItem {
	t.Helper()
	full := `{"type":"` + string(kind) + `"`
	if payload != "" {
		full += "," + payload
	}
	full += "}"
	var ev protocol.EventMsg
	if err := json.Unmarshal([]byte(full), &ev); err != nil {
		t.Fatalf("unmarshal event %s: %v", kind, err)
	}
	return NewEventMsgItem(ev)
}

func responseItem(t *testing.T, payload string) RolloutItem {
	t.Helper()
	var ri protocol.ResponseItem
	if err := json.Unmarshal([]byte(payload), &ri); err != nil {
		t.Fatalf("unmarshal response item: %v", err)
	}
	return NewResponseItem(ri)
}

func TestIsPersistedRolloutItem(t *testing.T) {
	tests := []struct {
		name        string
		item        RolloutItem
		mode        EventPersistenceMode
		wantPersist bool
	}{
		{
			name:        "session_meta always persisted",
			item:        NewSessionMetaItem(SessionMetaLine{}),
			mode:        EventPersistenceModeLimited,
			wantPersist: true,
		},
		{
			name:        "turn_context always persisted",
			item:        NewTurnContextItem(TurnContextItem{Raw: json.RawMessage(`{"cwd":"."}`), Cwd: "."}),
			mode:        EventPersistenceModeLimited,
			wantPersist: true,
		},
		{
			name:        "compacted always persisted",
			item:        NewCompactedItem(CompactedItem{Message: "x"}),
			mode:        EventPersistenceModeLimited,
			wantPersist: true,
		},
		{
			name:        "message response item persisted",
			item:        responseItem(t, `{"type":"message","role":"assistant","content":[]}`),
			mode:        EventPersistenceModeLimited,
			wantPersist: true,
		},
		{
			name:        "compaction_trigger response item not persisted",
			item:        responseItem(t, `{"type":"compaction_trigger"}`),
			mode:        EventPersistenceModeLimited,
			wantPersist: false,
		},
		{
			name:        "ghost_snapshot (other) response item not persisted",
			item:        responseItem(t, `{"type":"ghost_snapshot"}`),
			mode:        EventPersistenceModeLimited,
			wantPersist: false,
		},
		{
			name:        "agent_message event persisted in limited",
			item:        eventItem(t, protocol.EventMsgKindAgentMessage, `"message":"hi"`),
			mode:        EventPersistenceModeLimited,
			wantPersist: true,
		},
		{
			name:        "error event not persisted in limited",
			item:        eventItem(t, protocol.EventMsgKindError, `"message":"boom"`),
			mode:        EventPersistenceModeLimited,
			wantPersist: false,
		},
		{
			name:        "error event persisted in extended",
			item:        eventItem(t, protocol.EventMsgKindError, `"message":"boom"`),
			mode:        EventPersistenceModeExtended,
			wantPersist: true,
		},
		{
			name:        "exec_command_begin never persisted",
			item:        eventItem(t, protocol.EventMsgKindExecCommandBegin, `"call_id":"c","command":[],"cwd":"/","parsed_cmd":[]`),
			mode:        EventPersistenceModeExtended,
			wantPersist: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPersistedRolloutItem(tc.item, tc.mode); got != tc.wantPersist {
				t.Fatalf("IsPersistedRolloutItem = %v, want %v", got, tc.wantPersist)
			}
		})
	}
}

func TestPersistedRolloutItemsFiltersAndDoesNotMutate(t *testing.T) {
	items := []RolloutItem{
		NewSessionMetaItem(SessionMetaLine{}),
		responseItem(t, `{"type":"compaction_trigger"}`),
		eventItem(t, protocol.EventMsgKindAgentMessage, `"message":"hi"`),
		eventItem(t, protocol.EventMsgKindError, `"message":"boom"`),
	}
	got := PersistedRolloutItems(items, EventPersistenceModeLimited)
	if len(got) != 2 {
		t.Fatalf("expected 2 persisted items, got %d", len(got))
	}
	if got[0].Kind != RolloutItemKindSessionMeta {
		t.Fatalf("first persisted item should be session_meta, got %q", got[0].Kind)
	}
	if got[1].Kind != RolloutItemKindEventMsg || got[1].EventMsg.Type != protocol.EventMsgKindAgentMessage {
		t.Fatalf("second persisted item should be agent_message")
	}
	// Original slice unchanged.
	if len(items) != 4 {
		t.Fatalf("input slice mutated")
	}
}

func TestExtendedModeTruncatesExecAggregatedOutput(t *testing.T) {
	big := strings.Repeat("a", 50_000)
	payload := `"call_id":"c","turn_id":"t","completed_at_ms":0,"command":["echo"],"cwd":"/","parsed_cmd":[],"source":"agent","stdout":"out","stderr":"err","aggregated_output":"` + big + `","exit_code":0,"duration":{"secs":1,"nanos":0},"formatted_output":"fmt","status":"completed"`
	item := eventItem(t, protocol.EventMsgKindExecCommandEnd, payload)

	out := PersistedRolloutItems([]RolloutItem{item}, EventPersistenceModeExtended)
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	end := out[0].EventMsg.ExecCommandEnd
	if end == nil {
		t.Fatalf("expected exec command end payload")
	}
	if len(end.AggregatedOutput) >= len(big) {
		t.Fatalf("aggregated output not truncated: len=%d", len(end.AggregatedOutput))
	}
	if end.Stdout != "" || end.Stderr != "" || end.FormattedOutput != "" {
		t.Fatalf("stdout/stderr/formatted_output should be cleared")
	}

	// The original item must be unchanged (immutability).
	if item.EventMsg.ExecCommandEnd.Stdout != "out" {
		t.Fatalf("original item mutated: stdout=%q", item.EventMsg.ExecCommandEnd.Stdout)
	}
	if len(item.EventMsg.ExecCommandEnd.AggregatedOutput) != len(big) {
		t.Fatalf("original aggregated output mutated")
	}
}

func TestShouldPersistResponseItemForMemories(t *testing.T) {
	developer := responseItem(t, `{"type":"message","role":"developer","content":[]}`)
	user := responseItem(t, `{"type":"message","role":"user","content":[]}`)
	reasoning := responseItem(t, `{"type":"reasoning","summary":[]}`)
	if ShouldPersistResponseItemForMemories(*developer.ResponseItem) {
		t.Fatalf("developer messages should not be persisted for memories")
	}
	if !ShouldPersistResponseItemForMemories(*user.ResponseItem) {
		t.Fatalf("user messages should be persisted for memories")
	}
	if ShouldPersistResponseItemForMemories(*reasoning.ResponseItem) {
		t.Fatalf("reasoning should not be persisted for memories")
	}
}
