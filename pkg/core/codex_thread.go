package core

import (
	"context"
	"sync"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// CodexThread is the per-thread wrapper around a live [Codex] handle. It is a
// reduced, faithful port of the Rust `codex_thread::CodexThread`: the bidirectional
// conduit for the stream of [protocol.Op] submissions and [protocol.Event]
// values that compose a single thread (formerly "conversation").
//
// A CodexThread is created once per spawn by the [ThreadManager] and held in the
// manager's in-memory map. It caches the immutable [protocol.SessionConfiguredEvent]
// observed at startup, the rollout path (when the backing store is
// filesystem-based), and the [rollout.SessionSource] that classifies its origin.
//
// STUB: the Rust CodexThread additionally exposes goal-runtime application,
// steering, MCP resource/tool calls, out-of-band elicitation accounting, app
// server client info, and memory-mode persistence. Those flows are owned by the
// area agents that port goals/multi_agents/mcp/apps and are intentionally
// omitted from this turn-running subset.
type CodexThread struct {
	// codex is the live engine handle that drives this thread's SQ/EQ loop.
	codex *Codex
	// sessionSource classifies the origin of the thread (cli/exec/mcp/subagent).
	sessionSource rollout.SessionSource
	// sessionConfigured is the immutable startup ack captured at spawn time.
	sessionConfigured protocol.SessionConfiguredEvent
	// rolloutPath is the local rollout JSONL path, when filesystem-backed.
	rolloutPath *string

	// oobMu guards outOfBandElicitationCount.
	oobMu sync.Mutex
	// outOfBandElicitationCount tracks active out-of-band elicitations, mirroring
	// the Rust `out_of_band_elicitation_count`.
	outOfBandElicitationCount uint64
}

// newCodexThread constructs a CodexThread from a freshly spawned [Codex] and the
// startup ack, mirroring the Rust `CodexThread::new`.
func newCodexThread(
	codex *Codex,
	sessionConfigured protocol.SessionConfiguredEvent,
	rolloutPath *string,
	sessionSource rollout.SessionSource,
) *CodexThread {
	return &CodexThread{
		codex:             codex,
		sessionSource:     sessionSource,
		sessionConfigured: sessionConfigured,
		rolloutPath:       clonePtr(rolloutPath),
	}
}

// Codex returns the underlying engine handle (primarily for tests and area
// agents that need direct submission/event access).
func (t *CodexThread) Codex() *Codex { return t.codex }

// ThreadID returns the thread's stable identity.
func (t *CodexThread) ThreadID() protocol.ThreadID { return t.codex.session.threadID }

// SessionSource returns the source classification for this thread.
func (t *CodexThread) SessionSource() rollout.SessionSource { return t.sessionSource }

// Submit wraps op in a fresh submission and enqueues it, returning the generated
// submission id. Mirrors the Rust `CodexThread::submit`.
func (t *CodexThread) Submit(op protocol.Op) (string, error) {
	return t.codex.Submit(op)
}

// SubmitWithID enqueues a pre-built submission. Mirrors the Rust
// `CodexThread::submit_with_id`.
func (t *CodexThread) SubmitWithID(sub protocol.Submission) error {
	return t.codex.SubmitWithID(sub)
}

// NextEvent blocks until the next event is available, honoring ctx. Mirrors the
// Rust `CodexThread::next_event`.
func (t *CodexThread) NextEvent(ctx context.Context) (protocol.Event, error) {
	return t.codex.NextEvent(ctx)
}

// ShutdownAndWait submits a shutdown op and waits for the loop to terminate.
// Mirrors the Rust `CodexThread::shutdown_and_wait`.
func (t *CodexThread) ShutdownAndWait(ctx context.Context) error {
	return t.codex.Shutdown(ctx)
}

// AgentStatus returns the latest known agent status for this thread.
func (t *CodexThread) AgentStatus() protocol.AgentStatus { return t.codex.AgentStatus() }

// SubscribeAgentStatus returns the current status, a channel of subsequent
// transitions, and an unsubscribe function, mirroring the Rust
// `CodexThread`/`Session::subscribe_status` watch handle that `AgentControl`
// uses while waiting on a child agent.
func (t *CodexThread) SubscribeAgentStatus() (protocol.AgentStatus, <-chan protocol.AgentStatus, func()) {
	return t.codex.SubscribeAgentStatus()
}

// RolloutPath returns the local rollout path, when filesystem-backed.
func (t *CodexThread) RolloutPath() *string { return clonePtr(t.rolloutPath) }

// SessionConfigured returns a copy of the startup ack captured at spawn time.
// Mirrors the Rust `CodexThread::session_configured`.
func (t *CodexThread) SessionConfigured() protocol.SessionConfiguredEvent {
	return t.sessionConfigured
}

// IsRunning reports whether the underlying SQ is still open, mirroring the Rust
// `CodexThread::is_running`.
func (t *CodexThread) IsRunning() bool { return !t.codex.isClosed() }

// ConfigSnapshot returns the effective per-thread configuration snapshot taken
// under the session state lock. Mirrors the Rust `CodexThread::config_snapshot`,
// which forwards to `Codex::thread_config_snapshot`.
func (t *CodexThread) ConfigSnapshot() ThreadConfigSnapshot {
	return t.codex.threadConfigSnapshot()
}

// IncrementOutOfBandElicitationCount increments the out-of-band elicitation
// counter and returns the new value, mirroring the Rust
// `increment_out_of_band_elicitation_count`.
//
// STUB: the Rust version also flips the session pause state on the 0->1
// transition; that pause wiring is owned by the apps/elicitation area agent.
func (t *CodexThread) IncrementOutOfBandElicitationCount() uint64 {
	t.oobMu.Lock()
	defer t.oobMu.Unlock()
	t.outOfBandElicitationCount++
	return t.outOfBandElicitationCount
}

// DecrementOutOfBandElicitationCount decrements the out-of-band elicitation
// counter and returns the new value. It returns the unchanged count when already
// zero, mirroring the Rust `decrement_out_of_band_elicitation_count` (which
// instead returns an error in that case; here the count simply floors at zero).
func (t *CodexThread) DecrementOutOfBandElicitationCount() uint64 {
	t.oobMu.Lock()
	defer t.oobMu.Unlock()
	if t.outOfBandElicitationCount > 0 {
		t.outOfBandElicitationCount--
	}
	return t.outOfBandElicitationCount
}

// clonePtr returns a fresh pointer to a copy of *p, or nil. It preserves the
// immutability contract: callers can never mutate the receiver's pointer target.
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
