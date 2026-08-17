package core

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// captureRecorder records every persisted rollout item in order.
type captureRecorder struct {
	mu    sync.Mutex
	items []rollout.RolloutItem
	fail  bool
}

func (r *captureRecorder) Record(_ context.Context, items []rollout.RolloutItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("recorder down")
	}
	r.items = append(r.items, items...)
	return nil
}

func (r *captureRecorder) Flush(context.Context) error { return nil }

func (r *captureRecorder) kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.items))
	for _, it := range r.items {
		k := string(it.Kind)
		if it.Kind == rollout.RolloutItemKindEventMsg && it.EventMsg != nil {
			k += ":" + string(it.EventMsg.Type)
		}
		if it.Kind == rollout.RolloutItemKindResponseItem && it.ResponseItem != nil {
			k += ":" + string(it.ResponseItem.Type)
		}
		out = append(out, k)
	}
	return out
}

type persistStubClient struct{}

func (persistStubClient) Stream(context.Context, Prompt) (<-chan api.ResponseEvent, error) {
	return nil, errors.New("stub")
}
func (persistStubClient) ContextWindow() *int64 { return nil }
func (persistStubClient) ModelSlug() string     { return "stub" }

func spawnWithRecorder(t *testing.T, rec *captureRecorder, history rollout.InitialHistory) *Codex {
	t.Helper()
	router, err := NewDefaultToolRouter()
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	ok, err := Spawn(context.Background(), CodexSpawnArgs{
		ThreadID:            protocol.NewThreadID("00000000-0000-4000-8000-0000000000aa"),
		Services:            SessionServices{ModelClient: persistStubClient{}, ToolRouter: router, RolloutRecorder: rec},
		ConversationHistory: history,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = ok.Codex.Shutdown(context.Background()) })
	return ok.Codex
}

// TestRecordItemsPersistsPersistableResponseItems: RecordItems persists the
// policy-approved response items (message, function_call, …) and skips the
// others (compaction_trigger); events go through the same policy.
func TestRecordItemsPersistsPersistableResponseItems(t *testing.T) {
	rec := &captureRecorder{}
	codex := spawnWithRecorder(t, rec, rollout.InitialHistory{})
	sess := codex.Session()

	sess.RecordItems([]protocol.ResponseItem{
		{Type: protocol.ResponseItemKindMessage, Role: "user", Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: "hi"}}},
		{Type: protocol.ResponseItemKindCompactionTrigger},
		{Type: protocol.ResponseItemKindFunctionCall, Name: "x", CallID: "c1"},
	})
	sess.SendEvent("s1", protocol.EventMsg{Type: protocol.EventMsgKindAgentMessage, AgentMessage: &protocol.AgentMessageEvent{Message: "m"}})
	sess.SendEvent("s1", protocol.EventMsg{Type: protocol.EventMsgKindAgentMessageContentDelta, AgentMessageContentDelta: &protocol.AgentMessageContentDeltaEvent{Delta: "m"}})
	sess.EmitEvent(protocol.Event{ID: "s1", Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnComplete, TurnComplete: &protocol.TurnCompleteEvent{TurnID: "s1"}}})

	got := rec.kinds()
	want := []string{
		"response_item:message",
		"response_item:function_call",
		"event_msg:agent_message",
		"event_msg:task_complete",
	}
	if len(got) != len(want) {
		t.Fatalf("persisted = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("persisted[%d] = %s, want %s (all: %v)", i, got[i], want[i], got)
		}
	}
	// A failing recorder never breaks the session path.
	rec.fail = true
	sess.RecordItems([]protocol.ResponseItem{{Type: protocol.ResponseItemKindMessage, Role: "user"}})
	sess.SendEvent("s2", protocol.EventMsg{Type: protocol.EventMsgKindAgentMessage, AgentMessage: &protocol.AgentMessageEvent{Message: "m2"}})
	if len(sess.HistoryItems()) != 4 {
		t.Fatalf("history after failing recorder = %d items", len(sess.HistoryItems()))
	}
}

// TestSeededHistoryIsNotRePersisted: resumed history is already durable, so
// seeding it into the session must not write it again.
func TestSeededHistoryIsNotRePersisted(t *testing.T) {
	rec := &captureRecorder{}
	history := rollout.InitialHistory{
		Kind: rollout.InitialHistoryKindResumed,
		Resumed: &rollout.ResumedHistory{
			ConversationID: protocol.NewThreadID("00000000-0000-4000-8000-0000000000aa"),
			History: []rollout.RolloutItem{
				rollout.NewResponseItem(protocol.ResponseItem{Type: protocol.ResponseItemKindMessage, Role: "user", Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: "old"}}}),
			},
		},
	}
	codex := spawnWithRecorder(t, rec, history)
	if n := len(codex.Session().HistoryItems()); n != 1 {
		t.Fatalf("seeded history = %d items, want 1", n)
	}
	for _, k := range rec.kinds() {
		if k == "response_item:message" {
			t.Fatalf("resumed history was re-persisted: %v", rec.kinds())
		}
	}
}
