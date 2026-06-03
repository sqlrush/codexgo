package rollout

import "github.com/sqlrush/codexgo/internal/protocol"

// InitialHistoryKind enumerates the variants of InitialHistory.
//
// Mirrors the Rust `InitialHistory` externally tagged enum.
type InitialHistoryKind string

const (
	// InitialHistoryKindNew is a brand-new session with no history.
	InitialHistoryKindNew InitialHistoryKind = "New"
	// InitialHistoryKindCleared is a session whose history was cleared.
	InitialHistoryKindCleared InitialHistoryKind = "Cleared"
	// InitialHistoryKindResumed is a session resumed from a rollout file.
	InitialHistoryKindResumed InitialHistoryKind = "Resumed"
	// InitialHistoryKindForked is a session forked from another's history.
	InitialHistoryKindForked InitialHistoryKind = "Forked"
)

// ResumedHistory carries the history loaded when resuming a session.
//
// Mirrors the Rust `ResumedHistory` struct.
type ResumedHistory struct {
	ConversationID protocol.ThreadID `json:"conversation_id"`
	History        []RolloutItem     `json:"history"`
	RolloutPath    *string           `json:"rollout_path,omitempty"`
}

// InitialHistory describes the history a session starts with.
//
// Mirrors the Rust `InitialHistory` enum. Exactly one of the data fields is
// populated for the corresponding Kind.
type InitialHistory struct {
	Kind InitialHistoryKind

	// Resumed holds the data for InitialHistoryKindResumed.
	Resumed *ResumedHistory
	// Forked holds the items for InitialHistoryKindForked.
	Forked []RolloutItem
}

// NewInitialHistory returns a New (empty) initial history.
func NewInitialHistory() InitialHistory {
	return InitialHistory{Kind: InitialHistoryKindNew}
}

// GetRolloutItems returns the history items for this initial history, or an
// empty slice for New/Cleared.
func (h InitialHistory) GetRolloutItems() []RolloutItem {
	switch h.Kind {
	case InitialHistoryKindResumed:
		if h.Resumed != nil {
			return append([]RolloutItem(nil), h.Resumed.History...)
		}
		return nil
	case InitialHistoryKindForked:
		return append([]RolloutItem(nil), h.Forked...)
	default:
		return nil
	}
}

// items returns the underlying slice for scanning (no copy).
func (h InitialHistory) items() []RolloutItem {
	switch h.Kind {
	case InitialHistoryKindResumed:
		if h.Resumed != nil {
			return h.Resumed.History
		}
	case InitialHistoryKindForked:
		return h.Forked
	}
	return nil
}

// ScanRolloutItems reports whether any rollout item satisfies the predicate.
func (h InitialHistory) ScanRolloutItems(predicate func(RolloutItem) bool) bool {
	for _, item := range h.items() {
		if predicate(item) {
			return true
		}
	}
	return false
}

// ForkedFromID returns the thread id this history was forked from, if any.
func (h InitialHistory) ForkedFromID() *protocol.ThreadID {
	switch h.Kind {
	case InitialHistoryKindResumed:
		for _, item := range h.items() {
			if item.Kind == RolloutItemKindSessionMeta && item.SessionMeta != nil {
				return item.SessionMeta.Meta.ForkedFromID
			}
		}
	case InitialHistoryKindForked:
		for _, item := range h.items() {
			if item.Kind == RolloutItemKindSessionMeta && item.SessionMeta != nil {
				id := item.SessionMeta.Meta.ID
				return &id
			}
		}
	}
	return nil
}

// SessionCwd returns the cwd recorded in the session metadata, if any.
func (h InitialHistory) SessionCwd() *string {
	for _, item := range h.items() {
		if item.Kind == RolloutItemKindSessionMeta && item.SessionMeta != nil {
			cwd := item.SessionMeta.Meta.Cwd
			return &cwd
		}
	}
	return nil
}

// GetEventMsgs returns the event messages in the history, or nil for
// New/Cleared.
func (h InitialHistory) GetEventMsgs() []protocol.EventMsg {
	if h.Kind != InitialHistoryKindResumed && h.Kind != InitialHistoryKindForked {
		return nil
	}
	events := make([]protocol.EventMsg, 0)
	for _, item := range h.items() {
		if item.Kind == RolloutItemKindEventMsg && item.EventMsg != nil {
			events = append(events, *item.EventMsg)
		}
	}
	return events
}

// GetBaseInstructions returns the base instructions recorded in the session
// metadata, if any.
func (h InitialHistory) GetBaseInstructions() *BaseInstructions {
	for _, item := range h.items() {
		if item.Kind == RolloutItemKindSessionMeta && item.SessionMeta != nil {
			return item.SessionMeta.Meta.BaseInstructions
		}
	}
	return nil
}

// GetDynamicTools returns the dynamic tools recorded in the session metadata,
// if any.
func (h InitialHistory) GetDynamicTools() *[]protocol.DynamicToolSpec {
	for _, item := range h.items() {
		if item.Kind == RolloutItemKindSessionMeta && item.SessionMeta != nil {
			return item.SessionMeta.Meta.DynamicTools
		}
	}
	return nil
}

// GetResumedSessionMeta returns the session metadata for a resumed history.
func (h InitialHistory) GetResumedSessionMeta() *SessionMeta {
	if h.Kind != InitialHistoryKindResumed {
		return nil
	}
	for _, item := range h.items() {
		if item.Kind == RolloutItemKindSessionMeta && item.SessionMeta != nil {
			return &item.SessionMeta.Meta
		}
	}
	return nil
}

// GetResumedThreadSource returns the thread source for a resumed history.
func (h InitialHistory) GetResumedThreadSource() *ThreadSource {
	meta := h.GetResumedSessionMeta()
	if meta == nil {
		return nil
	}
	return meta.ThreadSource
}
