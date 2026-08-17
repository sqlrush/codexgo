package appserverclient

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// completedTurn builds a one-shot scripted assistant turn that streams a single
// message and ends the turn. The turn runner translates this into TurnStarted,
// AgentMessage, and TurnComplete engine events.
func completedTurn(text string) core.MockTurn {
	mid := "m1"
	return core.MockTurn{Events: []api.ResponseEvent{
		{Kind: api.ResponseEventCreated},
		{
			Kind: api.ResponseEventOutputItemDone,
			Item: &protocol.ResponseItem{
				Type:      protocol.ResponseItemKindMessage,
				Role:      "assistant",
				MessageID: &mid,
				Content:   []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
			},
		},
		{Kind: api.ResponseEventCompleted, EndTurn: boolPtr(true)},
	}}
}

func boolPtr(b bool) *bool { return &b }

// newInProcessClient builds an in-process client over an assembly whose model
// client replays the given turns.
func newInProcessClient(t *testing.T, ctx context.Context, turns ...core.MockTurn) *InProcessAppServerClient {
	t.Helper()
	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
			slug := cfg.Model()
			if slug == "" {
				slug = "gpt-test"
			}
			return core.NewMockModelClient(slug, nil, turns...), nil
		},
		CodexHome:    "/home/.codex",
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	return StartInProcess(ctx, InProcessStartArgs{
		Assembly: asm,
		Defaults: appserver.Defaults{Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "codex-go-test"},
	})
}

// collectCodexEvents drains the client's event channel, returning the core event
// kinds it observes until a TurnComplete (or TurnAborted) arrives or ctx ends.
func collectCodexEvents(t *testing.T, ctx context.Context, client *InProcessAppServerClient, until protocol.EventMsgKind) []protocol.EventMsgKind {
	t.Helper()
	var kinds []protocol.EventMsgKind
	for {
		ev, ok := client.NextEvent(ctx)
		if !ok {
			return kinds
		}
		if !ev.IsCodexEvent() {
			continue
		}
		var coreEvent protocol.Event
		if err := json.Unmarshal(ev.Notification.Params, &coreEvent); err != nil {
			t.Fatalf("decode codex event: %v", err)
		}
		kinds = append(kinds, coreEvent.Msg.Type)
		if coreEvent.Msg.Type == until {
			return kinds
		}
	}
}

// hasKind reports whether kinds contains target.
func hasKind(kinds []protocol.EventMsgKind, target protocol.EventMsgKind) bool {
	for _, k := range kinds {
		if k == target {
			return true
		}
	}
	return false
}

func TestInProcessFullTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := newInProcessClient(t, ctx, completedTurn("Hello, world"))
	defer client.Shutdown(context.Background())

	// 1) initialize handshake.
	var initResp appserverproto.InitializeResponse
	err := client.RequestTyped(ctx, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: "test", Version: "1.0"},
	}, &initResp)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initResp.CodexHome != protocol.AbsolutePath("/home/.codex") {
		t.Fatalf("CodexHome = %q", initResp.CodexHome)
	}

	// 2) thread/start spawns a thread.
	var startResp appserverproto.ThreadStartResponse
	if err := client.RequestTyped(ctx, "thread/start", appserverproto.ThreadStartParams{}, &startResp); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := threadIDFromAggregate(t, startResp.Thread)

	// The first codex/event after thread/start is SessionConfigured (re-emitted).
	firstKinds := drainUntil(t, ctx, client, protocol.EventMsgKindSessionConfigured)
	if !hasKind(firstKinds, protocol.EventMsgKindSessionConfigured) {
		t.Fatalf("did not observe session_configured; got %v", firstKinds)
	}

	// 3) turn/start submits the user input, driving the model turn.
	var turnResp appserverproto.TurnStartResponse
	if err := client.RequestTyped(ctx, "turn/start", appserverproto.TurnStartParams{
		ThreadID: threadID,
		Input:    []appserverproto.UserInput{{Kind: appserverproto.UserInputKindText, Text: "hi"}},
	}, &turnResp); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	// 4) Collect the turn's event stream until TurnComplete.
	kinds := collectCodexEvents(t, ctx, client, protocol.EventMsgKindTurnComplete)
	if !hasKind(kinds, protocol.EventMsgKindTurnStarted) {
		t.Fatalf("missing task_started in turn stream: %v", kinds)
	}
	if !hasKind(kinds, protocol.EventMsgKindAgentMessage) {
		t.Fatalf("missing agent_message in turn stream: %v", kinds)
	}
	if !hasKind(kinds, protocol.EventMsgKindTurnComplete) {
		t.Fatalf("missing task_complete in turn stream: %v", kinds)
	}
}

func TestInProcessRequiresInitialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := newInProcessClient(t, ctx)
	defer client.Shutdown(context.Background())

	res, err := client.Request(ctx, "thread/start", appserverproto.ThreadStartParams{})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !res.IsError() {
		t.Fatal("thread/start before initialize should fail")
	}
	if res.Error.Code != appserver.InvalidRequestCode {
		t.Fatalf("want invalid-request, got code %d", res.Error.Code)
	}
}

func TestInProcessTurnInterrupt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A turn that never ends on its own: a single scripted turn that, after the
	// first request, has no follow-up, so it stays running until interrupted.
	hangTurn := core.MockTurn{Events: []api.ResponseEvent{
		{Kind: api.ResponseEventCreated},
		{Kind: api.ResponseEventCompleted, EndTurn: boolPtr(false)},
	}}
	client := newInProcessClient(t, ctx, hangTurn)
	defer client.Shutdown(context.Background())

	if err := client.RequestTyped(ctx, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: "test", Version: "1.0"},
	}, &appserverproto.InitializeResponse{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	var startResp appserverproto.ThreadStartResponse
	if err := client.RequestTyped(ctx, "thread/start", appserverproto.ThreadStartParams{}, &startResp); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	threadID := threadIDFromAggregate(t, startResp.Thread)
	drainUntil(t, ctx, client, protocol.EventMsgKindSessionConfigured)

	// A startup interrupt (empty turn id) is acknowledged immediately.
	var interruptResp appserverproto.TurnInterruptResponse
	if err := client.RequestTyped(ctx, "turn/interrupt", appserverproto.TurnInterruptParams{
		ThreadID: threadID,
		TurnID:   "",
	}, &interruptResp); err != nil {
		t.Fatalf("turn/interrupt (startup): %v", err)
	}
}

// drainUntil collects codex events until the target kind arrives or ctx ends.
func drainUntil(t *testing.T, ctx context.Context, client *InProcessAppServerClient, target protocol.EventMsgKind) []protocol.EventMsgKind {
	t.Helper()
	return collectCodexEvents(t, ctx, client, target)
}

// threadIDFromAggregate extracts the thread id from a thread aggregate payload.
func threadIDFromAggregate(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var agg struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &agg); err != nil {
		t.Fatalf("decode thread aggregate: %v", err)
	}
	if agg.ID == "" {
		t.Fatal("thread aggregate has empty id")
	}
	return agg.ID
}
