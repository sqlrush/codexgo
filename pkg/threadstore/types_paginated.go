package threadstore

import (
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// This file carries the store types added by upstream 0.147 (spec 50 D0.1):
// targeted model-context reads, reference-backed forks, turn/item pagination,
// in-thread occurrence search, sections, and bulk archive/delete. Field names
// and JSON tags mirror the Rust `codex-thread-store` types.

// StoredModelContext is the persisted rollout items needed to reconstruct the
// latest model-visible context. Stores with targeted reads may return only a
// resumable suffix; stores without may return the full history. Items are in
// replay order either way. Mirrors Rust `StoredModelContext`.
type StoredModelContext struct {
	ThreadID protocol.ThreadID     `json:"thread_id"`
	Items    []rollout.RolloutItem `json:"items"`
}

// ForkBoundaryKind selects how much of a source thread's history a fork inherits.
type ForkBoundaryKind string

const (
	// ForkBoundaryLatest inherits the source thread's latest durable state.
	ForkBoundaryLatest ForkBoundaryKind = "latest"
	// ForkBoundaryThroughTurn inherits history through the newest visible
	// occurrence of TurnID.
	ForkBoundaryThroughTurn ForkBoundaryKind = "through_turn"
	// ForkBoundaryBeforeTurn inherits history preceding the original visible
	// occurrence of TurnID.
	ForkBoundaryBeforeTurn ForkBoundaryKind = "before_turn"
)

// ForkBoundary is the requested boundary for inheriting a paginated thread's
// history. Mirrors Rust `ForkBoundary`.
type ForkBoundary struct {
	Kind ForkBoundaryKind
	// TurnID is set for ThroughTurn / BeforeTurn.
	TurnID string
}

// PrepareForkParams freezes the source history used to initialize a fork.
type PrepareForkParams struct {
	// ThreadID is the immediate source thread whose metadata and approval
	// settings are inherited.
	ThreadID protocol.ThreadID
	// Boundary is the requested inclusive or exclusive fork boundary.
	Boundary ForkBoundary
}

// PreparedFork is the frozen source history and model context for a
// reference-backed fork. Mirrors Rust `PreparedFork`.
type PreparedFork struct {
	// SourceThreadID is the immediate source thread, even when the normalized
	// history base names an ancestor.
	SourceThreadID protocol.ThreadID
	// HistoryBase is the frozen physical rollout prefix inherited by the child.
	HistoryBase *protocol.HistoryPosition
	// ModelContext is the bounded model context selected by the boundary.
	ModelContext []rollout.RolloutItem
	// Release drops the store-owned source reservation that blocks source
	// deletion until the child's history reference is durable. nil when the
	// store needs no reservation. Callers invoke it exactly once.
	Release func()
}

// StoredTurnItemsView is the requested amount of item detail for stored turns.
type StoredTurnItemsView string

const (
	// StoredTurnItemsViewNotLoaded returns turn metadata only.
	StoredTurnItemsViewNotLoaded StoredTurnItemsView = "not_loaded"
	// StoredTurnItemsViewSummary returns display summary items for each turn
	// (the default).
	StoredTurnItemsViewSummary StoredTurnItemsView = "summary"
)

// StoredTurnStatus is the store-owned status for a persisted turn.
type StoredTurnStatus string

const (
	StoredTurnStatusCompleted   StoredTurnStatus = "completed"
	StoredTurnStatusInterrupted StoredTurnStatus = "interrupted"
	StoredTurnStatusFailed      StoredTurnStatus = "failed"
	StoredTurnStatusInProgress  StoredTurnStatus = "in_progress"
)

// StoredTurnError is the store-owned error detail for a failed persisted turn.
type StoredTurnError struct {
	Message string `json:"message"`
	// CodexErrorInfo is the structured error classification, when available.
	CodexErrorInfo    *protocol.CodexErrorInfo `json:"codexErrorInfo,omitempty"`
	AdditionalDetails *string                  `json:"additionalDetails,omitempty"`
}

// ListTurnsParams lists turns within a stored thread.
type ListTurnsParams struct {
	ThreadID        protocol.ThreadID
	IncludeArchived bool
	// Cursor is the opaque cursor returned by a previous list call.
	Cursor *string
	// PageSize is the maximum number of turns to return.
	PageSize      int
	SortDirection SortDirection
	// ItemsView selects the amount of item detail per turn; empty = Summary.
	ItemsView StoredTurnItemsView
}

// StoredTurn is the store-owned turn representation used by turn pagination.
type StoredTurn struct {
	TurnID string `json:"turn_id"`
	// Items are the projected app-server item snapshots for this turn,
	// according to ItemsView.
	Items     []StoredThreadItem  `json:"items"`
	ItemsView StoredTurnItemsView `json:"items_view"`
	Status    StoredTurnStatus    `json:"status"`
	Error     *StoredTurnError    `json:"error,omitempty"`
	// StartedAt / CompletedAt are unix seconds.
	StartedAt   *int64 `json:"started_at,omitempty"`
	CompletedAt *int64 `json:"completed_at,omitempty"`
	DurationMS  *int64 `json:"duration_ms,omitempty"`
}

// TurnPage is a page of stored turns.
type TurnPage struct {
	Turns []StoredTurn `json:"turns"`
	// NextCursor continues listing; BackwardsCursor fetches the opposite direction.
	NextCursor      *string `json:"next_cursor,omitempty"`
	BackwardsCursor *string `json:"backwards_cursor,omitempty"`
}

// ItemSortKey selects the ordinal used when listing persisted items.
type ItemSortKey string

const (
	// ItemSortKeyCreatedAtOrdinal sorts by the ordinal where the item was first projected.
	ItemSortKeyCreatedAtOrdinal ItemSortKey = "created_at_ordinal"
	// ItemSortKeyUpdatedAtOrdinal sorts by the ordinal where the item was last
	// updated (requires an update watermark).
	ItemSortKeyUpdatedAtOrdinal ItemSortKey = "updated_at_ordinal"
)

// ListItemsParams lists persisted items within a thread, optionally filtered to a turn.
type ListItemsParams struct {
	ThreadID protocol.ThreadID
	// TurnID filters to one turn; nil returns items across the thread.
	TurnID          *string
	IncludeArchived bool
	Cursor          *string
	PageSize        int
	SortDirection   SortDirection
	// SortKey selects the ordinal; empty = CreatedAtOrdinal.
	SortKey ItemSortKey
	// AfterUpdatedAtOrdinal filters out items with an update ordinal <= the value.
	AfterUpdatedAtOrdinal *uint64
}

// StoredThreadItem is a projected app-server ThreadItem snapshot within a turn.
type StoredThreadItem struct {
	TurnID string `json:"turn_id"`
	// ItemID is the stable item identifier within the turn.
	ItemID string `json:"item_id"`
	// UpdatedAtOrdinal is the rollout ordinal of the latest persisted update.
	UpdatedAtOrdinal uint64 `json:"updated_at_ordinal"`
	// CreatedAtMS is when this logical item was first projected (unix millis).
	CreatedAtMS int64 `json:"created_at_ms"`
	// ItemJSON is the serialized app-server ThreadItem snapshot.
	ItemJSON []byte `json:"item_json"`
}

// ItemPage is a page of persisted items within a thread.
type ItemPage struct {
	Items           []StoredThreadItem `json:"items"`
	NextCursor      *string            `json:"next_cursor,omitempty"`
	BackwardsCursor *string            `json:"backwards_cursor,omitempty"`
}

// SearchThreadOccurrencesParams searches visible message occurrences within
// one paginated thread.
type SearchThreadOccurrencesParams struct {
	ThreadID protocol.ThreadID
	// SearchTerm is a case-insensitive literal substring.
	SearchTerm string
	Cursor     *string
	PageSize   int
}

// SearchTextRange is a UTF-16 code-unit range within a snippet.
type SearchTextRange struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

// StoredThreadOccurrence is one visible message occurrence within a stored thread.
type StoredThreadOccurrence struct {
	TurnID            string          `json:"turn_id"`
	ItemID            string          `json:"item_id"`
	Snippet           string          `json:"snippet"`
	SnippetMatchRange SearchTextRange `json:"snippet_match_range"`
	// TurnCursor is the inclusive cursor accepted by ListTurns for this turn.
	TurnCursor string `json:"turn_cursor"`
}

// ThreadOccurrenceSearchPage is a page of occurrences within one thread.
type ThreadOccurrenceSearchPage struct {
	Items      []StoredThreadOccurrence `json:"items"`
	NextCursor *string                  `json:"next_cursor,omitempty"`
}

// ListThreadSectionsParams lists independently persisted thread sections.
type ListThreadSectionsParams struct {
	Cursor *string
	Limit  int
}

// CreateThreadSectionParams creates a thread section.
type CreateThreadSectionParams struct {
	// Name is the user-facing section name.
	Name string
}

// RenameThreadSectionParams renames a thread section.
type RenameThreadSectionParams struct {
	SectionID string
	Name      string
}

// DeleteThreadSectionParams deletes a thread section.
type DeleteThreadSectionParams struct {
	SectionID string
}

// StoredThreadSection is an independently persisted thread section.
type StoredThreadSection struct {
	// ID is the stable, server-owned section identifier.
	ID   string `json:"id"`
	Name string `json:"name"`
}

// StoredThreadSectionsPage is a cursor-paginated page of sections.
type StoredThreadSectionsPage struct {
	Sections   []StoredThreadSection `json:"sections"`
	NextCursor *string               `json:"next_cursor,omitempty"`
}

// MoveThreadToSectionParams moves a thread to, within, or out of a
// server-ordered section.
type MoveThreadToSectionParams struct {
	ThreadID protocol.ThreadID
	// Section is the destination section id; nil removes the thread from its section.
	Section *string
	// BeforeThreadID is an existing member to insert before; nil appends.
	BeforeThreadID *protocol.ThreadID
}

// ArchiveThreadsParams archives a set of threads as one store operation.
type ArchiveThreadsParams struct {
	// ThreadIDs are archived in order.
	ThreadIDs []protocol.ThreadID
	// WriterLockThreadIDs are the threads whose paginated writer ownership must
	// be checked before archiving, including descendants whose rollout has not
	// materialized yet.
	WriterLockThreadIDs []protocol.ThreadID
}

// DeleteThreadParams deletes a thread's persisted rollout data and metadata.
type DeleteThreadParams struct {
	ThreadID protocol.ThreadID
}

// DeleteThreadsParams deletes a set of threads as one store operation.
type DeleteThreadsParams struct {
	// ThreadIDs are deleted in order; already-missing members count as deleted.
	ThreadIDs []protocol.ThreadID
}
