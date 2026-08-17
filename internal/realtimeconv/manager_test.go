package realtimeconv

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// waitFor polls cond until true or the deadline elapses, failing the test on
// timeout. It avoids fixed sleeps in concurrency tests.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

func TestManagerLifecycle(t *testing.T) {
	m := NewManager()
	if m.RunningState() {
		t.Fatalf("new manager should not be running")
	}

	conn := newFakeConn(newFakeEvents())
	out, err := m.Start(context.Background(), conn, SessionKindV2)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !m.RunningState() {
		t.Fatalf("manager should be running after start")
	}
	if !m.IsRunningV2() {
		t.Fatalf("expected V2 session")
	}
	if !out.Active.load() {
		t.Fatalf("active flag should be true")
	}

	m.Shutdown()
	if m.RunningState() {
		t.Fatalf("manager should be stopped after shutdown")
	}
	if out.Active.load() {
		t.Fatalf("active flag should be false after shutdown")
	}
	// Events channel should be closed.
	waitFor(t, func() bool {
		select {
		case _, ok := <-out.Events:
			return !ok
		default:
			return false
		}
	}, "events channel close")
}

func TestManagerInputsWhenStopped(t *testing.T) {
	m := NewManager()
	if err := m.AudioIn(protocol.RealtimeAudioFrame{}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("AudioIn err=%v, want ErrNotRunning", err)
	}
	if err := m.TextIn(context.Background(), "hi"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("TextIn err=%v, want ErrNotRunning", err)
	}
	if err := m.HandoffOut(context.Background(), "x"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("HandoffOut err=%v, want ErrNotRunning", err)
	}
	// HandoffComplete and ClearActiveHandoff are no-ops when stopped.
	if err := m.HandoffComplete(context.Background()); err != nil {
		t.Fatalf("HandoffComplete err=%v", err)
	}
	m.ClearActiveHandoff()
	if _, ok := m.ActiveHandoffID(); ok {
		t.Fatalf("expected no active handoff when stopped")
	}
}

func TestManagerTextInPrefixesV2(t *testing.T) {
	m := NewManager()
	conn := newFakeConn(newFakeEvents())
	if _, err := m.Start(context.Background(), conn, SessionKindV2); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Shutdown()

	if err := m.TextIn(context.Background(), "hello"); err != nil {
		t.Fatalf("TextIn: %v", err)
	}
	waitFor(t, func() bool {
		for _, msg := range conn.writer.messages() {
			if msg.kind == sentItem && msg.text == "[USER] hello" {
				return true
			}
		}
		return false
	}, "prefixed user text forwarded")
}

func TestManagerAudioInForwards(t *testing.T) {
	m := NewManager()
	conn := newFakeConn(newFakeEvents())
	if _, err := m.Start(context.Background(), conn, SessionKindV1); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Shutdown()

	frame := protocol.RealtimeAudioFrame{Data: "AAAA", SampleRate: 24000, NumChannels: 1}
	if err := m.AudioIn(frame); err != nil {
		t.Fatalf("AudioIn: %v", err)
	}
	waitFor(t, func() bool {
		return conn.writer.countOf(sentAudio) == 1
	}, "audio frame forwarded")
}

func TestManagerHandoffFlowV2(t *testing.T) {
	m := NewManager()
	// Script a handoff request so the loop records an active handoff.
	events := newFakeEvents(serverScript{event: &Event{
		Kind:    EventKindHandoffRequested,
		Handoff: &HandoffRequested{HandoffID: "h1", InputTranscript: "task"},
	}})
	conn := newFakeConn(events)
	if _, err := m.Start(context.Background(), conn, SessionKindV2); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Shutdown()

	waitFor(t, func() bool {
		id, ok := m.ActiveHandoffID()
		return ok && id == "h1"
	}, "active handoff recorded")

	// Progress update should be prefixed and forwarded as a conversation item.
	if err := m.HandoffOut(context.Background(), "working"); err != nil {
		t.Fatalf("HandoffOut: %v", err)
	}
	waitFor(t, func() bool {
		for _, msg := range conn.writer.messages() {
			if msg.kind == sentItem && msg.text == "[BACKEND] working" {
				return true
			}
		}
		return false
	}, "handoff progress forwarded")

	// Completion sends the ack and a response.create.
	if err := m.HandoffComplete(context.Background()); err != nil {
		t.Fatalf("HandoffComplete: %v", err)
	}
	waitFor(t, func() bool {
		return conn.writer.countOf(sentFnOut) == 1 && conn.writer.countOf(sentCreate) == 1
	}, "handoff completion ack and create")

	m.ClearActiveHandoff()
	waitFor(t, func() bool {
		_, ok := m.ActiveHandoffID()
		return !ok
	}, "handoff cleared")
}

func TestManagerFinishIfActive(t *testing.T) {
	m := NewManager()
	conn := newFakeConn(newFakeEvents())
	out, err := m.Start(context.Background(), conn, SessionKindV1)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// A stale active flag must not stop the current conversation.
	stale := newActiveFlag()
	m.FinishIfActive(stale)
	if !m.RunningState() {
		t.Fatalf("stale finish should not stop conversation")
	}

	m.FinishIfActive(out.Active)
	if m.RunningState() {
		t.Fatalf("matching finish should stop conversation")
	}
}

func TestManagerStartReplacesPrevious(t *testing.T) {
	m := NewManager()
	first := newFakeConn(newFakeEvents())
	out1, err := m.Start(context.Background(), first, SessionKindV1)
	if err != nil {
		t.Fatalf("start1: %v", err)
	}

	second := newFakeConn(newFakeEvents())
	if _, err := m.Start(context.Background(), second, SessionKindV2); err != nil {
		t.Fatalf("start2: %v", err)
	}
	// First conversation should be aborted.
	if out1.Active.load() {
		t.Fatalf("first conversation should be inactive after replacement")
	}
	if !m.IsRunningV2() {
		t.Fatalf("expected the V2 replacement to be running")
	}
	m.Shutdown()
}

func TestManagerNilConnection(t *testing.T) {
	m := NewManager()
	if _, err := m.Start(context.Background(), nil, SessionKindV1); err == nil {
		t.Fatalf("expected error for nil connection")
	}
}

func TestLoopStopsOnServerError(t *testing.T) {
	m := NewManager()
	events := newFakeEvents(
		serverScript{event: &Event{Kind: EventKindOutputTranscriptDelta, TranscriptDelta: &TranscriptDelta{Delta: "hi"}}},
		serverScript{event: ptrEvent(NewError("stream broke"))},
	)
	conn := newFakeConn(events)
	out, err := m.Start(context.Background(), conn, SessionKindV2)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Shutdown()

	// The loop should forward both events then exit; the error event arrives last.
	var sawError bool
	for ev := range out.Events {
		if ev.IsError() && ev.ErrorMessage == "stream broke" {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected to observe the error event before close")
	}
}

func ptrEvent(e Event) *Event { return &e }
