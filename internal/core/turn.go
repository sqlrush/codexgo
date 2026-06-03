package core

import (
	"context"
	"sync"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// TaskKind classifies the work a running task performs. Mirrors the Rust
// `TaskKind`.
type TaskKind int

const (
	// TaskKindRegular is a normal user-driven turn.
	TaskKindRegular TaskKind = iota
	// TaskKindReview is a code-review turn driven by Op::Review.
	TaskKindReview
	// TaskKindCompact is an inline conversation-compaction turn.
	TaskKindCompact
)

// String renders the task kind for diagnostics.
func (k TaskKind) String() string {
	switch k {
	case TaskKindRegular:
		return "regular"
	case TaskKindReview:
		return "review"
	case TaskKindCompact:
		return "compact"
	default:
		return "unknown"
	}
}

// MailboxDeliveryPhase tracks whether mailbox deliveries should still fold into
// the current turn. Mirrors the Rust `MailboxDeliveryPhase`.
//
// STUB: the multi-agent mailbox machinery is deferred; this enum is provided so
// the turn-state surface matches the Rust shape and area agents can wire it up.
type MailboxDeliveryPhase int

const (
	// MailboxCurrentTurn means incoming mailbox messages can join this turn.
	MailboxCurrentTurn MailboxDeliveryPhase = iota
	// MailboxNextTurn means mailbox messages stay queued for a later turn.
	MailboxNextTurn
)

// RunningTask is the metadata for the at-most-one task running in a session. It
// is a reduced port of the Rust `RunningTask`: it owns the task's cancellation
// token (a child of the session token), a done signal, the task kind, and the
// turn context the task runs against.
//
// STUB: the abort-on-drop join handle, turn-extension data, and turn timer are
// owned by the task-runner area agent and are represented here by Cancel + the
// Done channel.
type RunningTask struct {
	// Kind classifies the running task.
	Kind TaskKind
	// TurnContext is the read-only snapshot the task runs against.
	TurnContext *TurnContext
	// Cancel cancels the task's context (and all of its children), mirroring the
	// tokio CancellationToken.
	Cancel context.CancelFunc
	// ctx is the task's cancellation context (a child of the session context).
	ctx context.Context
	// done is closed exactly once when the task finishes.
	done     chan struct{}
	doneOnce sync.Once
}

// Context returns the task's cancellation context.
func (t *RunningTask) Context() context.Context { return t.ctx }

// Done returns a channel closed when the task completes.
func (t *RunningTask) Done() <-chan struct{} { return t.done }

// markDone closes the done channel exactly once.
func (t *RunningTask) markDone() {
	t.doneOnce.Do(func() { close(t.done) })
}

// ActiveTurn is the currently running turn's metadata. It pairs the optional
// running task with the mutable per-turn state. Mirrors the Rust `ActiveTurn`.
type ActiveTurn struct {
	// Task is the running task, or nil when no task is active yet.
	Task *RunningTask
	// State is the mutable per-turn state (pending approvals, etc.).
	State *TurnState
}

// NewActiveTurn builds an active turn with empty per-turn state.
func NewActiveTurn() *ActiveTurn {
	return &ActiveTurn{State: NewTurnState()}
}

// TurnState is the mutable state for a single turn: the pending-approval map and
// per-turn counters. It is a reduced, thread-safe port of the Rust `TurnState`.
//
// Unlike the Rust version (which is guarded by an outer Arc<Mutex<>>), this type
// is internally synchronized so the turn runner and the submission loop (which
// resolves approvals from incoming ops) can touch it concurrently.
//
// STUB: pending request-permissions, user-input, elicitation, and dynamic-tool
// maps, granted-permission merging, and the mailbox phase are represented but
// only the exec/patch approval map is fully wired for the turn-running subset.
type TurnState struct {
	mu sync.Mutex

	// pendingApprovals maps an approval key (call id) to the channel that
	// delivers the user's [protocol.ReviewDecision] back to the waiting tool.
	pendingApprovals map[string]chan protocol.ReviewDecision

	mailboxPhase MailboxDeliveryPhase

	// toolCalls counts tool invocations made during this turn.
	toolCalls uint64
	// tokenUsageAtTurnStart snapshots the running total when the turn began.
	tokenUsageAtTurnStart protocol.TokenUsage
}

// NewTurnState builds an empty per-turn state.
func NewTurnState() *TurnState {
	return &TurnState{
		pendingApprovals: make(map[string]chan protocol.ReviewDecision),
		mailboxPhase:     MailboxCurrentTurn,
	}
}

// InsertPendingApproval registers a waiter for an approval keyed by id and
// returns the channel that will receive the decision. Any previous waiter for
// the same key is replaced (its channel is returned so the caller can close it).
func (s *TurnState) InsertPendingApproval(key string) (recv <-chan protocol.ReviewDecision, replaced chan protocol.ReviewDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.pendingApprovals[key]; ok {
		replaced = prev
	}
	ch := make(chan protocol.ReviewDecision, 1)
	s.pendingApprovals[key] = ch
	return ch, replaced
}

// ResolvePendingApproval delivers a decision to the waiter for key, returning
// true when a waiter was present. The waiter entry is removed.
func (s *TurnState) ResolvePendingApproval(key string, decision protocol.ReviewDecision) bool {
	s.mu.Lock()
	ch, ok := s.pendingApprovals[key]
	if ok {
		delete(s.pendingApprovals, key)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	ch <- decision
	return true
}

// ClearPendingWaiters drops all pending approval waiters, closing their
// channels so any blocked waiter unblocks. Mirrors the Rust
// `clear_pending_waiters` (approval subset).
func (s *TurnState) ClearPendingWaiters() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, ch := range s.pendingApprovals {
		close(ch)
		delete(s.pendingApprovals, key)
	}
}

// IncToolCalls increments and returns the tool-call counter.
func (s *TurnState) IncToolCalls() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolCalls++
	return s.toolCalls
}

// ToolCalls returns the current tool-call count.
func (s *TurnState) ToolCalls() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.toolCalls
}

// SetTokenUsageAtTurnStart snapshots the running total at turn start.
func (s *TurnState) SetTokenUsageAtTurnStart(u protocol.TokenUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenUsageAtTurnStart = u
}

// TokenUsageAtTurnStart returns the token usage snapshot taken at turn start.
func (s *TurnState) TokenUsageAtTurnStart() protocol.TokenUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenUsageAtTurnStart
}

// SetMailboxDeliveryPhase records the mailbox delivery phase.
func (s *TurnState) SetMailboxDeliveryPhase(phase MailboxDeliveryPhase) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mailboxPhase = phase
}

// AcceptsMailboxDeliveryForCurrentTurn reports whether mailbox messages may
// still fold into the current turn.
func (s *TurnState) AcceptsMailboxDeliveryForCurrentTurn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mailboxPhase == MailboxCurrentTurn
}
