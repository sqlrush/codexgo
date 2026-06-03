package analytics

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// fakeUploader records the batches it receives.
type fakeUploader struct {
	mu      sync.Mutex
	batches [][]TrackEventRequest
}

func (f *fakeUploader) SendTrackEvents(_ context.Context, events []TrackEventRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cloned := make([]TrackEventRequest, len(events))
	copy(cloned, events)
	f.batches = append(f.batches, cloned)
	return nil
}

func (f *fakeUploader) all() []TrackEventRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []TrackEventRequest
	for _, b := range f.batches {
		out = append(out, b...)
	}
	return out
}

func TestDisabledByDefaultDropsEvents(t *testing.T) {
	t.Parallel()
	enabledFalse := false
	up := &fakeUploader{}
	client, shutdown := NewAnalyticsEventsClient(context.Background(), &enabledFalse, up)
	defer shutdown()

	if client.Enabled() {
		t.Fatal("client should be disabled when analyticsEnabled is false")
	}
	client.TrackHookRun(BuildTrackEventsContext("m", "thread", "turn"), HookRunFact{
		EventName:  HookEventNamePreToolUse,
		HookSource: HookSourceUser,
		Status:     HookRunStatusCompleted,
	})
	shutdown()
	if len(up.all()) != 0 {
		t.Fatalf("disabled client should emit nothing, got %d events", len(up.all()))
	}
}

func TestDisabledClientHelper(t *testing.T) {
	t.Parallel()
	if DisabledClient().Enabled() {
		t.Fatal("DisabledClient should be disabled")
	}
}

func TestEnabledWhenNilOptIn(t *testing.T) {
	t.Parallel()
	up := &fakeUploader{}
	client, shutdown := NewAnalyticsEventsClient(context.Background(), nil, up)
	if !client.Enabled() {
		t.Fatal("nil analyticsEnabled should enable the client")
	}

	tracking := BuildTrackEventsContext("gpt-5", "thread-1", "turn-1")
	client.TrackTurnTokenUsage(TurnTokenUsageFact{
		TurnID:   "turn-1",
		ThreadID: "thread-1",
		TokenUsage: protocol.TokenUsage{
			InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
		},
	})
	client.TrackHookRun(tracking, HookRunFact{
		EventName:  HookEventNameStop,
		HookSource: HookSourceProject,
		Status:     HookRunStatusRunning, // normalized to failed
	})
	shutdown()

	events := up.all()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// The token usage event serializes with the expected event_type.
	var foundTokenUsage, foundHook bool
	for _, e := range events {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		switch m["event_type"] {
		case "codex_turn_token_usage":
			foundTokenUsage = true
		case "codex_hook_run":
			foundHook = true
			params := m["event_params"].(map[string]interface{})
			if params["status"] != "failed" {
				t.Errorf("running status should be normalized to failed, got %v", params["status"])
			}
		}
	}
	if !foundTokenUsage || !foundHook {
		t.Fatalf("missing events: tokenUsage=%v hook=%v", foundTokenUsage, foundHook)
	}
}

func TestAppUsedDedup(t *testing.T) {
	t.Parallel()
	up := &fakeUploader{}
	client, shutdown := NewAnalyticsEventsClient(context.Background(), nil, up)

	tracking := BuildTrackEventsContext("m", "thread", "turn")
	connector := "connector-x"
	app := AppInvocation{ConnectorID: &connector}

	client.TrackAppUsed(tracking, app)
	client.TrackAppUsed(tracking, app) // duplicate (same turn+connector): dropped
	shutdown()

	if got := len(up.all()); got != 1 {
		t.Fatalf("expected 1 deduped app_used event, got %d", got)
	}
}

func TestSkillInvocationsEmptyNoop(t *testing.T) {
	t.Parallel()
	up := &fakeUploader{}
	client, shutdown := NewAnalyticsEventsClient(context.Background(), nil, up)
	client.TrackSkillInvocations(BuildTrackEventsContext("m", "t", "u"), nil)
	shutdown()
	if got := len(up.all()); got != 0 {
		t.Fatalf("empty skill invocations should be a no-op, got %d", got)
	}
}
