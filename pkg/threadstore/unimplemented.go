package threadstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// UnimplementedStore supplies the Rust trait's default behavior for the
// optional [ThreadStore] operations: legacy history mode, no section or
// paginated-history support, and [ErrorKindUnsupported] for everything a store
// does not override. Embed it in a store and override the operations you
// support; the required operations (create/resume/append/persist/flush/
// shutdown/discard/load/read/list/update/archive/unarchive/delete) are
// deliberately NOT provided so the compiler still enforces them.
//
// The Rust defaults for archive_threads / delete_threads loop over the
// single-thread operations; an embedded value cannot reach the outer store, so
// those two report Unsupported here and stores implement them by calling
// [ArchiveThreadsSequentially] / [DeleteThreadsSequentially].
type UnimplementedStore struct{}

// DefaultHistoryMode returns Legacy.
func (UnimplementedStore) DefaultHistoryMode() protocol.ThreadHistoryMode {
	return protocol.ThreadHistoryModeLegacy
}

// LoadLatestModelContext reports Unsupported.
func (UnimplementedStore) LoadLatestModelContext(context.Context, LoadThreadHistoryParams) (StoredModelContext, error) {
	return StoredModelContext{}, unsupportedError("load_latest_model_context")
}

// PrepareFork reports Unsupported.
func (UnimplementedStore) PrepareFork(context.Context, PrepareForkParams) (PreparedFork, error) {
	return PreparedFork{}, unsupportedError("prepare_fork")
}

// SupportsThreadSections returns false.
func (UnimplementedStore) SupportsThreadSections() bool { return false }

// ListThreadSections reports Unsupported.
func (UnimplementedStore) ListThreadSections(context.Context, ListThreadSectionsParams) (StoredThreadSectionsPage, error) {
	return StoredThreadSectionsPage{}, unsupportedError("threadSection/list")
}

// CreateThreadSection reports Unsupported.
func (UnimplementedStore) CreateThreadSection(context.Context, CreateThreadSectionParams) (StoredThreadSection, error) {
	return StoredThreadSection{}, unsupportedError("threadSection/create")
}

// RenameThreadSection reports Unsupported.
func (UnimplementedStore) RenameThreadSection(context.Context, RenameThreadSectionParams) (*StoredThreadSection, error) {
	return nil, unsupportedError("threadSection/update")
}

// DeleteThreadSection reports Unsupported.
func (UnimplementedStore) DeleteThreadSection(context.Context, DeleteThreadSectionParams) (bool, error) {
	return false, unsupportedError("threadSection/delete")
}

// SupportsPaginatedHistoryLists returns false.
func (UnimplementedStore) SupportsPaginatedHistoryLists() bool { return false }

// SearchThreads reports Unsupported.
func (UnimplementedStore) SearchThreads(context.Context, SearchThreadsParams) (ThreadSearchPage, error) {
	return ThreadSearchPage{}, unsupportedError("thread/search")
}

// SearchThreadOccurrences reports Unsupported.
func (UnimplementedStore) SearchThreadOccurrences(context.Context, SearchThreadOccurrencesParams) (ThreadOccurrenceSearchPage, error) {
	return ThreadOccurrenceSearchPage{}, unsupportedError("thread/searchOccurrences")
}

// ListTurns reports Unsupported.
func (UnimplementedStore) ListTurns(context.Context, ListTurnsParams) (TurnPage, error) {
	return TurnPage{}, unsupportedError("list_turns")
}

// ListItems reports Unsupported.
func (UnimplementedStore) ListItems(context.Context, ListItemsParams) (ItemPage, error) {
	return ItemPage{}, unsupportedError("list_items")
}

// MoveThreadToSection reports Unsupported.
func (UnimplementedStore) MoveThreadToSection(context.Context, MoveThreadToSectionParams) error {
	return unsupportedError("thread/section/move")
}

// ArchiveThreads reports Unsupported; stores implement it via
// [ArchiveThreadsSequentially].
func (UnimplementedStore) ArchiveThreads(context.Context, ArchiveThreadsParams) ([]protocol.ThreadID, error) {
	return nil, unsupportedError("archive_threads")
}

// DeleteThreads reports Unsupported; stores implement it via
// [DeleteThreadsSequentially].
func (UnimplementedStore) DeleteThreads(context.Context, DeleteThreadsParams) error {
	return unsupportedError("delete_threads")
}

// ArchiveThreadsSequentially is the Rust `archive_threads` default: archive in
// order, fail if the FIRST archive fails, and treat later failures as best
// effort (returning the ids that did archive; later errors are reported through
// warn, which is nil-tolerant).
func ArchiveThreadsSequentially(ctx context.Context, store ThreadStore, params ArchiveThreadsParams, warn func(error)) ([]protocol.ThreadID, error) {
	archived := make([]protocol.ThreadID, 0, len(params.ThreadIDs))
	for _, id := range params.ThreadIDs {
		err := store.ArchiveThread(ctx, ArchiveThreadParams{ThreadID: id})
		switch {
		case err == nil:
			archived = append(archived, id)
		case len(archived) == 0:
			return nil, err
		default:
			if warn != nil {
				warn(fmt.Errorf("failed to archive thread %s: %w", id, err))
			}
		}
	}
	return archived, nil
}

// DeleteThreadsSequentially is the Rust `delete_threads` default: delete in
// order, treating already-missing members ([ErrorKindThreadNotFound]) as
// deleted and stopping at the first other error.
func DeleteThreadsSequentially(ctx context.Context, store ThreadStore, params DeleteThreadsParams) error {
	for _, id := range params.ThreadIDs {
		err := store.DeleteThread(ctx, DeleteThreadParams{ThreadID: id})
		if err == nil {
			continue
		}
		var storeErr *Error
		if errors.As(err, &storeErr) && storeErr.Kind == ErrorKindThreadNotFound {
			continue
		}
		return err
	}
	return nil
}
