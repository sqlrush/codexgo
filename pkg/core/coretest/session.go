// Package coretest provides fixtures for testing tool executors and other
// components layered on core (core/localexec, host assemblies) that need a
// live [core.Session] and its event stream. It builds sessions through the
// public [core.Spawn] entry point with stub services, so tests exercise the
// same session shape production uses without reaching into core internals.
package coretest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// EventTimeout bounds how long [Fixture.RecvEvent] waits for the next event.
const EventTimeout = time.Second

// Fixture is a spawned session plus helpers to observe its events.
type Fixture struct {
	Codex   *core.Codex
	Session *core.Session
	// Events receives every event the session emits after SessionConfigured
	// (a forwarding goroutine pumps Codex.NextEvent into it until shutdown).
	Events <-chan protocol.Event

	events chan protocol.Event
}

// stubModelClient satisfies core.ModelClient without a network; Stream fails
// loudly so a test that accidentally runs a turn notices.
type stubModelClient struct{}

func (stubModelClient) Stream(context.Context, core.Prompt) (<-chan api.ResponseEvent, error) {
	return nil, errors.New("coretest: stub model client cannot stream")
}
func (stubModelClient) ContextWindow() *int64 { return nil }
func (stubModelClient) ModelSlug() string     { return "coretest-stub" }

// NewSession spawns a session with a stub model client and the given executors
// (an empty list yields an empty router), drains the SessionConfigured event,
// and returns the fixture. The session is shut down at test cleanup.
func NewSession(t *testing.T, executors ...core.ToolExecutor) *Fixture {
	t.Helper()
	router, err := core.NewDefaultToolRouter(executors...)
	if err != nil {
		t.Fatalf("coretest: build router: %v", err)
	}
	ok, err := core.Spawn(context.Background(), core.CodexSpawnArgs{
		ThreadID: protocol.NewThreadID("00000000-0000-4000-8000-00000000c0de"),
		Services: core.SessionServices{
			ModelClient: stubModelClient{},
			ToolRouter:  router,
		},
	})
	if err != nil {
		t.Fatalf("coretest: spawn session: %v", err)
	}
	f := &Fixture{Codex: ok.Codex, Session: ok.Codex.Session()}
	// Approval waiters live on the active turn; install one so executor tests
	// can drive RequestCommandApproval/NotifyApproval without running a turn.
	f.Session.InstallActiveTurnForTesting()
	pumpCtx, stopPump := context.WithCancel(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), EventTimeout)
		defer cancel()
		_ = f.Codex.Shutdown(ctx)
		stopPump()
	})
	firstCtx, cancelFirst := context.WithTimeout(context.Background(), EventTimeout)
	defer cancelFirst()
	if ev, err := f.Codex.NextEvent(firstCtx); err != nil || ev.Msg.Type != protocol.EventMsgKindSessionConfigured {
		t.Fatalf("coretest: first event = %s (%v), want session_configured", ev.Msg.Type, err)
	}
	f.events = make(chan protocol.Event, 256)
	f.Events = f.events
	go f.pump(pumpCtx)
	return f
}

// pump forwards session events into the fixture channel until ctx is done or
// the session's event stream closes.
func (f *Fixture) pump(ctx context.Context) {
	defer close(f.events)
	for {
		ev, err := f.Codex.NextEvent(ctx)
		if err != nil {
			return
		}
		select {
		case f.events <- ev:
		case <-ctx.Done():
			return
		}
	}
}

// RecvEvent returns the next event or fails the test after [EventTimeout].
func (f *Fixture) RecvEvent(t *testing.T) protocol.Event {
	t.Helper()
	return RecvEvent(t, f.Events)
}

// RecvEvent returns the next event from events or fails the test after
// [EventTimeout].
func RecvEvent(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("coretest: event stream closed")
		}
		return ev
	case <-time.After(EventTimeout):
		t.Fatal("coretest: timed out waiting for event")
		return protocol.Event{}
	}
}

// DrainEventKinds returns the kinds of every event currently buffered.
func (f *Fixture) DrainEventKinds() []protocol.EventMsgKind {
	var kinds []protocol.EventMsgKind
	for {
		select {
		case ev, ok := <-f.Events:
			if !ok {
				return kinds
			}
			kinds = append(kinds, ev.Msg.Type)
		default:
			return kinds
		}
	}
}

// NewTurn builds a minimal TurnContext rooted at cwd, matching the shape the
// core tool tests use.
func NewTurn(cwd string) *core.TurnContext {
	return &core.TurnContext{
		SubID: "turn-1",
		Cwd:   cwd,
		CollaborationMode: protocol.CollaborationMode{
			Mode: protocol.ModeKind("default"),
		},
	}
}
