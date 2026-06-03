package threadstore

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ThreadStore is the storage-neutral thread persistence boundary, mirroring the
// Rust `ThreadStore` trait.
//
// SearchThreads, ListTurns, and ListItems have default Unsupported behavior in
// the Rust trait; concrete implementations override the ones they support. In Go
// the interface declares the full surface and implementations return an
// [ErrorKindUnsupported] error from operations they do not support.
type ThreadStore interface {
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

	// ReadThread reads a thread summary and optionally its persisted history.
	ReadThread(ctx context.Context, params ReadThreadParams) (StoredThread, error)

	// ReadThreadByRolloutPath reads a rollout-backed thread by path when the store
	// supports path-addressed lookups.
	ReadThreadByRolloutPath(ctx context.Context, params ReadThreadByRolloutPathParams) (StoredThread, error)

	// ListThreads lists stored threads matching the supplied filters.
	ListThreads(ctx context.Context, params ListThreadsParams) (ThreadPage, error)

	// SearchThreads searches stored threads and returns search-only preview
	// metadata. Implementations that do not support search return an
	// [ErrorKindUnsupported] error.
	SearchThreads(ctx context.Context, params SearchThreadsParams) (ThreadSearchPage, error)

	// UpdateThreadMetadata applies a literal metadata patch and returns the updated
	// thread.
	UpdateThreadMetadata(ctx context.Context, params UpdateThreadMetadataParams) (StoredThread, error)

	// ArchiveThread archives a thread.
	ArchiveThread(ctx context.Context, params ArchiveThreadParams) error

	// UnarchiveThread unarchives a thread and returns its updated metadata.
	UnarchiveThread(ctx context.Context, params ArchiveThreadParams) (StoredThread, error)
}
