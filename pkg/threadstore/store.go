package threadstore

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ThreadStore is the storage-neutral thread persistence boundary, mirroring the
// Rust `ThreadStore` trait as of upstream 0.147 (32 operations).
//
// Many operations have default Unsupported behavior in the Rust trait
// (sections, occurrence search, turn/item pagination, prepare_fork, ...);
// concrete implementations override the ones they support. In Go the interface
// declares the full surface; implementations embed [UnimplementedStore] to
// inherit the Rust defaults and override what they support, returning an
// [ErrorKindUnsupported] error from the rest.
type ThreadStore interface {
	// DefaultHistoryMode returns the history mode to use when history does not
	// carry a persisted mode. Legacy keeps existing stores compatible; stores
	// whose durable contract is paginated return Paginated.
	DefaultHistoryMode() protocol.ThreadHistoryMode

	// CreateThread creates a new live thread.
	CreateThread(ctx context.Context, params CreateThreadParams) error

	// ResumeThread reopens an existing thread for live appends.
	ResumeThread(ctx context.Context, params ResumeThreadParams) error

	// AppendItems appends canonical rollout items to a live thread.
	//
	// This is the raw history API: it does not infer metadata from item contents.
	// Callers that need metadata updates should call UpdateThreadMetadata with
	// explicit metadata facts prepared above the store.
	AppendItems(ctx context.Context, params AppendThreadItemsParams) error

	// PersistThread materializes the thread if persistence is lazy, then persists
	// all queued items.
	PersistThread(ctx context.Context, threadID protocol.ThreadID) error

	// FlushThread flushes all queued items and returns once they are durable.
	FlushThread(ctx context.Context, threadID protocol.ThreadID) error

	// ShutdownThread flushes pending items and closes the live thread writer.
	ShutdownThread(ctx context.Context, threadID protocol.ThreadID) error

	// DiscardThread discards the live thread writer without forcing pending
	// in-memory items to become durable.
	DiscardThread(ctx context.Context, threadID protocol.ThreadID) error

	// LoadHistory loads persisted history for resume, fork, rollback, and memory
	// jobs.
	LoadHistory(ctx context.Context, params LoadThreadHistoryParams) (StoredThreadHistory, error)

	// LoadLatestModelContext loads the persisted rollout items needed to
	// reconstruct the latest model-visible context. Implementations that cannot
	// perform a targeted read may return the full persisted history.
	LoadLatestModelContext(ctx context.Context, params LoadThreadHistoryParams) (StoredModelContext, error)

	// PrepareFork freezes source history and model context used to initialize a
	// referenced fork. Stores without reference-backed fork support report
	// Unsupported.
	PrepareFork(ctx context.Context, params PrepareForkParams) (PreparedFork, error)

	// ReadThread reads a thread summary and optionally its persisted history.
	ReadThread(ctx context.Context, params ReadThreadParams) (StoredThread, error)

	// ReadThreadByRolloutPath reads a rollout-backed thread by path when the store
	// supports path-addressed lookups.
	ReadThreadByRolloutPath(ctx context.Context, params ReadThreadByRolloutPathParams) (StoredThread, error)

	// ListThreads lists stored threads matching the supplied filters.
	ListThreads(ctx context.Context, params ListThreadsParams) (ThreadPage, error)

	// SupportsThreadSections reports whether this store can discover and manage
	// independently persisted thread sections.
	SupportsThreadSections() bool
	// ListThreadSections lists independently persisted thread sections.
	ListThreadSections(ctx context.Context, params ListThreadSectionsParams) (StoredThreadSectionsPage, error)
	// CreateThreadSection creates a custom section with a stable, server-assigned id.
	CreateThreadSection(ctx context.Context, params CreateThreadSectionParams) (StoredThreadSection, error)
	// RenameThreadSection renames a custom section, returning nil when it does
	// not exist.
	RenameThreadSection(ctx context.Context, params RenameThreadSectionParams) (*StoredThreadSection, error)
	// DeleteThreadSection deletes a custom section and reports whether it existed.
	DeleteThreadSection(ctx context.Context, params DeleteThreadSectionParams) (bool, error)

	// SupportsPaginatedHistoryLists reports whether paginated threads can
	// hydrate durable history through ListTurns/ListItems.
	SupportsPaginatedHistoryLists() bool

	// SearchThreads searches stored threads and returns search-only preview
	// metadata. Implementations that do not support search return an
	// [ErrorKindUnsupported] error.
	SearchThreads(ctx context.Context, params SearchThreadsParams) (ThreadSearchPage, error)
	// SearchThreadOccurrences searches visible message occurrences within one
	// paginated thread.
	SearchThreadOccurrences(ctx context.Context, params SearchThreadOccurrencesParams) (ThreadOccurrenceSearchPage, error)
	// ListTurns lists turns within a stored thread.
	ListTurns(ctx context.Context, params ListTurnsParams) (TurnPage, error)
	// ListItems lists persisted items within a stored thread, optionally
	// filtered to a turn.
	ListItems(ctx context.Context, params ListItemsParams) (ItemPage, error)

	// UpdateThreadMetadata applies a literal metadata patch and returns the updated
	// thread. Policy such as deciding whether an append-derived preview should
	// be emitted belongs above the store.
	UpdateThreadMetadata(ctx context.Context, params UpdateThreadMetadataParams) (StoredThread, error)

	// MoveThreadToSection moves a thread to, within, or out of a server-ordered
	// section.
	MoveThreadToSection(ctx context.Context, params MoveThreadToSectionParams) error

	// ArchiveThread archives a thread.
	ArchiveThread(ctx context.Context, params ArchiveThreadParams) error

	// ArchiveThreads archives threads in order, returning the successfully
	// archived ids. The first thread must archive successfully; later failures
	// are best effort ([ArchiveThreadsSequentially] is the Rust default).
	ArchiveThreads(ctx context.Context, params ArchiveThreadsParams) ([]protocol.ThreadID, error)

	// UnarchiveThread unarchives a thread and returns its updated metadata.
	UnarchiveThread(ctx context.Context, params ArchiveThreadParams) (StoredThread, error)

	// DeleteThread deletes a thread's persisted rollout data and associated
	// metadata.
	DeleteThread(ctx context.Context, params DeleteThreadParams) error

	// DeleteThreads deletes threads in order, treating already-missing members
	// as deleted ([DeleteThreadsSequentially] is the Rust default).
	DeleteThreads(ctx context.Context, params DeleteThreadsParams) error
}
