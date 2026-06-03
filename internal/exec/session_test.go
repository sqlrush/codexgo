package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverclient"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// fakeClient is a scripted [client] used to drive the session loop in isolation
// from the engine. It serves canned responses by method and replays a fixed
// sequence of codex/event notifications.
type fakeClient struct {
	threadID string
	events   []protocol.Event
	cursor   int
}

func (f *fakeClient) RequestTyped(_ context.Context, method string, _ any, out any) error {
	switch method {
	case "initialize":
		return nil
	case "thread/start":
		resp := out.(*appserverproto.ThreadStartResponse)
		resp.Thread = json.RawMessage(`{"id":"` + f.threadID + `"}`)
		return nil
	case "thread/resume":
		resp := out.(*appserverproto.ThreadResumeResponse)
		resp.Thread = json.RawMessage(`{"id":"` + f.threadID + `"}`)
		return nil
	case "turn/start":
		return nil
	default:
		return nil
	}
}

func (f *fakeClient) NextEvent(_ context.Context) (appserverclient.ServerEvent, bool) {
	if f.cursor >= len(f.events) {
		return appserverclient.ServerEvent{}, false
	}
	ev := f.events[f.cursor]
	f.cursor++
	raw, _ := json.Marshal(ev)
	return appserverclient.ServerEvent{
		Notification: &appserverproto.JSONRPCNotification{Method: "codex/event", Params: raw},
	}, true
}

func (f *fakeClient) Shutdown(context.Context) {}

// captureSink records emitted events and the final-message handoff for tests.
type captureSink struct {
	events    []ThreadEvent
	warnings  []string
	final     *string
	emitFinal bool
}

func (s *captureSink) Emit(ev ThreadEvent) { s.events = append(s.events, ev) }
func (s *captureSink) Warn(message string) { s.warnings = append(s.warnings, message) }
func (s *captureSink) Finish(final *string, emit bool) {
	s.final = final
	s.emitFinal = emit
}

// TestSessionLoopJSONLStream drives the session against a scripted fake client
// and asserts the full ordered JSONL stream for a successful turn.
func TestSessionLoopJSONLStream(t *testing.T) {
	final := "all done"
	events := []protocol.Event{
		{ID: "s", Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnStarted, TurnStarted: &protocol.TurnStartedEvent{TurnID: "t"}}},
		{ID: "s", Msg: protocol.EventMsg{
			Type: protocol.EventMsgKindItemCompleted,
			ItemCompleted: &protocol.ItemCompletedEvent{Item: protocol.TurnItem{
				Type: protocol.TurnItemKindAgentMessage,
				AgentMessage: &protocol.AgentMessageItem{
					ID:      "m1",
					Content: []protocol.AgentMessageContent{protocol.NewAgentMessageText(final)},
				},
			}},
		}},
		{ID: "s", Msg: protocol.EventMsg{
			Type:         protocol.EventMsgKindTurnComplete,
			TurnComplete: &protocol.TurnCompleteEvent{TurnID: "t", LastAgentMessage: &final},
		}},
	}
	fc := &fakeClient{threadID: "thread-xyz", events: events}
	sink := &captureSink{}
	session := NewSession(fc, sink)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcome, err := session.Run(ctx, SessionConfig{
		Input: []appserverproto.UserInput{{Kind: appserverproto.UserInputKindText, Text: "go"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.ErrorSeen {
		t.Fatal("did not expect an error outcome")
	}

	wantKinds := []ThreadEventKind{
		ThreadEventKindThreadStarted,
		ThreadEventKindTurnStarted,
		ThreadEventKindItemCompleted,
		ThreadEventKindTurnCompleted,
	}
	if got := kindsOf(sink.events); !equalKinds(got, wantKinds) {
		t.Fatalf("event kinds:\n got %v\nwant %v", got, wantKinds)
	}
	if sink.events[0].ThreadStarted.ThreadID != "thread-xyz" {
		t.Fatalf("thread id: got %q", sink.events[0].ThreadStarted.ThreadID)
	}
	if !sink.emitFinal || sink.final == nil || *sink.final != final {
		t.Fatalf("final message handoff wrong: emit=%v final=%v", sink.emitFinal, sink.final)
	}
}

// TestSessionResume verifies a session with a resume thread id emits the
// thread.started event from the resume response and completes the turn.
func TestSessionResume(t *testing.T) {
	final := "resumed"
	events := []protocol.Event{
		{ID: "s", Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnStarted, TurnStarted: &protocol.TurnStartedEvent{TurnID: "t"}}},
		{ID: "s", Msg: protocol.EventMsg{
			Type:         protocol.EventMsgKindTurnComplete,
			TurnComplete: &protocol.TurnCompleteEvent{TurnID: "t", LastAgentMessage: &final},
		}},
	}
	fc := &fakeClient{threadID: "resumed-thread", events: events}
	sink := &captureSink{}
	session := NewSession(fc, sink)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := session.Run(ctx, SessionConfig{ResumeThreadID: "resumed-thread"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sink.events[0].Kind != ThreadEventKindThreadStarted ||
		sink.events[0].ThreadStarted.ThreadID != "resumed-thread" {
		t.Fatalf("expected thread.started for resumed-thread, got %+v", sink.events[0])
	}
}

// TestSessionLoopErrorTurn verifies a fatal error followed by a turn abort yields
// an error outcome and the error / turn.failed events.
func TestSessionLoopErrorTurn(t *testing.T) {
	events := []protocol.Event{
		{ID: "s", Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnStarted, TurnStarted: &protocol.TurnStartedEvent{TurnID: "t"}}},
		{ID: "s", Msg: protocol.EventMsg{Type: protocol.EventMsgKindError, Error: &protocol.ErrorEvent{Message: "stream died"}}},
		{ID: "s", Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnAborted, TurnAborted: &protocol.TurnAbortedEvent{Reason: protocol.TurnAbortReasonReplaced}}},
	}
	fc := &fakeClient{threadID: "th", events: events}
	sink := &captureSink{}
	session := NewSession(fc, sink)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcome, err := session.Run(ctx, SessionConfig{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.ErrorSeen {
		t.Fatal("expected an error outcome")
	}
	kinds := kindsOf(sink.events)
	wantTail := []ThreadEventKind{ThreadEventKindError, ThreadEventKindTurnFailed}
	if len(kinds) < 2 || !equalKinds(kinds[len(kinds)-2:], wantTail) {
		t.Fatalf("expected trailing error + turn.failed, got %v", kinds)
	}
	if sink.emitFinal {
		t.Fatal("failed turn should not emit a final message")
	}
}

// TestRunEndToEndJSON wires the real in-process client + a mock model and asserts
// the JSONL bytes Run produces for a complete turn.
func TestRunEndToEndJSON(t *testing.T) {
	asm := mockAssembly(t, "Hi there")
	var stdout, stderr bytes.Buffer
	env := Environment{
		Stdin:           strings.NewReader(""),
		Stdout:          &stdout,
		Stderr:          &stderr,
		StdinIsTerminal: true,
		Assembly:        asm,
		Defaults:        appserver.Defaults{Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "exec-test"},
	}
	cli := CLI{Subcommand: SubcommandRun, JSON: true, Prompt: strPtr("hello")}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := Run(ctx, cli, env)
	if code != 0 {
		t.Fatalf("exit code: got %d want 0; stderr=%s", code, stderr.String())
	}

	lines := nonEmptyLines(stdout.String())
	var kinds []ThreadEventKind
	var agentText string
	for _, line := range lines {
		var ev ThreadEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("decode JSONL line %q: %v", line, err)
		}
		kinds = append(kinds, ev.Kind)
		if ev.Kind == ThreadEventKindItemCompleted && ev.ItemCompleted.Item.Details.Kind == ThreadItemDetailKindAgentMessage {
			agentText = ev.ItemCompleted.Item.Details.AgentMessage.Text
		}
	}

	if !containsKind(kinds, ThreadEventKindThreadStarted) ||
		!containsKind(kinds, ThreadEventKindTurnStarted) ||
		!containsKind(kinds, ThreadEventKindTurnCompleted) {
		t.Fatalf("missing lifecycle events: %v", kinds)
	}
	if agentText != "Hi there" {
		t.Fatalf("agent message: got %q want %q", agentText, "Hi there")
	}
	if kinds[0] != ThreadEventKindThreadStarted {
		t.Fatalf("first event must be thread.started, got %v", kinds[0])
	}
	if kinds[len(kinds)-1] != ThreadEventKindTurnCompleted {
		t.Fatalf("last event must be turn.completed, got %v", kinds[len(kinds)-1])
	}
}

// TestRunEndToEndText verifies the human mode prints the final message to stdout.
func TestRunEndToEndText(t *testing.T) {
	asm := mockAssembly(t, "Final answer")
	var stdout, stderr bytes.Buffer
	env := Environment{
		Stdin:           strings.NewReader(""),
		Stdout:          &stdout,
		Stderr:          &stderr,
		StdinIsTerminal: true,
		Assembly:        asm,
		Defaults:        appserver.Defaults{Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "exec-test"},
	}
	cli := CLI{Subcommand: SubcommandRun, Prompt: strPtr("hello")}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if code := Run(ctx, cli, env); code != 0 {
		t.Fatalf("exit code %d; stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "Final answer" {
		t.Fatalf("stdout: got %q want %q", got, "Final answer")
	}
}

// mockAssembly builds an appserver assembly whose model client replays a single
// completed turn streaming the given text.
func mockAssembly(t *testing.T, text string) *appserver.Assembly {
	t.Helper()
	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
			slug := cfg.Model()
			if slug == "" {
				slug = "gpt-test"
			}
			return core.NewMockModelClient(slug, nil, completedTurn(text)), nil
		},
		CodexHome:    "/home/.codex",
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	return asm
}

// completedTurn scripts a one-shot assistant turn emitting a single message.
func completedTurn(text string) core.MockTurn {
	mid := "m1"
	end := true
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
		{Kind: api.ResponseEventCompleted, EndTurn: &end},
	}}
}

func strPtr(s string) *string { return &s }

// nonEmptyLines splits text into trimmed non-empty lines.
func nonEmptyLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// containsKind reports whether kinds includes target.
func containsKind(kinds []ThreadEventKind, target ThreadEventKind) bool {
	for _, k := range kinds {
		if k == target {
			return true
		}
	}
	return false
}
