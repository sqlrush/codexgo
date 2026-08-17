package appserver

import (
	"context"
	"sync"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// eventForwarder pumps a thread's [protocol.Event] stream out to a client as
// `codex/event` notifications. It is the reduced port of the Rust per-thread
// listener task (ensure_listener_task_running): one goroutine per thread that
// reads next_event in a loop and forwards each event, breaking on error or when
// the thread loop terminates.
//
// In addition to forwarding, the forwarder resolves pending turn/interrupt
// requests when a TurnAborted event arrives, mirroring the Rust pattern where an
// interrupt's JSON-RPC response is deferred until the turn actually aborts.
type eventForwarder struct {
	thread *core.CodexThread
	sink   OutgoingSink

	// mu guards pendingInterrupts.
	mu sync.Mutex
	// pendingInterrupts maps a turn id to the interrupt request ids waiting for
	// that turn to abort.
	pendingInterrupts map[string][]appserverproto.RequestId

	// done is closed when the forwarding goroutine exits.
	done chan struct{}
	// cancel cancels the forwarding loop. It is created at construction so it can
	// be read by Stop without racing the Run goroutine.
	cancel context.CancelFunc
	// ctx is the forwarding loop's context, a child of the parent passed to Run.
	ctx context.Context
}

// newEventForwarder constructs a forwarder for thread, delivering events through
// sink. The forwarding loop's lifetime is scoped to parent; Call
// [eventForwarder.Run] to start it (typically in a goroutine).
func newEventForwarder(parent context.Context, thread *core.CodexThread, sink OutgoingSink) *eventForwarder {
	ctx, cancel := context.WithCancel(parent)
	return &eventForwarder{
		thread:            thread,
		sink:              sink,
		pendingInterrupts: make(map[string][]appserverproto.RequestId),
		done:              make(chan struct{}),
		ctx:               ctx,
		cancel:            cancel,
	}
}

// registerInterrupt records that requestID is awaiting the abort of turnID. When
// the matching TurnAborted event is forwarded, the request is answered with an
// empty TurnInterruptResponse.
func (f *eventForwarder) registerInterrupt(turnID string, requestID appserverproto.RequestId) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pendingInterrupts[turnID] = append(f.pendingInterrupts[turnID], requestID)
}

// unregisterInterrupt removes a previously registered interrupt waiter, used
// when the interrupt submission itself fails so no waiter is leaked.
func (f *eventForwarder) unregisterInterrupt(turnID string, requestID appserverproto.RequestId) {
	f.mu.Lock()
	defer f.mu.Unlock()
	waiters := f.pendingInterrupts[turnID]
	kept := waiters[:0]
	for _, id := range waiters {
		if id != requestID {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		delete(f.pendingInterrupts, turnID)
	} else {
		f.pendingInterrupts[turnID] = kept
	}
}

// Run drives the forwarding loop until the thread loop terminates or the
// forwarder's context is cancelled. It is intended to run in its own goroutine.
// The done channel is closed on return.
func (f *eventForwarder) Run() {
	defer close(f.done)
	defer f.cancel()

	for {
		ev, err := f.thread.NextEvent(f.ctx)
		if err != nil {
			// The thread loop terminated (ErrInternalAgentDied) or the context was
			// cancelled. Either way, stop forwarding.
			return
		}

		// Forward the raw event to the client. A send failure means the peer is
		// gone, so stop.
		if sendErr := sendCodexEvent(f.sink, ev); sendErr != nil {
			return
		}

		f.maybeResolveInterrupt(ev)

		// ShutdownComplete is the terminal event; the loop will also return on the
		// next NextEvent, but stop eagerly to release the goroutine promptly.
		if ev.Msg.Type == protocol.EventMsgKindShutdownComplete {
			return
		}
	}
}

// maybeResolveInterrupt answers any pending interrupt requests for the aborted
// turn. The turn id is the event's correlation id (the submission id of the
// UserInput that started the turn).
func (f *eventForwarder) maybeResolveInterrupt(ev protocol.Event) {
	if ev.Msg.Type != protocol.EventMsgKindTurnAborted {
		return
	}
	f.mu.Lock()
	waiters := f.pendingInterrupts[ev.ID]
	delete(f.pendingInterrupts, ev.ID)
	f.mu.Unlock()

	for _, id := range waiters {
		// Best-effort: a send failure here means the peer is gone; the main loop
		// will observe it on its next send.
		_ = sendResponse(f.sink, id, appserverproto.TurnInterruptResponse{})
	}
}

// Stop cancels the forwarding loop and waits for it to exit. It is safe to call
// multiple times: context.CancelFunc is idempotent.
func (f *eventForwarder) Stop() {
	f.cancel()
	<-f.done
}

// Done returns a channel closed when the forwarding loop exits.
func (f *eventForwarder) Done() <-chan struct{} { return f.done }
