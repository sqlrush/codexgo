package core

import (
	"github.com/sqlrush/codexgo/internal/protocol"
)

// PreviousTurnSettings captures the model/realtime settings used by the most
// recent regular user turn. It feeds turn-to-turn diffing (model switches,
// realtime transitions) and full-context reinjection after resume or compaction.
// Mirrors the Rust `PreviousTurnSettings`.
type PreviousTurnSettings struct {
	// Model is the model slug used by the previous turn.
	Model string
	// RealtimeActive records the previous turn's realtime state, if known.
	RealtimeActive *bool
}

// SessionState holds the persistent, session-scoped mutable state shared across
// turns. It is a faithful port of the Rust `state::session::SessionState`,
// reduced to the fields the turn-running path needs.
//
// SessionState is NOT internally synchronized; it is always accessed while
// holding the owning [Session]'s state mutex (mirroring the Rust
// `Mutex<SessionState>`). The conversation history it points to IS independently
// thread-safe.
//
// STUB: connector selection, granted-permission merging, MCP-dependency
// prompting, additional-context store, and startup prewarm are deferred to the
// area agents that own those flows.
type SessionState struct {
	// Config is the effective session configuration snapshot.
	Config SessionConfiguration
	// History is the shared, thread-safe conversation history.
	History *ConversationHistory

	latestRateLimits        *protocol.RateLimitSnapshot
	serverReasoningIncluded bool
	previousTurnSettings    *PreviousTurnSettings
	autoCompactWindow       autoCompactWindow
	nextTurnIsFirst         bool
}

// NewSessionState builds the initial session state for a configuration,
// mirroring the Rust `SessionState::new`.
func NewSessionState(cfg SessionConfiguration) *SessionState {
	return &SessionState{
		Config:            cfg,
		History:           NewConversationHistory(),
		autoCompactWindow: newAutoCompactWindow(),
		nextTurnIsFirst:   true,
	}
}

// PreviousTurnSettings returns a copy of the previous-turn settings, if any.
func (s *SessionState) PreviousTurnSettings() *PreviousTurnSettings {
	if s.previousTurnSettings == nil {
		return nil
	}
	cp := *s.previousTurnSettings
	if s.previousTurnSettings.RealtimeActive != nil {
		v := *s.previousTurnSettings.RealtimeActive
		cp.RealtimeActive = &v
	}
	return &cp
}

// SetPreviousTurnSettings records the settings used by the latest regular turn.
func (s *SessionState) SetPreviousTurnSettings(settings *PreviousTurnSettings) {
	if settings == nil {
		s.previousTurnSettings = nil
		return
	}
	cp := *settings
	s.previousTurnSettings = &cp
}

// SetNextTurnIsFirst records whether the next turn should be treated as the
// session's first user turn.
func (s *SessionState) SetNextTurnIsFirst(v bool) { s.nextTurnIsFirst = v }

// TakeNextTurnIsFirst returns the first-turn flag and resets it to false,
// mirroring the Rust `take_next_turn_is_first`.
func (s *SessionState) TakeNextTurnIsFirst() bool {
	v := s.nextTurnIsFirst
	s.nextTurnIsFirst = false
	return v
}

// SetTokenInfo stores the latest token-usage snapshot on the history.
func (s *SessionState) SetTokenInfo(info *protocol.TokenUsageInfo) {
	s.History.SetTokenInfo(info)
}

// TokenInfo returns the latest token-usage snapshot, or nil.
func (s *SessionState) TokenInfo() *protocol.TokenUsageInfo { return s.History.TokenInfo() }

// UpdateTokenInfoFromUsage folds a turn's usage into the running total.
func (s *SessionState) UpdateTokenInfoFromUsage(usage protocol.TokenUsage, modelContextWindow *int64) {
	s.History.UpdateTokenInfoFromUsage(usage, modelContextWindow)
	s.autoCompactWindow.ensureServerObservedPrefillFromUsage(usage)
}

// SetRateLimits merges a new rate-limit snapshot with the previous one,
// preserving credits/plan fields that the new snapshot omits. Mirrors the Rust
// `set_rate_limits` + `merge_rate_limit_fields`.
func (s *SessionState) SetRateLimits(snapshot protocol.RateLimitSnapshot) {
	s.latestRateLimits = mergeRateLimitFields(s.latestRateLimits, snapshot)
}

// LatestRateLimits returns a copy of the most recent rate-limit snapshot.
func (s *SessionState) LatestRateLimits() *protocol.RateLimitSnapshot {
	if s.latestRateLimits == nil {
		return nil
	}
	cp := *s.latestRateLimits
	return &cp
}

// TokenInfoAndRateLimits returns the current token info and rate-limit snapshot.
func (s *SessionState) TokenInfoAndRateLimits() (*protocol.TokenUsageInfo, *protocol.RateLimitSnapshot) {
	return s.TokenInfo(), s.LatestRateLimits()
}

// SetServerReasoningIncluded records whether the server includes reasoning in
// usage accounting.
func (s *SessionState) SetServerReasoningIncluded(v bool) { s.serverReasoningIncluded = v }

// ServerReasoningIncluded reports the server-reasoning-included flag.
func (s *SessionState) ServerReasoningIncluded() bool { return s.serverReasoningIncluded }

// SetAutoCompactWindowEstimatedPrefill records an estimated prefill baseline.
func (s *SessionState) SetAutoCompactWindowEstimatedPrefill(tokens int64) {
	s.autoCompactWindow.setEstimatedPrefill(tokens)
}

// StartNextAutoCompactWindow advances the auto-compaction window.
func (s *SessionState) StartNextAutoCompactWindow() { s.autoCompactWindow.startNext() }

// AutoCompactWindowSnapshot returns an immutable view of the window state.
func (s *SessionState) AutoCompactWindowSnapshot() AutoCompactWindowSnapshot {
	return s.autoCompactWindow.snapshot()
}

// ReplaceHistory swaps the conversation history and clears the prefill baseline,
// mirroring the Rust `replace_history`.
func (s *SessionState) ReplaceHistory(items []protocol.ResponseItem) {
	s.History.Replace(items)
	s.autoCompactWindow.clearPrefill()
}

// mergeRateLimitFields preserves credits/plan fields from a previous snapshot
// when the incoming one omits them, and defaults a missing limit_id to "codex".
// Mirrors the Rust `merge_rate_limit_fields`.
func mergeRateLimitFields(previous *protocol.RateLimitSnapshot, snapshot protocol.RateLimitSnapshot) *protocol.RateLimitSnapshot {
	if snapshot.LimitID == nil {
		def := "codex"
		snapshot.LimitID = &def
	}
	if snapshot.Credits == nil && previous != nil {
		snapshot.Credits = previous.Credits
	}
	if snapshot.PlanType == nil && previous != nil {
		snapshot.PlanType = previous.PlanType
	}
	return &snapshot
}

// StartNewContextWindow advances to a fresh context window (0.147
// `start_new_context_window`): new window id, cleared prefill and per-window
// flags. Returns the new window number and identities.
func (s *SessionState) StartNewContextWindow() (uint64, AutoCompactWindowIDs) {
	n, ids := s.autoCompactWindow.advance()
	s.autoCompactWindow.clearPrefill()
	return n, ids
}

// CurrentWindowID returns the active context window id (UUIDv7).
func (s *SessionState) CurrentWindowID() string { return s.autoCompactWindow.ids.WindowID }

// RequestNewContextWindow records a pending `new_context_window` request.
func (s *SessionState) RequestNewContextWindow() { s.autoCompactWindow.requestNewContextWindow() }

// TakeNewContextWindowRequest returns and clears the pending request.
func (s *SessionState) TakeNewContextWindowRequest() bool {
	return s.autoCompactWindow.takeNewContextWindowRequest()
}

// ClaimTokenBudgetReminder / ClaimAutoCompactFallback return true the first
// time they are called within the current window.
func (s *SessionState) ClaimTokenBudgetReminder() bool {
	return s.autoCompactWindow.claimTokenBudgetReminder()
}

// ClaimAutoCompactFallback returns true the first time per window.
func (s *SessionState) ClaimAutoCompactFallback() bool {
	return s.autoCompactWindow.claimAutoCompactFallback()
}

// RestoreContextWindow re-installs persisted window accounting on resume.
func (s *SessionState) RestoreContextWindow(windowNumber uint64, ids AutoCompactWindowIDs) {
	s.autoCompactWindow.restore(windowNumber, ids)
}

// GetTotalTokenUsage returns the tokens in the active context window (0.147
// `get_total_token_usage`): the last request's usage plus an estimate of the
// trailing client-added items when the server accounts past reasoning.
func (s *SessionState) GetTotalTokenUsage() int64 {
	return s.History.GetTotalTokenUsage(s.serverReasoningIncluded)
}
