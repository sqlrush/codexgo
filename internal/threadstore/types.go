// Package threadstore provides storage-neutral thread persistence interfaces, a
// faithful Go port of the Rust `codex-thread-store` crate.
//
// Application code should treat a [protocol.ThreadID] as the only durable thread
// handle. Implementations are responsible for resolving that id to local rollout
// files or any other backing store.
package threadstore

import (
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// ThreadEventPersistenceMode controls how many event variants are persisted for
// future replay, mirroring the Rust enum (default Limited).
type ThreadEventPersistenceMode int

const (
	// ThreadEventPersistenceModeLimited persists only the legacy minimal replay
	// surface. It is the default.
	ThreadEventPersistenceModeLimited ThreadEventPersistenceMode = iota
	// ThreadEventPersistenceModeExtended persists the richer event surface used by
	// app-server history reconstruction.
	ThreadEventPersistenceModeExtended
)

// ToRolloutMode converts to the rollout package's [rollout.EventPersistenceMode].
func (m ThreadEventPersistenceMode) ToRolloutMode() rollout.EventPersistenceMode {
	if m == ThreadEventPersistenceModeExtended {
		return rollout.EventPersistenceModeExtended
	}
	return rollout.EventPersistenceModeLimited
}

// ThreadPersistenceMetadata is thread-scoped metadata used when opening live
// persistence, mirroring the Rust struct.
type ThreadPersistenceMetadata struct {
	// Cwd is the effective working directory for environment-backed threads. nil
	// means the thread has no filesystem/environment context.
	Cwd *string
	// ModelProvider is the model provider associated with the thread.
	ModelProvider string
	// MemoryMode is the memory mode associated with the live thread.
	MemoryMode protocol.ThreadMemoryMode
}

// CreateThreadParams holds the parameters required to create a persisted thread.
type CreateThreadParams struct {
	// ThreadID is the thread id generated before opening persistence.
	ThreadID protocol.ThreadID
	// ForkedFromID is the source thread id when this thread is created as a fork.
	ForkedFromID *protocol.ThreadID
	// Source is the runtime source for the thread.
	Source rollout.SessionSource
	// ThreadSource is the optional analytics source classification.
	ThreadSource *rollout.ThreadSource
	// BaseInstructions are persisted in session metadata.
	BaseInstructions rollout.BaseInstructions
	// DynamicTools are available to the thread at startup.
	DynamicTools []protocol.DynamicToolSpec
	// Metadata captured for the newly created thread.
	Metadata ThreadPersistenceMetadata
	// EventPersistenceMode selects whether persistence includes the extended event
	// surface.
	EventPersistenceMode ThreadEventPersistenceMode
}

// ResumeThreadParams holds the parameters required to reopen persistence for an
// existing thread.
type ResumeThreadParams struct {
	// ThreadID is the existing thread id whose future items should be appended.
	ThreadID protocol.ThreadID
	// RolloutPath is the known local rollout path when resuming from a file.
	RolloutPath *string
	// History is the known replay history when already loaded by the caller.
	History *[]rollout.RolloutItem
	// IncludeArchived reports whether archived threads may be reopened.
	IncludeArchived bool
	// Metadata for future writes appended to the resumed live thread.
	Metadata ThreadPersistenceMetadata
	// EventPersistenceMode selects whether persistence includes the extended event
	// surface.
	EventPersistenceMode ThreadEventPersistenceMode
}

// AppendThreadItemsParams holds the parameters for appending rollout items to a
// live thread.
type AppendThreadItemsParams struct {
	// ThreadID is the thread id to append to.
	ThreadID protocol.ThreadID
	// Items are appended in order.
	Items []rollout.RolloutItem
}

// LoadThreadHistoryParams holds the parameters for loading persisted history.
type LoadThreadHistoryParams struct {
	// ThreadID is the thread id to load.
	ThreadID protocol.ThreadID
	// IncludeArchived reports whether archived threads are eligible.
	IncludeArchived bool
}

// StoredThreadHistory is the persisted rollout history for a thread, without any
// filesystem path requirement.
type StoredThreadHistory struct {
	// ThreadID is the thread represented by the history.
	ThreadID protocol.ThreadID
	// Items are the persisted rollout items in replay order.
	Items []rollout.RolloutItem
}

// ReadThreadParams holds the parameters for reading a thread summary and
// optionally its replay history.
type ReadThreadParams struct {
	// ThreadID is the thread id to read.
	ThreadID protocol.ThreadID
	// IncludeArchived reports whether archived threads are eligible.
	IncludeArchived bool
	// IncludeHistory reports whether persisted rollout items should be included.
	IncludeHistory bool
}

// ReadThreadByRolloutPathParams holds the parameters for reading a local
// rollout-backed thread by path.
type ReadThreadByRolloutPathParams struct {
	// RolloutPath is the local rollout JSONL path to read.
	RolloutPath string
	// IncludeArchived reports whether archived threads are eligible.
	IncludeArchived bool
	// IncludeHistory reports whether persisted rollout items should be included.
	IncludeHistory bool
}

// ThreadSortKey is the sort key used when listing stored threads (default
// CreatedAt).
type ThreadSortKey int

const (
	// ThreadSortKeyCreatedAt sorts by the thread creation timestamp (default).
	ThreadSortKeyCreatedAt ThreadSortKey = iota
	// ThreadSortKeyUpdatedAt sorts by the thread last-update timestamp.
	ThreadSortKeyUpdatedAt
)

// SortDirection is the direction used when listing stored threads (default Desc).
type SortDirection int

const (
	// SortDirectionAsc returns older threads before newer threads.
	SortDirectionAsc SortDirection = iota
	// SortDirectionDesc returns newer threads before older threads (default).
	SortDirectionDesc
)

// ListThreadsParams holds the parameters for listing threads.
type ListThreadsParams struct {
	// PageSize is the maximum number of threads to return.
	PageSize int
	// Cursor is an opaque cursor returned by a previous list call.
	Cursor *string
	// SortKey is the sort order requested by the caller.
	SortKey ThreadSortKey
	// SortDirection is the sort direction requested by the caller.
	SortDirection SortDirection
	// AllowedSources are the allowed session sources; empty means default.
	AllowedSources []rollout.SessionSource
	// ModelProviders is the optional model provider filter; nil means default
	// while an empty slice means all providers.
	ModelProviders *[]string
	// CwdFilters is the optional cwd filter; nil means all working directories
	// while an empty slice matches no threads.
	CwdFilters *[]string
	// Archived reports whether archived threads should be listed instead of active.
	Archived bool
	// SearchTerm is an optional substring/full-text search term.
	SearchTerm *string
	// UseStateDBOnly returns from the state DB without scanning JSONL rollouts.
	UseStateDBOnly bool
}

// SearchThreadsParams holds the parameters for searching thread content.
type SearchThreadsParams struct {
	// PageSize is the maximum number of threads to return.
	PageSize int
	// Cursor is an opaque cursor returned by a previous search call.
	Cursor *string
	// SortKey is the sort order requested by the caller.
	SortKey ThreadSortKey
	// SortDirection is the sort direction requested by the caller.
	SortDirection SortDirection
	// AllowedSources are the allowed session sources; empty means default.
	AllowedSources []rollout.SessionSource
	// Archived reports whether archived threads should be searched.
	Archived bool
	// SearchTerm is the visible thread content to search for.
	SearchTerm string
}

// ThreadPage is a page of stored thread records.
type ThreadPage struct {
	// Items are the threads returned for this page.
	Items []StoredThread
	// NextCursor is the opaque cursor to continue listing.
	NextCursor *string
}

// StoredThreadSearchResult pairs a stored thread with a matching snippet.
type StoredThreadSearchResult struct {
	Thread  StoredThread
	Snippet string
}

// ThreadSearchPage is a page of thread-search results.
type ThreadSearchPage struct {
	// Items are the search results returned for this page.
	Items []StoredThreadSearchResult
	// NextCursor is the opaque cursor to continue searching.
	NextCursor *string
}

// StoredThread is the store-owned thread metadata used by list/read/resume
// responses, mirroring the Rust `StoredThread`.
type StoredThread struct {
	// ThreadID is the thread id.
	ThreadID protocol.ThreadID
	// RolloutPath is the local rollout path when the backing store is
	// filesystem-based.
	RolloutPath *string
	// ForkedFromID is the source thread id when this thread was forked.
	ForkedFromID *protocol.ThreadID
	// Preview is the best available user-facing preview.
	Preview string
	// Name is the optional user-facing thread name/title.
	Name *string
	// ModelProvider is the model provider id associated with the thread.
	ModelProvider string
	// Model is the latest observed model, if known.
	Model *string
	// ReasoningEffort is the latest observed reasoning effort, if known.
	ReasoningEffort *protocol.ReasoningEffort
	// CreatedAt is the thread creation timestamp.
	CreatedAt time.Time
	// UpdatedAt is the thread last-update timestamp.
	UpdatedAt time.Time
	// ArchivedAt is the thread archive timestamp, if archived.
	ArchivedAt *time.Time
	// Cwd is the working directory captured for the thread.
	Cwd string
	// CliVersion is the CLI version captured for the thread.
	CliVersion string
	// Source is the runtime source for the thread.
	Source rollout.SessionSource
	// ThreadSource is the optional analytics source classification.
	ThreadSource *rollout.ThreadSource
	// AgentNickname is the optional random nickname for thread-spawn sub-agents.
	AgentNickname *string
	// AgentRole is the optional role for thread-spawn sub-agents.
	AgentRole *string
	// AgentPath is the optional canonical path for thread-spawn sub-agents.
	AgentPath *string
	// GitInfo is the optional Git metadata captured for the thread.
	GitInfo *GitInfo
	// ApprovalMode is the approval mode captured for the thread.
	ApprovalMode protocol.AskForApproval
	// PermissionProfile is the canonical runtime permissions captured.
	PermissionProfile protocol.PermissionProfile
	// TokenUsage is the last observed token usage.
	TokenUsage *protocol.TokenUsage
	// FirstUserMessage is the first user message observed, if any.
	FirstUserMessage *string
	// History is the persisted history, populated only when requested.
	History *StoredThreadHistory
}

// UpdateThreadMetadataParams holds the parameters for patching mutable thread
// metadata.
type UpdateThreadMetadataParams struct {
	// ThreadID is the thread id to update.
	ThreadID protocol.ThreadID
	// Patch is the patch to apply.
	Patch ThreadMetadataPatch
	// IncludeArchived reports whether archived threads are eligible.
	IncludeArchived bool
}

// ArchiveThreadParams holds the parameters for archiving or unarchiving a thread.
type ArchiveThreadParams struct {
	// ThreadID is the thread id to archive or unarchive.
	ThreadID protocol.ThreadID
}

// ReadOnlyPermissionProfile returns the managed read-only permission profile
// used as a default for rollout-derived threads, mirroring the Rust
// `PermissionProfile::read_only()`.
func ReadOnlyPermissionProfile() protocol.PermissionProfile {
	root := protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{
		Kind: protocol.FileSystemSpecialPathKindRoot,
	})
	fs := protocol.NewRestrictedManagedFileSystem([]protocol.FileSystemSandboxEntry{{
		Path:   root,
		Access: protocol.FileSystemAccessModeRead,
	}}, nil)
	return protocol.NewManagedPermissionProfile(fs, protocol.NetworkSandboxPolicyRestricted)
}

// OnRequestApproval returns the default on-request approval mode used for
// rollout-derived threads.
func OnRequestApproval() protocol.AskForApproval {
	return protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest}
}

// neverApproval returns the never approval mode used as the in-memory store
// default, mirroring the Rust `AskForApproval::Never`.
func neverApproval() protocol.AskForApproval {
	return protocol.AskForApproval{Kind: protocol.AskForApprovalNever}
}
