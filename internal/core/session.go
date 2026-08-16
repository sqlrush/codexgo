package core

import (
	"context"
	"sync"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// SessionServices bundles the injected manager dependencies a [Session] uses to
// run turns. It is the Go analogue of the Rust `SessionServices`, reduced to the
// turn-running subset and built entirely from the small DI interfaces declared
// in managers.go. Concrete implementations are supplied by callers through
// [CodexSpawnArgs].
//
// All fields are required unless documented otherwise; nil managers must be
// supplied as no-op implementations rather than left nil.
type SessionServices struct {
	ModelClient     ModelClient
	ToolRouter      ToolRouter
	McpManager      McpManager
	SkillsManager   SkillsManager
	PluginsManager  PluginsManager
	HooksEngine     HooksEngine
	ModelsManager   ModelsManager
	RolloutRecorder RolloutRecorder
}

// Session owns the long-lived, thread-safe state for a single Codex thread and
// the injected services used to run its turns. It is a reduced port of the Rust
// `session::session::Session`.
//
// A session has at most one running task at a time (tracked by activeTurn) and
// can be interrupted by user input. Shared mutable state lives under stateMu;
// the active-turn slot has its own mutex so approval resolution does not block
// on the larger state lock.
type Session struct {
	// threadID is the stable identity for this thread.
	threadID protocol.ThreadID
	// sessionID is the identity shared by the root thread and descendants.
	sessionID protocol.SessionID
	// installationID is the stable installation identifier.
	installationID string

	// rolloutPath is the local rollout path for this thread, when
	// filesystem-backed. It is read-only after construction. The collab spawn
	// handler reads it to thread the parent rollout path into a forked child
	// spawn, mirroring the Rust fork path that snapshots the parent's stored
	// history. nil means the host has no rollout persistence for this thread.
	rolloutPath *string

	// services holds the injected manager dependencies.
	services SessionServices

	// txEvent is the outbound event-queue sender; events are correlated to
	// submissions by their id.
	txEvent chan<- protocol.Event

	// stateMu guards state.
	stateMu sync.Mutex
	state   *SessionState

	// activeTurnMu guards activeTurn.
	activeTurnMu sync.Mutex
	activeTurn   *ActiveTurn

	// agentStatusMu guards agentStatus and notifies watchers.
	agentStatusMu  sync.Mutex
	agentStatus    protocol.AgentStatus
	statusWatchers []chan protocol.AgentStatus

	// ctx is the session-wide cancellation context; per-task contexts are
	// children of it, mirroring the tokio CancellationToken hierarchy.
	ctx    context.Context
	cancel context.CancelFunc

	// nextInternalSubID counts auto-generated internal submission ids.
	nextInternalSubID uint64
}

// ThreadID returns the session's thread identity.
func (s *Session) ThreadID() protocol.ThreadID { return s.threadID }

// SessionID returns the session's shared identity.
func (s *Session) SessionID() protocol.SessionID { return s.sessionID }

// RolloutPath returns the session's local rollout path, when filesystem-backed,
// or nil when the host has no rollout persistence for this thread. It is the
// parent path the collab spawn handler threads into a forked child spawn,
// mirroring the Rust fork path that snapshots the parent's stored history. The
// returned pointer is a copy so callers cannot mutate session state.
func (s *Session) RolloutPath() *string { return clonePtr(s.rolloutPath) }

// Services returns the injected manager dependencies.
func (s *Session) Services() SessionServices { return s.services }

// Context returns the session-wide cancellation context.
func (s *Session) Context() context.Context { return s.ctx }

// WithState runs fn while holding the session state lock and returns its result.
// This is the only sanctioned way to read or mutate [SessionState], mirroring
// the Rust `self.state.lock().await` pattern.
func (s *Session) WithState(fn func(*SessionState)) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	fn(s.state)
}

// CloneConfiguration returns a copy of the current session configuration taken
// under the state lock.
func (s *Session) CloneConfiguration() SessionConfiguration {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.state.Config
}

// UpdateConfiguration applies an immutable settings update to the session
// configuration under the state lock and returns the new configuration.
func (s *Session) UpdateConfiguration(u SessionSettingsUpdate) SessionConfiguration {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.state.Config = s.state.Config.Apply(u)
	return s.state.Config
}

// RecordItems appends items to the shared conversation history.
func (s *Session) RecordItems(items []protocol.ResponseItem) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.state.History.RecordItems(items)
}

// HistoryItems returns a copy of the current conversation history.
func (s *Session) HistoryItems() []protocol.ResponseItem {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.state.History.Items()
}

// SendEvent enqueues an event on the event queue, correlating it with id, and
// derives the next agent status from it. It respects session cancellation so a
// shutdown does not block forever on a full/closed queue.
func (s *Session) SendEvent(id string, msg protocol.EventMsg) {
	ev := protocol.Event{ID: id, Msg: msg}
	s.updateAgentStatusFromEvent(msg)
	select {
	case s.txEvent <- ev:
	case <-s.ctx.Done():
	}
}

// EmitEvent enqueues a pre-built event on the event queue, preserving its
// correlation id. It is the entry point for host components (e.g. the extension
// event sink) that construct a fully-formed [protocol.Event] with the id
// appropriate for the callback they handle. Agent-status derivation and
// cancellation handling match [SendEvent].
func (s *Session) EmitEvent(ev protocol.Event) {
	s.updateAgentStatusFromEvent(ev.Msg)
	select {
	case s.txEvent <- ev:
	case <-s.ctx.Done():
	}
}

// updateAgentStatusFromEvent advances the cached agent status based on an
// emitted event, mirroring the Rust `agent_status_from_event`.
func (s *Session) updateAgentStatusFromEvent(msg protocol.EventMsg) {
	next, ok := agentStatusFromEvent(msg)
	if !ok {
		return
	}
	s.setAgentStatus(next)
}

// AgentStatus returns the latest known agent status.
func (s *Session) AgentStatus() protocol.AgentStatus {
	s.agentStatusMu.Lock()
	defer s.agentStatusMu.Unlock()
	return s.agentStatus
}

// setAgentStatus updates the cached status and notifies any watchers.
func (s *Session) setAgentStatus(status protocol.AgentStatus) {
	s.agentStatusMu.Lock()
	s.agentStatus = status
	watchers := s.statusWatchers
	s.agentStatusMu.Unlock()
	for _, w := range watchers {
		select {
		case w <- status:
		default:
		}
	}
}

// SubscribeAgentStatus returns the current status plus a channel that receives
// every subsequent status transition, and an unsubscribe function the caller
// must invoke when done. It is the Go analogue of the Rust
// `Session::subscribe_status` watch channel: the channel is buffered so a slow
// watcher never blocks the status producer; the latest value is always readable
// via [Session.AgentStatus]. The unsubscribe closes the channel and removes it
// from the watcher set.
func (s *Session) SubscribeAgentStatus() (protocol.AgentStatus, <-chan protocol.AgentStatus, func()) {
	s.agentStatusMu.Lock()
	current := s.agentStatus
	// Buffer generously so bursts of transitions during a turn do not drop the
	// final status before the watcher observes it.
	ch := make(chan protocol.AgentStatus, 16)
	s.statusWatchers = append(s.statusWatchers, ch)
	s.agentStatusMu.Unlock()

	unsubscribe := func() {
		s.agentStatusMu.Lock()
		defer s.agentStatusMu.Unlock()
		for i, w := range s.statusWatchers {
			if w == ch {
				s.statusWatchers = append(s.statusWatchers[:i], s.statusWatchers[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return current, ch, unsubscribe
}

// nextInternalSubmissionID returns a fresh auto-compaction-style internal
// submission id, mirroring the Rust `next_internal_sub_id`.
func (s *Session) nextInternalSubmissionID() string {
	s.agentStatusMu.Lock()
	id := s.nextInternalSubID
	s.nextInternalSubID++
	s.agentStatusMu.Unlock()
	return "auto-compact-" + itoa(id)
}

// ActiveTurn returns the current active turn, or nil when no task is running.
func (s *Session) ActiveTurn() *ActiveTurn {
	s.activeTurnMu.Lock()
	defer s.activeTurnMu.Unlock()
	return s.activeTurn
}

// setActiveTurn installs (or clears with nil) the active turn slot.
func (s *Session) setActiveTurn(at *ActiveTurn) {
	s.activeTurnMu.Lock()
	defer s.activeTurnMu.Unlock()
	s.activeTurn = at
}

// agentStatusFromEvent derives the next agent status from a single emitted
// event, mirroring the Rust `agent_status_from_event`. It returns (status, true)
// when the event affects status and (zero, false) otherwise.
func agentStatusFromEvent(msg protocol.EventMsg) (protocol.AgentStatus, bool) {
	switch msg.Type {
	case protocol.EventMsgKindTurnStarted:
		return protocol.AgentStatus{Kind: protocol.AgentStatusRunning}, true
	case protocol.EventMsgKindTurnComplete:
		var last *string
		if msg.TurnComplete != nil {
			last = msg.TurnComplete.LastAgentMessage
		}
		return protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, CompletedMessage: last}, true
	case protocol.EventMsgKindTurnAborted:
		reason := protocol.TurnAbortReasonInterrupted
		if msg.TurnAborted != nil {
			reason = msg.TurnAborted.Reason
		}
		switch reason {
		case protocol.TurnAbortReasonInterrupted, protocol.TurnAbortReasonBudgetLimited:
			return protocol.AgentStatus{Kind: protocol.AgentStatusInterrupted}, true
		default:
			return protocol.AgentStatus{Kind: protocol.AgentStatusErrored, ErroredMessage: string(reason)}, true
		}
	case protocol.EventMsgKindError:
		var m string
		if msg.Error != nil {
			m = msg.Error.Message
		}
		return protocol.AgentStatus{Kind: protocol.AgentStatusErrored, ErroredMessage: m}, true
	case protocol.EventMsgKindShutdownComplete:
		return protocol.AgentStatus{Kind: protocol.AgentStatusShutdown}, true
	default:
		return protocol.AgentStatus{}, false
	}
}

// isFinalAgentStatus reports whether a status is terminal, mirroring the Rust
// `status::is_final`.
func isFinalAgentStatus(status protocol.AgentStatus) bool {
	switch status.Kind {
	case protocol.AgentStatusPendingInit, protocol.AgentStatusRunning, protocol.AgentStatusInterrupted:
		return false
	default:
		return true
	}
}
