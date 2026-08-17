package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// ----------------------------------------------------------------------------
// Turn-test doubles
// ----------------------------------------------------------------------------

// fakeToolRouter is a [ToolRouter] test double. SpecsForTurn returns the
// configured specs; Dispatch records each invocation and returns a scripted
// result (or runs a per-call function, e.g. to assert cancellation). It is safe
// for concurrent use.
type fakeToolRouter struct {
	mu sync.Mutex

	specs []tools.ToolSpec

	// dispatch, when set, fully controls each Dispatch call.
	dispatch func(ctx context.Context, tc *TurnContext, inv ToolInvocation) (ToolResult, error)

	// result is the canned result returned when dispatch is nil.
	result ToolResult
	// dispatchErr is the canned error returned when dispatch is nil.
	dispatchErr error

	// calls records every invocation Dispatch saw, in order.
	calls []ToolInvocation
}

var _ ToolRouter = (*fakeToolRouter)(nil)

func (r *fakeToolRouter) SpecsForTurn(_ context.Context, _ *TurnContext) ([]tools.ToolSpec, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]tools.ToolSpec(nil), r.specs...), nil
}

func (r *fakeToolRouter) Dispatch(ctx context.Context, tc *TurnContext, inv ToolInvocation) (ToolResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, inv)
	fn := r.dispatch
	res := r.result
	err := r.dispatchErr
	r.mu.Unlock()
	if fn != nil {
		return fn(ctx, tc, inv)
	}
	return res, err
}

func (r *fakeToolRouter) recordedCalls() []ToolInvocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ToolInvocation(nil), r.calls...)
}

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// turnTestSession builds an in-process [Session] wired to the supplied model
// client and tool router, returning the session plus the outbound event channel
// so tests can assert on emitted events. The session context is derived from
// context.Background; tests cancel it via the returned cancel func.
func turnTestSession(t *testing.T, mc ModelClient, tr ToolRouter) (*Session, <-chan protocol.Event, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	evCh := make(chan protocol.Event, 256)
	sess := &Session{
		threadID:    protocol.NewThreadID("thread-turn-test"),
		sessionID:   protocol.NewSessionID("session-turn-test"),
		services:    SessionServices{ModelClient: mc, ToolRouter: tr},
		txEvent:     evCh,
		state:       NewSessionState(turnTestConfig()),
		agentStatus: protocol.AgentStatus{Kind: protocol.AgentStatusPendingInit},
		ctx:         ctx,
		cancel:      cancel,
	}
	return sess, evCh, cancel
}

// turnTestConfig returns a minimal session configuration sufficient to build a
// turn context.
func turnTestConfig() SessionConfiguration {
	return SessionConfiguration{
		ProviderID: "openai",
		CollaborationMode: protocol.CollaborationMode{
			Settings: protocol.Settings{Model: "gpt-test"},
		},
		Cwd:              "/work",
		CodexHome:        "/home/.codex",
		BaseInstructions: "be helpful",
	}
}

// installActiveTurn installs an empty active turn so dispatch paths that bump the
// tool-call counter have somewhere to write.
func installActiveTurn(sess *Session, tc *TurnContext) *ActiveTurn {
	taskCtx, cancel := context.WithCancel(sess.ctx)
	at := &ActiveTurn{
		Task: &RunningTask{
			Kind:        TaskKindRegular,
			TurnContext: tc,
			Cancel:      cancel,
			ctx:         taskCtx,
			done:        make(chan struct{}),
		},
		State: NewTurnState(),
	}
	sess.setActiveTurn(at)
	return at
}

// drainEvents collects events from ch until it is closed or a short idle timeout
// elapses, returning everything seen.
func drainEvents(ch <-chan protocol.Event) []protocol.Event {
	var out []protocol.Event
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(200 * time.Millisecond)
		case <-timer.C:
			return out
		}
	}
}

// eventsByKind counts events of each EventMsg kind.
func eventsByKind(events []protocol.Event) map[protocol.EventMsgKind]int {
	counts := make(map[protocol.EventMsgKind]int)
	for _, ev := range events {
		counts[ev.Msg.Type]++
	}
	return counts
}

// firstEvent returns the first event of the given kind, or (zero, false).
func firstEvent(events []protocol.Event, kind protocol.EventMsgKind) (protocol.Event, bool) {
	for _, ev := range events {
		if ev.Msg.Type == kind {
			return ev, true
		}
	}
	return protocol.Event{}, false
}

// ----------------------------------------------------------------------------
// Scripted-event builders (mirror the Responses stream surface)
// ----------------------------------------------------------------------------

func evCreated() api.ResponseEvent { return api.ResponseEvent{Kind: api.ResponseEventCreated} }

func evMessageAdded(id string) api.ResponseEvent {
	mid := id
	return api.ResponseEvent{
		Kind: api.ResponseEventOutputItemAdded,
		Item: &protocol.ResponseItem{Type: protocol.ResponseItemKindMessage, Role: "assistant", MessageID: &mid},
	}
}

func evTextDelta(delta string) api.ResponseEvent {
	return api.ResponseEvent{Kind: api.ResponseEventOutputTextDelta, Delta: delta}
}

func evMessageDone(id, text string) api.ResponseEvent {
	mid := id
	return api.ResponseEvent{
		Kind: api.ResponseEventOutputItemDone,
		Item: &protocol.ResponseItem{
			Type:      protocol.ResponseItemKindMessage,
			Role:      "assistant",
			MessageID: &mid,
			Content:   []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
		},
	}
}

func evFunctionCall(callID, name, args string) api.ResponseEvent {
	return api.ResponseEvent{
		Kind: api.ResponseEventOutputItemDone,
		Item: &protocol.ResponseItem{
			Type:      protocol.ResponseItemKindFunctionCall,
			Name:      name,
			CallID:    callID,
			Arguments: args,
		},
	}
}

func evReasoningDone(id, summary string) api.ResponseEvent {
	return api.ResponseEvent{
		Kind: api.ResponseEventOutputItemDone,
		Item: &protocol.ResponseItem{
			Type:        protocol.ResponseItemKindReasoning,
			ReasoningID: id,
			Summary:     []protocol.ReasoningItemReasoningSummary{{Text: summary}},
		},
	}
}

func evCompleted(endTurn bool, usage *protocol.TokenUsage) api.ResponseEvent {
	et := endTurn
	return api.ResponseEvent{Kind: api.ResponseEventCompleted, EndTurn: &et, TokenUsage: usage}
}

// ----------------------------------------------------------------------------
// runSamplingRequest: single-request streaming
// ----------------------------------------------------------------------------

func TestRunSamplingRequestStreamsTextAndCompletes(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evMessageAdded("m1"),
		evTextDelta("Hello"),
		evTextDelta(", world"),
		evMessageDone("m1", "Hello, world"),
		evCompleted(true, nil),
	}})
	tr := &fakeToolRouter{}
	sess, evCh, cancel := turnTestSession(t, mc, tr)
	defer cancel()

	tc, err := newTurnContext(sess.ctx, sess, "turn-1", nil)
	if err != nil {
		t.Fatalf("newTurnContext: %v", err)
	}

	out, err := runSamplingRequest(sess.ctx, sess, tc)
	if err != nil {
		t.Fatalf("runSamplingRequest: %v", err)
	}
	if out.NeedsFollowUp {
		t.Fatalf("end_turn=true should not need follow-up")
	}
	if out.LastAgentMessage == nil || *out.LastAgentMessage != "Hello, world" {
		t.Fatalf("LastAgentMessage = %v, want %q", out.LastAgentMessage, "Hello, world")
	}

	events := drainEvents(evCh)
	counts := eventsByKind(events)
	if counts[protocol.EventMsgKindAgentMessageContentDelta] != 2 {
		t.Fatalf("want 2 content deltas, got %d", counts[protocol.EventMsgKindAgentMessageContentDelta])
	}
	if counts[protocol.EventMsgKindAgentMessage] != 1 {
		t.Fatalf("want 1 terminal agent message, got %d", counts[protocol.EventMsgKindAgentMessage])
	}
	if counts[protocol.EventMsgKindItemStarted] != 1 {
		t.Fatalf("want 1 ItemStarted, got %d", counts[protocol.EventMsgKindItemStarted])
	}
	if counts[protocol.EventMsgKindItemCompleted] != 1 {
		t.Fatalf("want 1 ItemCompleted, got %d", counts[protocol.EventMsgKindItemCompleted])
	}

	// The assistant message must have been recorded into history.
	hist := sess.HistoryItems()
	if len(hist) != 1 || hist[0].Type != protocol.ResponseItemKindMessage || hist[0].Role != "assistant" {
		t.Fatalf("history did not record assistant message: %+v", hist)
	}
}

func TestRunSamplingRequestEmitsReasoning(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evReasoningDone("r1", "thinking hard"),
		evMessageDone("m1", "done"),
		evCompleted(true, nil),
	}})
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	if _, err := runSamplingRequest(sess.ctx, sess, tc); err != nil {
		t.Fatalf("runSamplingRequest: %v", err)
	}

	events := drainEvents(evCh)
	if _, ok := firstEvent(events, protocol.EventMsgKindAgentReasoning); !ok {
		t.Fatalf("expected an AgentReasoning event, got kinds %v", eventsByKind(events))
	}
}

func TestRunSamplingRequestEmitsTokenCount(t *testing.T) {
	window := int64(8192)
	usage := &protocol.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}
	mc := NewMockModelClient("gpt-test", &window, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evMessageDone("m1", "hi"),
		evCompleted(true, usage),
	}})
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	if _, err := runSamplingRequest(sess.ctx, sess, tc); err != nil {
		t.Fatalf("runSamplingRequest: %v", err)
	}

	events := drainEvents(evCh)
	ev, ok := firstEvent(events, protocol.EventMsgKindTokenCount)
	if !ok {
		t.Fatalf("expected a TokenCount event, got kinds %v", eventsByKind(events))
	}
	if ev.Msg.TokenCount == nil || ev.Msg.TokenCount.Info == nil {
		t.Fatalf("TokenCount event missing info: %+v", ev.Msg.TokenCount)
	}
	if got := ev.Msg.TokenCount.Info.TotalTokenUsage.TotalTokens; got != 15 {
		t.Fatalf("accounted total tokens = %d, want 15", got)
	}
}

func TestRunSamplingRequestStreamClosedEarlyErrors(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evMessageDone("m1", "no completed event"),
		// No Completed event: stream closes early.
	}})
	sess, _, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	_, err := runSamplingRequest(sess.ctx, sess, tc)
	if err == nil {
		t.Fatalf("expected error when stream closes before response.completed")
	}
}

func TestRunSamplingRequestStreamStartErrorWrapped(t *testing.T) {
	boom := errors.New("provider unavailable")
	mc := NewMockModelClient("gpt-test", nil, MockTurn{StreamErr: boom})
	sess, _, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	_, err := runSamplingRequest(sess.ctx, sess, tc)
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected wrapped stream error, got %v", err)
	}
}

// ----------------------------------------------------------------------------
// Tool dispatch
// ----------------------------------------------------------------------------

func TestRunSamplingRequestDispatchesFunctionCall(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evFunctionCall("call-1", "do_thing", `{"k":"v"}`),
		evCompleted(true, nil),
	}})
	tr := &fakeToolRouter{result: ToolResult{Output: "tool says hi", Success: true}}
	sess, _, cancel := turnTestSession(t, mc, tr)
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	installActiveTurn(sess, tc)

	out, err := runSamplingRequest(sess.ctx, sess, tc)
	if err != nil {
		t.Fatalf("runSamplingRequest: %v", err)
	}
	if !out.NeedsFollowUp {
		t.Fatalf("a tool call must mark the turn as needing follow-up")
	}

	calls := tr.recordedCalls()
	if len(calls) != 1 {
		t.Fatalf("want 1 dispatched call, got %d", len(calls))
	}
	if calls[0].CallID != "call-1" || calls[0].Name.String() != "do_thing" {
		t.Fatalf("unexpected invocation: %+v", calls[0])
	}
	if string(calls[0].Arguments) != `{"k":"v"}` {
		t.Fatalf("arguments = %q, want %q", string(calls[0].Arguments), `{"k":"v"}`)
	}

	// History must carry the call item AND its function_call_output.
	hist := sess.HistoryItems()
	var sawCall, sawOutput bool
	for _, it := range hist {
		switch it.Type {
		case protocol.ResponseItemKindFunctionCall:
			sawCall = true
		case protocol.ResponseItemKindFunctionCallOutput:
			sawOutput = true
			if it.Output == nil || it.Output.Text == nil || *it.Output.Text != "tool says hi" {
				t.Fatalf("function_call_output payload wrong: %+v", it.Output)
			}
			if it.Output.Success == nil || !*it.Output.Success {
				t.Fatalf("function_call_output success flag wrong: %+v", it.Output)
			}
		}
	}
	if !sawCall || !sawOutput {
		t.Fatalf("history missing call/output (call=%v output=%v): %+v", sawCall, sawOutput, hist)
	}

	// The active turn's tool-call counter must have advanced.
	if got := sess.ActiveTurn().State.ToolCalls(); got != 1 {
		t.Fatalf("tool-call counter = %d, want 1", got)
	}
}

func TestDispatchFunctionCallErrorBecomesFailedOutput(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evFunctionCall("call-err", "broken", `{}`),
		evCompleted(true, nil),
	}})
	tr := &fakeToolRouter{dispatchErr: errors.New("kaboom")}
	sess, _, cancel := turnTestSession(t, mc, tr)
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	installActiveTurn(sess, tc)

	if _, err := runSamplingRequest(sess.ctx, sess, tc); err != nil {
		t.Fatalf("a tool dispatch error must NOT fail the turn: %v", err)
	}

	hist := sess.HistoryItems()
	var found bool
	for _, it := range hist {
		if it.Type == protocol.ResponseItemKindFunctionCallOutput {
			found = true
			if it.Output == nil || it.Output.Success == nil || *it.Output.Success {
				t.Fatalf("dispatch error should yield a failed output, got %+v", it.Output)
			}
		}
	}
	if !found {
		t.Fatalf("expected a failed function_call_output in history: %+v", hist)
	}
}

// ----------------------------------------------------------------------------
// runTurn: multi-request loop until end-turn
// ----------------------------------------------------------------------------

func TestRunTurnLoopsUntilEndTurn(t *testing.T) {
	// Turn 1: function call (end_turn=false -> follow-up). Turn 2: final message.
	mc := NewMockModelClient("gpt-test", nil,
		MockTurn{Events: []api.ResponseEvent{
			evCreated(),
			evFunctionCall("call-1", "lookup", `{}`),
			evCompleted(false, nil),
		}},
		MockTurn{Events: []api.ResponseEvent{
			evCreated(),
			evMessageDone("m2", "all done"),
			evCompleted(true, nil),
		}},
	)
	tr := &fakeToolRouter{result: ToolResult{Output: "result data", Success: true}}
	sess, _, cancel := turnTestSession(t, mc, tr)
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	installActiveTurn(sess, tc)

	input := []turnInput{{UserContent: []protocol.UserInput{{Type: protocol.UserInputKindText, Text: "go"}}}}
	last := runTurn(sess.ctx, sess, tc, input)

	if last == nil || *last != "all done" {
		t.Fatalf("runTurn last message = %v, want %q", last, "all done")
	}
	if mc.CallCount() != 2 {
		t.Fatalf("expected 2 model requests, got %d", mc.CallCount())
	}
	if len(tr.recordedCalls()) != 1 {
		t.Fatalf("expected 1 tool dispatch, got %d", len(tr.recordedCalls()))
	}

	// The second request's prompt must include the tool output recorded by the
	// first request, proving the loop folds outputs back into history.
	prompts := mc.ReceivedPrompts()
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(prompts))
	}
	var sawOutput bool
	for _, it := range prompts[1].Input {
		if it.Type == protocol.ResponseItemKindFunctionCallOutput {
			sawOutput = true
		}
	}
	if !sawOutput {
		t.Fatalf("second prompt did not include the prior tool output: %+v", prompts[1].Input)
	}
}

func TestRunTurnRecordsUserInput(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evMessageDone("m1", "ack"),
		evCompleted(true, nil),
	}})
	sess, _, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	installActiveTurn(sess, tc)

	input := []turnInput{{UserContent: []protocol.UserInput{{Type: protocol.UserInputKindText, Text: "hello model"}}}}
	runTurn(sess.ctx, sess, tc, input)

	hist := sess.HistoryItems()
	if len(hist) == 0 || hist[0].Role != "user" {
		t.Fatalf("expected user input recorded first, got %+v", hist)
	}
	if hist[0].Content[0].Text != "hello model" {
		t.Fatalf("recorded user text = %q, want %q", hist[0].Content[0].Text, "hello model")
	}
}

func TestRunTurnEmitsErrorEventOnStreamFailure(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{StreamErr: errors.New("network down")})
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	installActiveTurn(sess, tc)

	last := runTurn(sess.ctx, sess, tc, nil)
	if last != nil {
		t.Fatalf("expected nil last message on stream failure, got %v", last)
	}

	events := drainEvents(evCh)
	if _, ok := firstEvent(events, protocol.EventMsgKindError); !ok {
		t.Fatalf("expected an Error event, got kinds %v", eventsByKind(events))
	}
}

// ----------------------------------------------------------------------------
// Cancellation / interrupt
// ----------------------------------------------------------------------------

func TestRunSamplingRequestAbortsOnCancellation(t *testing.T) {
	// A dispatch that blocks until its context is cancelled lets us assert the
	// turn aborts cleanly on interrupt.
	released := make(chan struct{})
	tr := &fakeToolRouter{dispatch: func(ctx context.Context, _ *TurnContext, _ ToolInvocation) (ToolResult, error) {
		<-ctx.Done()
		close(released)
		return ToolResult{}, ctx.Err()
	}}
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evFunctionCall("call-1", "slow", `{}`),
		evCompleted(true, nil),
	}})
	sess, _, cancel := turnTestSession(t, mc, tr)
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	installActiveTurn(sess, tc)

	ctx, taskCancel := context.WithCancel(sess.ctx)
	done := make(chan error, 1)
	go func() {
		_, err := runSamplingRequest(ctx, sess, tc)
		done <- err
	}()

	// Give the goroutine time to reach the blocking dispatch, then interrupt.
	time.Sleep(20 * time.Millisecond)
	taskCancel()

	select {
	case err := <-done:
		if !errors.Is(err, ErrTurnAborted) && !errors.Is(err, context.Canceled) {
			t.Fatalf("expected abort/cancel error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runSamplingRequest did not return after cancellation")
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("tool dispatch was not unblocked by cancellation")
	}
}

func TestRunTurnReturnsEarlyWhenContextAlreadyCancelled(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil) // no scripted turns: must not be called
	sess, _, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)

	ctx, c := context.WithCancel(sess.ctx)
	c() // cancel before running
	last := runTurn(ctx, sess, tc, nil)
	if last != nil {
		t.Fatalf("expected nil last message, got %v", last)
	}
	if mc.CallCount() != 0 {
		t.Fatalf("model must not be called when ctx is pre-cancelled, got %d calls", mc.CallCount())
	}
}

// ----------------------------------------------------------------------------
// spawnTask lifecycle + interrupt + shutdown (handlers.rs surface)
// ----------------------------------------------------------------------------

func TestSpawnTaskEmitsLifecycleEvents(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evMessageDone("m1", "final answer"),
		evCompleted(true, nil),
	}})
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)

	spawnTask(sess, tc, TaskKindRegular, func(ctx context.Context) *string {
		return runTurn(ctx, sess, tc, nil)
	})

	// Wait for the task to finish.
	at := sess.ActiveTurn()
	if at == nil || at.Task == nil {
		t.Fatal("spawnTask did not install an active turn")
	}
	select {
	case <-at.Task.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("task did not complete")
	}

	events := drainEvents(evCh)
	if _, ok := firstEvent(events, protocol.EventMsgKindTurnStarted); !ok {
		t.Fatalf("expected TurnStarted, got %v", eventsByKind(events))
	}
	ev, ok := firstEvent(events, protocol.EventMsgKindTurnComplete)
	if !ok {
		t.Fatalf("expected TurnComplete, got %v", eventsByKind(events))
	}
	if ev.Msg.TurnComplete == nil || ev.Msg.TurnComplete.LastAgentMessage == nil ||
		*ev.Msg.TurnComplete.LastAgentMessage != "final answer" {
		t.Fatalf("TurnComplete last message wrong: %+v", ev.Msg.TurnComplete)
	}

	// Active turn slot must be cleared after completion.
	if sess.ActiveTurn() != nil {
		t.Fatalf("active turn should be cleared after the task finishes")
	}
}

func TestInterruptAbortsActiveTurn(t *testing.T) {
	// The model stream blocks on a never-firing channel so the turn stays running
	// until interrupted.
	block := make(chan struct{})
	mc := &blockingModelClient{block: block}
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()
	defer close(block)

	tc, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	spawnTask(sess, tc, TaskKindRegular, func(ctx context.Context) *string {
		return runTurn(ctx, sess, tc, nil)
	})

	at := sess.ActiveTurn()
	if at == nil {
		t.Fatal("no active turn after spawn")
	}

	// Interrupt mirrors Op::Interrupt handling.
	handleInterrupt(sess)

	select {
	case <-at.Task.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("task did not abort after interrupt")
	}

	events := drainEvents(evCh)
	ev, ok := firstEvent(events, protocol.EventMsgKindTurnAborted)
	if !ok {
		t.Fatalf("expected TurnAborted after interrupt, got %v", eventsByKind(events))
	}
	if ev.Msg.TurnAborted == nil || ev.Msg.TurnAborted.Reason != protocol.TurnAbortReasonInterrupted {
		t.Fatalf("TurnAborted reason wrong: %+v", ev.Msg.TurnAborted)
	}
}

func TestSpawnTaskReplacesPreviousTask(t *testing.T) {
	block := make(chan struct{})
	mc := &blockingModelClient{block: block}
	sess, _, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()
	defer close(block)

	tc1, _ := newTurnContext(sess.ctx, sess, "turn-1", nil)
	spawnTask(sess, tc1, TaskKindRegular, func(ctx context.Context) *string {
		return runTurn(ctx, sess, tc1, nil)
	})
	first := sess.ActiveTurn().Task

	// Spawning a second task must cancel the first (replace semantics).
	tc2, _ := newTurnContext(sess.ctx, sess, "turn-2", nil)
	spawnTask(sess, tc2, TaskKindRegular, func(ctx context.Context) *string {
		return runTurn(ctx, sess, tc2, nil)
	})

	select {
	case <-first.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("first task was not cancelled when replaced")
	}
}

// ----------------------------------------------------------------------------
// End-to-end through the public Codex/Spawn API (submission loop + EQ)
// ----------------------------------------------------------------------------

func TestCodexUserInputTurnEndToEnd(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, MockTurn{Events: []api.ResponseEvent{
		evCreated(),
		evMessageAdded("m1"),
		evTextDelta("hi"),
		evMessageDone("m1", "hi"),
		evCompleted(true, nil),
	}})
	ok, err := Spawn(context.Background(), CodexSpawnArgs{
		ThreadID:      protocol.NewThreadID("e2e-thread"),
		Configuration: turnTestConfig(),
		Services:      SessionServices{ModelClient: mc, ToolRouter: &fakeToolRouter{}},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	codex := ok.Codex
	defer func() { _ = codex.Shutdown(context.Background()) }()

	// Drain the initial SessionConfigured event.
	ctx := context.Background()
	if ev, err := codex.NextEvent(ctx); err != nil || ev.Msg.Type != protocol.EventMsgKindSessionConfigured {
		t.Fatalf("first event = %+v (err %v), want SessionConfigured", ev.Msg.Type, err)
	}

	if _, err := codex.Submit(protocol.Op{
		Type:  protocol.OpUserInput,
		Items: []protocol.UserInput{{Type: protocol.UserInputKindText, Text: "hello"}},
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Collect events until TurnComplete.
	seen := collectUntil(t, codex, protocol.EventMsgKindTurnComplete)
	counts := eventsByKind(seen)
	if counts[protocol.EventMsgKindTurnStarted] != 1 {
		t.Fatalf("want 1 TurnStarted, got %d (%v)", counts[protocol.EventMsgKindTurnStarted], counts)
	}
	if counts[protocol.EventMsgKindAgentMessage] != 1 {
		t.Fatalf("want 1 AgentMessage, got %d", counts[protocol.EventMsgKindAgentMessage])
	}
}

func TestCodexInterruptEndToEnd(t *testing.T) {
	block := make(chan struct{})
	mc := &blockingModelClient{block: block}
	ok, err := Spawn(context.Background(), CodexSpawnArgs{
		ThreadID:      protocol.NewThreadID("e2e-interrupt"),
		Configuration: turnTestConfig(),
		Services:      SessionServices{ModelClient: mc, ToolRouter: &fakeToolRouter{}},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	codex := ok.Codex
	defer close(block)
	defer func() { _ = codex.Shutdown(context.Background()) }()

	ctx := context.Background()
	if _, err := codex.NextEvent(ctx); err != nil { // SessionConfigured
		t.Fatalf("drain SessionConfigured: %v", err)
	}

	if _, err := codex.Submit(protocol.Op{
		Type:  protocol.OpUserInput,
		Items: []protocol.UserInput{{Type: protocol.UserInputKindText, Text: "long task"}},
	}); err != nil {
		t.Fatalf("Submit user input: %v", err)
	}

	// Wait for TurnStarted so the turn is actually running before interrupting.
	collectUntil(t, codex, protocol.EventMsgKindTurnStarted)

	if _, err := codex.Submit(protocol.Op{Type: protocol.OpInterrupt}); err != nil {
		t.Fatalf("Submit interrupt: %v", err)
	}

	seen := collectUntil(t, codex, protocol.EventMsgKindTurnAborted)
	ev, _ := firstEvent(seen, protocol.EventMsgKindTurnAborted)
	if ev.Msg.TurnAborted == nil || ev.Msg.TurnAborted.Reason != protocol.TurnAbortReasonInterrupted {
		t.Fatalf("expected interrupted abort, got %+v", ev.Msg.TurnAborted)
	}
}

func TestCodexShutdownEmitsShutdownComplete(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil)
	ok, err := Spawn(context.Background(), CodexSpawnArgs{
		ThreadID:      protocol.NewThreadID("e2e-shutdown"),
		Configuration: turnTestConfig(),
		Services:      SessionServices{ModelClient: mc, ToolRouter: &fakeToolRouter{}},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	codex := ok.Codex

	ctx := context.Background()
	if _, err := codex.NextEvent(ctx); err != nil { // SessionConfigured
		t.Fatalf("drain SessionConfigured: %v", err)
	}

	// Submit Shutdown directly (not via the helper, so we can observe the
	// terminal ShutdownComplete event before the queues drain).
	if _, err := codex.Submit(protocol.Op{Type: protocol.OpShutdown}); err != nil {
		t.Fatalf("submit shutdown: %v", err)
	}

	// The shutdown handler must emit a terminal ShutdownComplete event.
	seen := collectUntil(t, codex, protocol.EventMsgKindShutdownComplete)
	if _, ok := firstEvent(seen, protocol.EventMsgKindShutdownComplete); !ok {
		t.Fatalf("expected ShutdownComplete event, got %v", eventsByKind(seen))
	}

	// The submission loop must terminate; NextEvent eventually reports the
	// agent has died once the event queue is drained.
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, err := codex.NextEvent(waitCtx)
		if errors.Is(err, ErrInternalAgentDied) {
			return
		}
		if err != nil {
			t.Fatalf("waiting for agent-died after shutdown: %v", err)
		}
	}
}

// collectUntil drains events from codex until one of the target kind is seen,
// returning all events collected (including the target). It fails the test on
// timeout.
func collectUntil(t *testing.T, codex *Codex, target protocol.EventMsgKind) []protocol.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var seen []protocol.Event
	for {
		ev, err := codex.NextEvent(ctx)
		if err != nil {
			t.Fatalf("NextEvent waiting for %s: %v (seen %v)", target, err, eventsByKind(seen))
		}
		seen = append(seen, ev)
		if ev.Msg.Type == target {
			return seen
		}
	}
}

// blockingModelClient is a ModelClient whose Stream returns a channel that never
// produces events until block is closed or ctx is cancelled, letting tests hold
// a turn open for interrupt/replace scenarios.
type blockingModelClient struct {
	block chan struct{}
}

var _ ModelClient = (*blockingModelClient)(nil)

func (b *blockingModelClient) ModelSlug() string     { return "gpt-test" }
func (b *blockingModelClient) ContextWindow() *int64 { return nil }

func (b *blockingModelClient) Stream(ctx context.Context, _ Prompt) (<-chan api.ResponseEvent, error) {
	out := make(chan api.ResponseEvent)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
		case <-b.block:
		}
	}()
	return out, nil
}

// fmtInvocations renders invocations for test diagnostics.
func fmtInvocations(calls []ToolInvocation) string {
	var b []string
	for _, c := range calls {
		b = append(b, fmt.Sprintf("%s(%s)", c.Name.String(), string(c.Arguments)))
	}
	return fmt.Sprintf("%v", b)
}

var _ = fmtInvocations // keep helper available for future assertions
