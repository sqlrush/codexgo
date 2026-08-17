package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/features"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// This file tests the deferred collab (multi-agent) tool executors once they are
// wired to a CollabControl. The reference behavior is codex-core's
// tools/handlers/multi_agents/{spawn,send_input,resume_agent,wait,close_agent}.rs
// plus multi_agents_common.rs. A fakeCollabControl stands in for the
// internal/multiagent control plane so the executors can be exercised at the
// core boundary without spawning real threads.

// fakeCollabControl is an in-memory CollabControl for executor tests. It records
// each call and returns scripted results.
type fakeCollabControl struct {
	mu sync.Mutex

	spawnResult CollabSpawnResult
	spawnErr    error
	spawnCalls  int
	lastSpawn   CollabSpawnRequest

	sendSubID string
	sendErr   error
	sendCalls int
	lastSend  collabSendCall

	interruptCalls int

	closeErr     error
	closeCalls   int
	closedThread *protocol.ThreadID

	resumeErr    error
	resumeCalls  int
	resumeThread *protocol.ThreadID

	statuses map[string]protocol.AgentStatus
	metadata map[string]CollabAgentMetadata

	// subscribeCh, when set for a thread, is returned by SubscribeStatus so a
	// test can drive transitions.
	subscribeCh map[string]chan protocol.AgentStatus
}

type collabSendCall struct {
	threadID protocol.ThreadID
	op       protocol.Op
}

func newFakeCollabControl() *fakeCollabControl {
	return &fakeCollabControl{
		statuses:    map[string]protocol.AgentStatus{},
		metadata:    map[string]CollabAgentMetadata{},
		subscribeCh: map[string]chan protocol.AgentStatus{},
	}
}

func (f *fakeCollabControl) SpawnAgent(_ context.Context, req CollabSpawnRequest) (CollabSpawnResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawnCalls++
	f.lastSpawn = req
	if f.spawnErr != nil {
		return CollabSpawnResult{}, f.spawnErr
	}
	return f.spawnResult, nil
}

func (f *fakeCollabControl) SendInput(_ context.Context, threadID protocol.ThreadID, op protocol.Op) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCalls++
	f.lastSend = collabSendCall{threadID: threadID, op: op}
	if f.sendErr != nil {
		return "", f.sendErr
	}
	return f.sendSubID, nil
}

func (f *fakeCollabControl) InterruptAgent(_ context.Context, _ protocol.ThreadID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interruptCalls++
	return "interrupt-sub", nil
}

func (f *fakeCollabControl) CloseAgent(_ context.Context, threadID protocol.ThreadID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	id := threadID
	f.closedThread = &id
	return f.closeErr
}

func (f *fakeCollabControl) ResumeAgent(_ context.Context, req CollabResumeRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumeCalls++
	id := req.ThreadID
	f.resumeThread = &id
	return f.resumeErr
}

func (f *fakeCollabControl) GetStatus(threadID protocol.ThreadID) protocol.AgentStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.statuses[threadID.String()]; ok {
		return s
	}
	return protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}
}

func (f *fakeCollabControl) GetAgentMetadata(threadID protocol.ThreadID) *CollabAgentMetadata {
	f.mu.Lock()
	defer f.mu.Unlock()
	if md, ok := f.metadata[threadID.String()]; ok {
		m := md
		return &m
	}
	return nil
}

func (f *fakeCollabControl) AgentConfigSnapshot(threadID protocol.ThreadID) *CollabAgentConfigSnapshot {
	return nil
}

func (f *fakeCollabControl) SubscribeStatus(_ context.Context, threadID protocol.ThreadID) (protocol.AgentStatus, <-chan protocol.AgentStatus, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	current := protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}
	if s, ok := f.statuses[threadID.String()]; ok {
		current = s
	}
	ch, ok := f.subscribeCh[threadID.String()]
	if !ok {
		// No live subscription: report ThreadNotFound so wait falls back to the
		// initial status path.
		return current, nil, nil, ErrThreadNotFound
	}
	return current, ch, func() {}, nil
}

// collabTurn builds a TurnContext with collab features enabled and a thread-spawn
// session source so the executors can derive a child depth.
func collabTurn(t *testing.T) *TurnContext {
	t.Helper()
	feats := features.NewFeaturesWithDefaults()
	feats.SetEnabled(features.FeatureCollab, true)
	tc := newTestTurn("/work")
	tc.Features = &feats
	tc.SessionSource = rollout.NewCliSource()
	tc.Environments = nil
	return tc
}

func collabPayload(args string) tools.ToolPayload { return tools.FunctionPayload(args) }

func findCollabExecutor(t *testing.T, deps BuiltinToolDeps, suffix string) collabExecutor {
	t.Helper()
	for _, ce := range collabExecutorsWithDeps(deps) {
		if ce.name.Name == suffix {
			return ce
		}
	}
	t.Fatalf("collab executor %q not found", suffix)
	return collabExecutor{}
}

func TestCollabSpawnAgentHandleSpawnsAndReturnsID(t *testing.T) {
	sess, events := newTestSession(t)
	fake := newFakeCollabControl()
	childID := protocol.NewThreadID("11111111-1111-1111-1111-111111111111")
	nick := "Euclid"
	fake.spawnResult = CollabSpawnResult{
		ThreadID: childID,
		Metadata: CollabAgentMetadata{AgentNickname: &nick},
		Status:   protocol.AgentStatus{Kind: protocol.AgentStatusRunning},
	}

	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "spawn_agent")
	out, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session:  sess,
		Turn:     collabTurn(t),
		CallID:   "call-1",
		ToolName: ex.Name(),
		Payload:  collabPayload(`{"message":"investigate the bug"}`),
	})
	if err != nil {
		t.Fatalf("spawn handle: %v", err)
	}
	if fake.spawnCalls != 1 {
		t.Fatalf("spawn calls = %d, want 1", fake.spawnCalls)
	}
	text := toolOutputText(out, "call-1", collabPayload(`{}`))
	var got struct {
		AgentID  string  `json:"agent_id"`
		Nickname *string `json:"nickname"`
	}
	if uerr := json.Unmarshal([]byte(text), &got); uerr != nil {
		t.Fatalf("unmarshal spawn result %q: %v", text, uerr)
	}
	if got.AgentID != childID.String() {
		t.Fatalf("agent_id = %q, want %q", got.AgentID, childID.String())
	}
	if got.Nickname == nil || *got.Nickname != nick {
		t.Fatalf("nickname = %v, want %q", got.Nickname, nick)
	}

	kinds := drainEventKinds(events)
	if !hasEventKind(kinds, protocol.EventMsgKindCollabAgentSpawnBegin) ||
		!hasEventKind(kinds, protocol.EventMsgKindCollabAgentSpawnEnd) {
		t.Fatalf("missing spawn begin/end events, got %v", kinds)
	}
}

func TestCollabSpawnAgentThreadsParentRolloutPathForFork(t *testing.T) {
	sess, _ := newTestSession(t)
	parentPath := "/rollouts/parent.jsonl"
	sess.rolloutPath = &parentPath
	fake := newFakeCollabControl()
	fake.spawnResult = CollabSpawnResult{
		ThreadID: protocol.NewThreadID("33333333-3333-3333-3333-333333333333"),
		Status:   protocol.AgentStatus{Kind: protocol.AgentStatusRunning},
	}

	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "spawn_agent")
	if _, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session:  sess,
		Turn:     collabTurn(t),
		CallID:   "call-fork",
		ToolName: ex.Name(),
		Payload:  collabPayload(`{"message":"continue","fork_context":true}`),
	}); err != nil {
		t.Fatalf("fork spawn handle: %v", err)
	}
	if !fake.lastSpawn.ForkContext {
		t.Fatalf("ForkContext = false, want true")
	}
	if fake.lastSpawn.ParentRolloutPath == nil || *fake.lastSpawn.ParentRolloutPath != parentPath {
		t.Fatalf("ParentRolloutPath = %v, want %q", fake.lastSpawn.ParentRolloutPath, parentPath)
	}
}

func TestCollabSpawnAgentRejectsBothMessageAndItems(t *testing.T) {
	sess, _ := newTestSession(t)
	fake := newFakeCollabControl()
	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "spawn_agent")
	_, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: collabTurn(t), CallID: "c", ToolName: ex.Name(),
		Payload: collabPayload(`{"message":"x","items":[{"type":"text","text":"y"}]}`),
	})
	if err == nil {
		t.Fatalf("expected error when both message and items are provided")
	}
	if fake.spawnCalls != 0 {
		t.Fatalf("spawn should not be called on invalid input")
	}
}

func TestCollabSendInputRoutesAndReturnsSubmissionID(t *testing.T) {
	sess, events := newTestSession(t)
	fake := newFakeCollabControl()
	target := protocol.NewThreadID("22222222-2222-2222-2222-222222222222")
	fake.sendSubID = "sub-99"
	fake.statuses[target.String()] = protocol.AgentStatus{Kind: protocol.AgentStatusRunning}

	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "send_input")
	out, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: collabTurn(t), CallID: "c", ToolName: ex.Name(),
		Payload: collabPayload(`{"target":"` + target.String() + `","message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("send_input handle: %v", err)
	}
	if fake.sendCalls != 1 || fake.lastSend.threadID != target {
		t.Fatalf("send not routed to target: calls=%d thread=%v", fake.sendCalls, fake.lastSend.threadID)
	}
	text := toolOutputText(out, "c", collabPayload(`{}`))
	var got struct {
		SubmissionID string `json:"submission_id"`
	}
	if uerr := json.Unmarshal([]byte(text), &got); uerr != nil {
		t.Fatalf("unmarshal send result %q: %v", text, uerr)
	}
	if got.SubmissionID != "sub-99" {
		t.Fatalf("submission_id = %q, want sub-99", got.SubmissionID)
	}
	kinds := drainEventKinds(events)
	if !hasEventKind(kinds, protocol.EventMsgKindCollabAgentInteractionBegin) ||
		!hasEventKind(kinds, protocol.EventMsgKindCollabAgentInteractionEnd) {
		t.Fatalf("missing interaction begin/end events, got %v", kinds)
	}
}

func TestCollabSendInputInterruptsWhenRequested(t *testing.T) {
	sess, _ := newTestSession(t)
	fake := newFakeCollabControl()
	target := protocol.NewThreadID("22222222-2222-2222-2222-222222222222")
	fake.statuses[target.String()] = protocol.AgentStatus{Kind: protocol.AgentStatusRunning}

	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "send_input")
	_, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: collabTurn(t), CallID: "c", ToolName: ex.Name(),
		Payload: collabPayload(`{"target":"` + target.String() + `","message":"stop","interrupt":true}`),
	})
	if err != nil {
		t.Fatalf("send_input interrupt handle: %v", err)
	}
	if fake.interruptCalls != 1 {
		t.Fatalf("interrupt calls = %d, want 1", fake.interruptCalls)
	}
}

func TestCollabWaitAgentReturnsFinalStatusKeyedByTarget(t *testing.T) {
	sess, events := newTestSession(t)
	fake := newFakeCollabControl()
	target := protocol.NewThreadID("33333333-3333-3333-3333-333333333333")
	final := "all done"
	fake.statuses[target.String()] = protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, CompletedMessage: &final}
	// No live subscription -> SubscribeStatus returns ThreadNotFound, but the
	// initial GetStatus already reports a final status which wait reports.
	fake.subscribeCh[target.String()] = func() chan protocol.AgentStatus {
		ch := make(chan protocol.AgentStatus, 1)
		ch <- protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, CompletedMessage: &final}
		return ch
	}()

	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "wait_agent")
	out, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: collabTurn(t), CallID: "c", ToolName: ex.Name(),
		Payload: collabPayload(`{"targets":["` + target.String() + `"],"timeout_ms":10000}`),
	})
	if err != nil {
		t.Fatalf("wait handle: %v", err)
	}
	text := toolOutputText(out, "c", collabPayload(`{}`))
	var got struct {
		Status   map[string]protocol.AgentStatus `json:"status"`
		TimedOut bool                            `json:"timed_out"`
	}
	if uerr := json.Unmarshal([]byte(text), &got); uerr != nil {
		t.Fatalf("unmarshal wait result %q: %v", text, uerr)
	}
	if got.TimedOut {
		t.Fatalf("wait should not have timed out: %q", text)
	}
	st, ok := got.Status[target.String()]
	if !ok {
		t.Fatalf("status missing target key: %q", text)
	}
	if st.Kind != protocol.AgentStatusCompleted || st.CompletedMessage == nil || *st.CompletedMessage != final {
		t.Fatalf("status = %+v, want completed(%q)", st, final)
	}
	kinds := drainEventKinds(events)
	if !hasEventKind(kinds, protocol.EventMsgKindCollabWaitingBegin) ||
		!hasEventKind(kinds, protocol.EventMsgKindCollabWaitingEnd) {
		t.Fatalf("missing waiting begin/end events, got %v", kinds)
	}
}

func TestCollabWaitAgentRejectsEmptyTargets(t *testing.T) {
	sess, _ := newTestSession(t)
	fake := newFakeCollabControl()
	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "wait_agent")
	_, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: collabTurn(t), CallID: "c", ToolName: ex.Name(),
		Payload: collabPayload(`{"targets":[]}`),
	})
	if err == nil {
		t.Fatalf("expected error for empty targets")
	}
}

func TestCollabCloseAgentReportsPreviousStatus(t *testing.T) {
	sess, events := newTestSession(t)
	fake := newFakeCollabControl()
	target := protocol.NewThreadID("44444444-4444-4444-4444-444444444444")
	fake.statuses[target.String()] = protocol.AgentStatus{Kind: protocol.AgentStatusRunning}
	fake.metadata[target.String()] = CollabAgentMetadata{}

	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "close_agent")
	out, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: collabTurn(t), CallID: "c", ToolName: ex.Name(),
		Payload: collabPayload(`{"target":"` + target.String() + `"}`),
	})
	if err != nil {
		t.Fatalf("close handle: %v", err)
	}
	if fake.closeCalls != 1 || fake.closedThread == nil || *fake.closedThread != target {
		t.Fatalf("close not routed to target: calls=%d thread=%v", fake.closeCalls, fake.closedThread)
	}
	text := toolOutputText(out, "c", collabPayload(`{}`))
	var got struct {
		PreviousStatus protocol.AgentStatus `json:"previous_status"`
	}
	if uerr := json.Unmarshal([]byte(text), &got); uerr != nil {
		t.Fatalf("unmarshal close result %q: %v", text, uerr)
	}
	if got.PreviousStatus.Kind != protocol.AgentStatusRunning {
		t.Fatalf("previous_status = %+v, want running", got.PreviousStatus)
	}
	kinds := drainEventKinds(events)
	if !hasEventKind(kinds, protocol.EventMsgKindCollabCloseBegin) ||
		!hasEventKind(kinds, protocol.EventMsgKindCollabCloseEnd) {
		t.Fatalf("missing close begin/end events, got %v", kinds)
	}
}

func TestCollabResumeAgentReopensClosedAgent(t *testing.T) {
	sess, events := newTestSession(t)
	fake := newFakeCollabControl()
	target := protocol.NewThreadID("55555555-5555-5555-5555-555555555555")
	// Initially not found; after resume the control reports running.
	resumed := protocol.AgentStatus{Kind: protocol.AgentStatusRunning}
	fake.statuses[target.String()] = protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}
	fake.resumeErr = nil

	// After ResumeAgent succeeds, status should reflect the live agent. Mutate the
	// fake's status map inside ResumeAgent by pre-seeding a post-resume value.
	postResume := map[string]protocol.AgentStatus{target.String(): resumed}

	deps := BuiltinToolDeps{Collab: &resumingFakeControl{fakeCollabControl: fake, postResume: postResume}}
	ex := findCollabExecutor(t, deps, "resume_agent")
	out, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: collabTurn(t), CallID: "c", ToolName: ex.Name(),
		Payload: collabPayload(`{"id":"` + target.String() + `"}`),
	})
	if err != nil {
		t.Fatalf("resume handle: %v", err)
	}
	if fake.resumeCalls != 1 || fake.resumeThread == nil || *fake.resumeThread != target {
		t.Fatalf("resume not routed to target: calls=%d thread=%v", fake.resumeCalls, fake.resumeThread)
	}
	text := toolOutputText(out, "c", collabPayload(`{}`))
	var got struct {
		Status protocol.AgentStatus `json:"status"`
	}
	if uerr := json.Unmarshal([]byte(text), &got); uerr != nil {
		t.Fatalf("unmarshal resume result %q: %v", text, uerr)
	}
	if got.Status.Kind != protocol.AgentStatusRunning {
		t.Fatalf("status = %+v, want running", got.Status)
	}
	kinds := drainEventKinds(events)
	if !hasEventKind(kinds, protocol.EventMsgKindCollabResumeBegin) ||
		!hasEventKind(kinds, protocol.EventMsgKindCollabResumeEnd) {
		t.Fatalf("missing resume begin/end events, got %v", kinds)
	}
}

// resumingFakeControl wraps fakeCollabControl to flip the status map to a
// post-resume value once ResumeAgent has been called, mirroring a closed agent
// coming back online.
type resumingFakeControl struct {
	*fakeCollabControl
	postResume map[string]protocol.AgentStatus
}

func (r *resumingFakeControl) ResumeAgent(ctx context.Context, req CollabResumeRequest) error {
	if err := r.fakeCollabControl.ResumeAgent(ctx, req); err != nil {
		return err
	}
	r.fakeCollabControl.mu.Lock()
	for k, v := range r.postResume {
		r.fakeCollabControl.statuses[k] = v
	}
	r.fakeCollabControl.mu.Unlock()
	return nil
}

// collabToolCallItems collects the CollabAgentToolCall lifecycle items among
// the buffered item_started/item_completed events, in order.
func collabToolCallItems(events <-chan protocol.Event) []protocol.CollabAgentToolCallItem {
	var out []protocol.CollabAgentToolCallItem
	for {
		select {
		case ev := <-events:
			var item *protocol.TurnItem
			switch ev.Msg.Type {
			case protocol.EventMsgKindItemStarted:
				item = &ev.Msg.ItemStarted.Item
			case protocol.EventMsgKindItemCompleted:
				item = &ev.Msg.ItemCompleted.Item
			}
			if item != nil && item.Type == protocol.TurnItemKindCollabAgentToolCall {
				out = append(out, *item.CollabAgentToolCall)
			}
		default:
			return out
		}
	}
}

// TestCollabWaitAgentUpcastsSubAgentFailure asserts the 0.147 wait semantics
// (spec 50 D0.7): the CollabAgentToolCall item starts in_progress and completes
// as failed when a target ended Errored or NotFound, and as completed otherwise —
// a failed sub-agent is no longer an empty success for the parent.
func TestCollabWaitAgentUpcastsSubAgentFailure(t *testing.T) {
	cases := []struct {
		name   string
		status *protocol.AgentStatus // nil = unknown target (NotFound)
		want   protocol.CollabAgentToolCallStatus
	}{
		{"errored target", &protocol.AgentStatus{Kind: protocol.AgentStatusErrored, ErroredMessage: "boom"}, protocol.CollabAgentToolCallStatusFailed},
		{"not found target", nil, protocol.CollabAgentToolCallStatusFailed},
		{"completed target", &protocol.AgentStatus{Kind: protocol.AgentStatusCompleted}, protocol.CollabAgentToolCallStatusCompleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess, events := newTestSession(t)
			fake := newFakeCollabControl()
			target := protocol.NewThreadID("44444444-4444-4444-4444-444444444444")
			if tc.status != nil {
				fake.statuses[target.String()] = *tc.status
				ch := make(chan protocol.AgentStatus, 1)
				ch <- *tc.status
				fake.subscribeCh[target.String()] = ch
			}
			deps := BuiltinToolDeps{Collab: fake}
			ex := findCollabExecutor(t, deps, "wait_agent")
			if _, err := ex.Handle(context.Background(), &ToolHandlerContext{
				Session: sess, Turn: collabTurn(t), CallID: "wait-1", ToolName: ex.Name(),
				Payload: collabPayload(`{"targets":["` + target.String() + `"],"timeout_ms":10000}`),
			}); err != nil {
				t.Fatalf("wait handle: %v", err)
			}
			items := collabToolCallItems(events)
			if len(items) != 2 {
				t.Fatalf("collab tool-call items = %d, want started+completed", len(items))
			}
			if items[0].Status != protocol.CollabAgentToolCallStatusInProgress || items[0].Tool != protocol.CollabAgentToolWait || items[0].ID != "wait-1" {
				t.Fatalf("started item = %+v", items[0])
			}
			if items[1].Status != tc.want {
				t.Fatalf("completed status = %q, want %q (states %v)", items[1].Status, tc.want, items[1].AgentsStates)
			}
			if _, ok := items[1].AgentsStates[target.String()]; !ok {
				t.Fatalf("completed item should carry the target state, got %v", items[1].AgentsStates)
			}
		})
	}
}

// TestCollabWaitAgentInterruptedBySteer asserts a steer for the waiting parent
// ends wait_agent early (0.147 WaitOutcome::Steered; spec 50 D0.2): the wait
// returns without a timeout and without statuses so the parent can react to
// the new input.
func TestCollabWaitAgentInterruptedBySteer(t *testing.T) {
	sess, _ := newTestSession(t)
	fake := newFakeCollabControl()
	target := protocol.NewThreadID("55555555-5555-5555-5555-555555555555")
	// A live target that never reaches a final status.
	fake.statuses[target.String()] = protocol.AgentStatus{Kind: protocol.AgentStatusRunning}
	fake.subscribeCh[target.String()] = make(chan protocol.AgentStatus, 1)

	deps := BuiltinToolDeps{Collab: fake}
	ex := findCollabExecutor(t, deps, "wait_agent")
	type result struct {
		out tools.ToolOutput
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := ex.Handle(context.Background(), &ToolHandlerContext{
			Session: sess, Turn: collabTurn(t), CallID: "w", ToolName: ex.Name(),
			Payload: collabPayload(`{"targets":["` + target.String() + `"],"timeout_ms":30000}`),
		})
		done <- result{out, err}
	}()

	// Give the wait a moment to subscribe, then steer the parent.
	time.Sleep(50 * time.Millisecond)
	sess.InputQueue().ExtendPendingInputAndAcceptMailboxDelivery(sess.ActiveTurn().State, []turnInput{{UserContent: []protocol.UserInput{{Type: protocol.UserInputKindText, Text: "change of plan"}}}})

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("wait handle: %v", res.err)
		}
		text := toolOutputText(res.out, "w", collabPayload(`{}`))
		var got struct {
			Status   map[string]protocol.AgentStatus `json:"status"`
			TimedOut bool                            `json:"timed_out"`
		}
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatalf("unmarshal wait result %q: %v", text, err)
		}
		if got.TimedOut || len(got.Status) != 0 {
			t.Fatalf("steer-interrupted wait = %q, want no timeout and no statuses", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait_agent was not interrupted by the steer")
	}
}
