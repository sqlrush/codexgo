package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// gatedModelClient serves scripted turns but blocks each Stream call until the
// test releases it, so a turn can be observed mid-flight (steer window).
type gatedModelClient struct {
	mu    sync.Mutex
	turns []MockTurn
	gate  chan struct{} // receive = release one stream
	calls int
}

func (g *gatedModelClient) ModelSlug() string     { return "gpt-test" }
func (g *gatedModelClient) ContextWindow() *int64 { return nil }

func (g *gatedModelClient) Stream(ctx context.Context, _ Prompt) (<-chan api.ResponseEvent, error) {
	select {
	case <-g.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	g.mu.Lock()
	idx := g.calls
	g.calls++
	var turn MockTurn
	if idx < len(g.turns) {
		turn = g.turns[idx]
	} else {
		turn = g.turns[len(g.turns)-1]
	}
	g.mu.Unlock()
	out := make(chan api.ResponseEvent, len(turn.Events)+1)
	for _, ev := range turn.Events {
		out <- ev
	}
	close(out)
	return out, nil
}

func (g *gatedModelClient) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func doneTurn(text string) MockTurn {
	return MockTurn{Events: []api.ResponseEvent{evCreated(), evMessageDone("m", text), evCompleted(true, nil)}}
}

func textInput(text string) []protocol.UserInput {
	return []protocol.UserInput{{Type: protocol.UserInputKindText, Text: text}}
}

func lastUserMessageText(sess *Session) string {
	items := sess.HistoryItems()
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].IsUserMessage() && len(items[i].Content) > 0 {
			return items[i].Content[0].Text
		}
	}
	return ""
}

// TestSteerInputFoldsIntoRunningTurn asserts user input submitted while a
// regular turn samples is admitted as Steered, recorded into history before
// the next sampling request, and keeps the turn alive for one more request.
func TestSteerInputFoldsIntoRunningTurn(t *testing.T) {
	mc := &gatedModelClient{turns: []MockTurn{doneTurn("first reply"), doneTurn("second reply")}, gate: make(chan struct{})}
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()

	handleUserInput(sess, "turn-1", protocol.Op{Type: protocol.OpUserInput, Items: textInput("hello")}, nil)
	first := sess.ActiveTurn()
	if first == nil || first.Task == nil {
		t.Fatalf("expected an active turn after the first submission")
	}

	// Steer while the first sampling request is still gated.
	admission, err := admitUserInput(sess, "turn-2", protocol.Op{Type: protocol.OpUserInput, Items: textInput("also this")}, nil)
	if err != nil {
		t.Fatalf("admit steer: %v", err)
	}
	if admission.Kind != UserMessageAdmissionSteered || admission.TurnID != "turn-1" {
		t.Fatalf("admission = %+v, want steered into turn-1", admission)
	}
	if sess.ActiveTurn() != first {
		t.Fatalf("steering must not replace the running task")
	}
	if !first.State.HasPendingUserInput() {
		t.Fatalf("steered input should be pending on the turn state")
	}

	// Release both sampling requests: the first samples "hello", the loop
	// sees pending input, drains "also this" into history and samples again.
	mc.gate <- struct{}{}
	mc.gate <- struct{}{}
	select {
	case <-first.Task.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not finish")
	}
	if got := mc.callCount(); got != 2 {
		t.Fatalf("sampling requests = %d, want 2 (one extra for the steered input)", got)
	}
	if got := lastUserMessageText(sess); got != "also this" {
		t.Fatalf("last user message in history = %q, want the steered input", got)
	}
	events := drainEvents(evCh)
	if n := eventsByKind(events)[protocol.EventMsgKindTurnStarted]; n != 1 {
		t.Fatalf("turn_started events = %d, want exactly 1 (steer does not start a turn)", n)
	}
}

// TestUserInputStartsTurnWhenIdle asserts the no-active-turn path admits as
// Started with the submission id as the turn id.
func TestUserInputStartsTurnWhenIdle(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, doneTurn("ok"))
	sess, _, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()
	admission, err := admitUserInput(sess, "turn-9", protocol.Op{Type: protocol.OpUserInput, Items: textInput("hi")}, nil)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admission.Kind != UserMessageAdmissionStarted || admission.TurnID != "turn-9" {
		t.Fatalf("admission = %+v, want started turn-9", admission)
	}
	<-sess.ActiveTurn().Task.Done()
}

// TestSteerRejectedForNonSteerableTurn asserts a running compaction/review turn
// rejects steering with a BadRequest error event and no new turn.
func TestSteerRejectedForNonSteerableTurn(t *testing.T) {
	block := make(chan struct{})
	mc := &blockingModelClient{block: block}
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()
	defer close(block)

	tc, _ := newTurnContext(sess.ctx, sess, "compact-1", nil)
	spawnTask(sess, tc, TaskKindCompact, func(ctx context.Context) *string {
		<-ctx.Done()
		return nil
	})
	compact := sess.ActiveTurn()

	_, err := admitUserInput(sess, "turn-2", protocol.Op{Type: protocol.OpUserInput, Items: textInput("hi")}, nil)
	if err == nil {
		t.Fatalf("steering a compaction turn must fail")
	}
	if sess.ActiveTurn() != compact {
		t.Fatalf("a rejected admission must not replace the compaction turn")
	}
	ev, ok := firstEvent(drainEvents(evCh), protocol.EventMsgKindError)
	if !ok || ev.Msg.Error == nil || ev.Msg.Error.CodexErrorInfo == nil || ev.Msg.Error.CodexErrorInfo.Kind != protocol.CodexErrorInfoBadRequest {
		t.Fatalf("expected a bad_request error event, got %+v", ev)
	}
	// Steering into the wrong turn id is rejected too.
	if _, err := sess.SteerInput(textInput("x"), "other-turn", nil); !IsSteerInputError(err, SteerInputExpectedTurnMismatch) {
		t.Fatalf("expected turn mismatch, got %v", err)
	}
	if _, err := sess.SteerInput(nil, "", nil); !IsSteerInputError(err, SteerInputActiveTurnNotSteerable) {
		t.Fatalf("expected not-steerable (compact) before the empty-input check, got %v", err)
	}
}

// TestMailboxTriggerTurnStartsTurnWhenIdle asserts trigger-turn mail on an
// idle session starts a regular turn whose first request carries the mail as
// an agent_message item.
func TestMailboxTriggerTurnStartsTurnWhenIdle(t *testing.T) {
	mc := NewMockModelClient("gpt-test", nil, doneTurn("ack"))
	sess, evCh, cancel := turnTestSession(t, mc, &fakeToolRouter{})
	defer cancel()
	author, _ := protocol.NewAgentPath("/root")
	recipient, _ := protocol.NewAgentPath("/root/worker")
	handleInterAgentCommunication(sess, "mail-1", protocol.Op{
		Type:          protocol.OpInterAgentCommunication,
		Communication: &protocol.InterAgentCommunication{Author: author, Recipient: recipient, Content: "do it", TriggerTurn: true},
	})
	at := sess.ActiveTurn()
	if at == nil || at.Task == nil {
		t.Fatalf("trigger-turn mail should start a turn on an idle session")
	}
	<-at.Task.Done()
	var mail *protocol.ResponseItem
	for _, it := range sess.HistoryItems() {
		if it.Type == protocol.ResponseItemKindAgentMessage {
			item := it
			mail = &item
		}
	}
	if mail == nil || mail.Author != "/root" || mail.Recipient != "/root/worker" || len(mail.AgentMessageContent) != 1 || mail.AgentMessageContent[0].Text != "do it" {
		t.Fatalf("mail was not recorded as an agent_message item: %+v", mail)
	}
	if n := eventsByKind(drainEvents(evCh))[protocol.EventMsgKindTurnStarted]; n != 1 {
		t.Fatalf("turn_started = %d, want 1", n)
	}
	// Non-trigger mail on an idle session just queues.
	handleInterAgentCommunication(sess, "mail-2", protocol.Op{
		Type:          protocol.OpInterAgentCommunication,
		Communication: &protocol.InterAgentCommunication{Author: author, Recipient: recipient, Content: "fyi"},
	})
	if at := sess.ActiveTurn(); at != nil && at.Task != nil {
		t.Fatalf("queue-only mail must not start a turn")
	}
	if !sess.InputQueue().HasPendingMailboxItems() {
		t.Fatalf("queue-only mail should stay queued for the next turn")
	}
}

// TestInputQueueActivityAndDrain covers the queue primitives: activity
// notifications, pending detection, and the parent-turn-id reduction.
func TestInputQueueActivityAndDrain(t *testing.T) {
	q := NewInputQueue()
	ch, pending, cancel := q.SubscribeActivity(nil)
	defer cancel()
	if pending != nil {
		t.Fatalf("fresh queue has no pending activity, got %v", *pending)
	}
	author, _ := protocol.NewAgentPath("/root")
	recipient, _ := protocol.NewAgentPath("/root/w")
	p1 := "turn-a"
	q.EnqueueMailboxCommunication(protocol.InterAgentCommunication{Author: author, Recipient: recipient, Content: "1", TriggerTurn: true}, &p1)
	q.EnqueueMailboxCommunication(protocol.InterAgentCommunication{Author: author, Recipient: recipient, Content: "2"}, nil)
	select {
	case a := <-ch:
		if a != InputQueueActivityMailbox {
			t.Fatalf("activity = %v, want mailbox", a)
		}
	case <-time.After(time.Second):
		t.Fatal("no mailbox activity delivered")
	}
	if !q.HasPendingMailboxItems() || !q.HasTriggerTurnMailboxItems() {
		t.Fatalf("pending / trigger-turn detection failed")
	}
	items, parent := q.DrainMailboxInputItems()
	if len(items) != 2 || parent == nil || *parent != "turn-a" {
		t.Fatalf("drain = %d items parent %v, want 2 items parent turn-a", len(items), parent)
	}
	if q.HasPendingMailboxItems() {
		t.Fatalf("drain should empty the mailbox")
	}
	// Disagreeing parent turn ids collapse to nil.
	p2 := "turn-b"
	q.EnqueueMailboxCommunication(protocol.InterAgentCommunication{Author: author, Recipient: recipient, Content: "3", TriggerTurn: true}, &p1)
	q.EnqueueMailboxCommunication(protocol.InterAgentCommunication{Author: author, Recipient: recipient, Content: "4", TriggerTurn: true}, &p2)
	if _, parent := q.DrainMailboxInputItems(); parent != nil {
		t.Fatalf("disagreeing parent turn ids should yield nil, got %q", *parent)
	}
	// A steer on a turn state reports pending Steer to new subscribers.
	ts := NewTurnState()
	q.ExtendPendingInputAndAcceptMailboxDelivery(ts, []turnInput{{UserContent: textInput("s")}})
	_, pending, cancel2 := q.SubscribeActivity(ts)
	defer cancel2()
	if pending == nil || *pending != InputQueueActivitySteer {
		t.Fatalf("pending activity = %v, want steer", pending)
	}
	if !q.HasPendingInput(&ActiveTurn{State: ts}) {
		t.Fatalf("HasPendingInput should see the steered input")
	}
	ts.SetMailboxDeliveryPhase(MailboxNextTurn)
	if q.HasPendingInput(&ActiveTurn{State: ts}) {
		t.Fatalf("a turn that no longer accepts delivery reports no pending input")
	}
}

// TestSubmitUserMessageReportsAdmission drives the public API end to end: the
// first message starts a turn, a second one during it is steered.
func TestSubmitUserMessageReportsAdmission(t *testing.T) {
	mc := &gatedModelClient{turns: []MockTurn{doneTurn("a"), doneTurn("b")}, gate: make(chan struct{})}
	ok, err := Spawn(context.Background(), CodexSpawnArgs{
		ThreadID:      protocol.NewThreadID("adm-thread"),
		Configuration: turnTestConfig(),
		Services:      SessionServices{ModelClient: mc, ToolRouter: &fakeToolRouter{}},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	codex := ok.Codex
	defer func() { _ = codex.Shutdown(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started, err := codex.SubmitUserMessage(ctx, protocol.Op{Type: protocol.OpUserInput, Items: textInput("one")}, nil)
	if err != nil || started.Kind != UserMessageAdmissionStarted || started.TurnID == "" {
		t.Fatalf("first admission = %+v, %v; want started", started, err)
	}
	steered, err := codex.SubmitUserMessage(ctx, protocol.Op{Type: protocol.OpUserInput, Items: textInput("two")}, nil)
	if err != nil || steered.Kind != UserMessageAdmissionSteered || steered.TurnID != started.TurnID {
		t.Fatalf("second admission = %+v, %v; want steered into %s", steered, err, started.TurnID)
	}
	mc.gate <- struct{}{}
	mc.gate <- struct{}{}
	// Drain until the turn completes.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ev, err := codex.NextEvent(ctx)
		if err != nil {
			t.Fatalf("NextEvent: %v", err)
		}
		if ev.Msg.Type == protocol.EventMsgKindTurnComplete {
			return
		}
	}
	t.Fatal("turn did not complete")
}
