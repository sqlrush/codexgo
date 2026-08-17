package state

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// threadSelectColumns is the canonical projection used to read thread metadata.
// The millisecond timestamp columns are aliased to created_at/updated_at to
// match the Rust `push_thread_select_columns`.
const threadSelectColumns = `
SELECT
    threads.id,
    threads.rollout_path,
    threads.created_at_ms AS created_at,
    threads.updated_at_ms AS updated_at,
    threads.source,
    threads.thread_source,
    threads.agent_nickname,
    threads.agent_role,
    threads.agent_path,
    threads.model_provider,
    threads.model,
    threads.reasoning_effort,
    threads.cwd,
    threads.cli_version,
    threads.title,
    threads.preview,
    threads.sandbox_policy,
    threads.approval_mode,
    threads.tokens_used,
    threads.first_user_message,
    threads.archived_at,
    threads.git_sha,
    threads.git_branch,
    threads.git_origin_url
`

// GetThread reads the thread metadata for id, or nil when no row exists.
func (r *StateRuntime) GetThread(ctx context.Context, id protocol.ThreadID) (*ThreadMetadata, error) {
	query := threadSelectColumns + "\nFROM threads\nWHERE threads.id = ?"
	row := r.pool.QueryRowContext(ctx, query, id.String())
	meta, err := scanThreadMetadata(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get thread %s: %w", id.String(), err)
	}
	return meta, nil
}

// GetThreadMemoryMode returns the stored memory_mode for a thread, if present.
func (r *StateRuntime) GetThreadMemoryMode(ctx context.Context, id protocol.ThreadID) (*string, error) {
	var mode sql.NullString
	err := r.pool.QueryRowContext(ctx, "SELECT memory_mode FROM threads WHERE id = ?", id.String()).Scan(&mode)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get thread memory mode: %w", err)
	}
	if !mode.Valid {
		return nil, nil
	}
	v := mode.String
	return &v, nil
}

// ThreadFilterOptions controls thread listing filters and ordering, mirroring
// the Rust `ThreadFilterOptions`.
type ThreadFilterOptions struct {
	ArchivedOnly   bool
	AllowedSources []string
	ModelProviders *[]string
	CwdFilters     *[]string
	Anchor         *Anchor
	SortKey        SortKey
	SortDirection  SortDirection
	SearchTerm     *string
}

// ListThreads returns a page of thread metadata applying filters, ordering, and
// keyset pagination, mirroring the Rust `list_threads`.
func (r *StateRuntime) ListThreads(ctx context.Context, pageSize int, filters ThreadFilterOptions) (*ThreadsPage, error) {
	limit := pageSize + 1
	if limit < 1 {
		limit = 1
	}

	var sb strings.Builder
	var args []any
	sb.WriteString(threadSelectColumns)
	sb.WriteString(" FROM threads")
	pushThreadFilters(&sb, &args, filters)
	pushThreadOrderAndLimit(&sb, filters.SortKey, filters.SortDirection, limit, &args)

	rows, err := r.pool.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	defer rows.Close()

	var items []ThreadMetadata
	for rows.Next() {
		meta, err := scanThreadMetadata(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan thread row: %w", err)
		}
		items = append(items, *meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread rows: %w", err)
	}

	numScanned := len(items)
	var nextAnchor *Anchor
	if len(items) > pageSize {
		items = items[:len(items)-1]
		if len(items) > 0 {
			nextAnchor = anchorFromItem(&items[len(items)-1], filters.SortKey)
		}
	}
	return &ThreadsPage{Items: items, NextAnchor: nextAnchor, NumScannedRows: numScanned}, nil
}

// ListThreadIDs returns thread ids matching the given filters, ordered and
// limited, mirroring the Rust `list_thread_ids`.
func (r *StateRuntime) ListThreadIDs(
	ctx context.Context,
	limit int,
	anchor *Anchor,
	sortKey SortKey,
	allowedSources []string,
	modelProviders *[]string,
	archivedOnly bool,
) ([]protocol.ThreadID, error) {
	var sb strings.Builder
	var args []any
	sb.WriteString("SELECT threads.id FROM threads")
	pushThreadFilters(&sb, &args, ThreadFilterOptions{
		ArchivedOnly:   archivedOnly,
		AllowedSources: allowedSources,
		ModelProviders: modelProviders,
		Anchor:         anchor,
		SortKey:        sortKey,
		SortDirection:  SortDirectionDesc,
	})
	pushThreadOrderAndLimit(&sb, sortKey, SortDirectionDesc, limit, &args)

	rows, err := r.pool.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list thread ids: %w", err)
	}
	defer rows.Close()
	var ids []protocol.ThreadID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan thread id: %w", err)
		}
		ids = append(ids, protocol.NewThreadID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread ids: %w", err)
	}
	return ids, nil
}

// UpsertThread inserts or updates thread metadata directly, preserving existing
// non-empty preview and non-null git fields, mirroring the Rust `upsert_thread`.
func (r *StateRuntime) UpsertThread(ctx context.Context, metadata *ThreadMetadata) error {
	return r.upsertThreadWithCreationMemoryMode(ctx, metadata, nil)
}

// InsertThreadIfAbsent inserts thread metadata only when no row exists yet,
// returning whether a row was inserted, mirroring `insert_thread_if_absent`.
func (r *StateRuntime) InsertThreadIfAbsent(ctx context.Context, metadata *ThreadMetadata) (bool, error) {
	updatedAt, err := r.allocateThreadUpdatedAt(metadata.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("allocate updated_at: %w", err)
	}
	args := threadInsertArgs(metadata, updatedAt, "enabled")
	const stmt = insertThreadColumns + `
ON CONFLICT(id) DO NOTHING
`
	res, err := r.pool.ExecContext(ctx, stmt, args...)
	if err != nil {
		return false, fmt.Errorf("insert thread if absent: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return affected > 0, nil
}

func (r *StateRuntime) upsertThreadWithCreationMemoryMode(ctx context.Context, metadata *ThreadMetadata, creationMemoryMode *string) error {
	updatedAt, err := r.allocateThreadUpdatedAt(metadata.UpdatedAt)
	if err != nil {
		return fmt.Errorf("allocate updated_at: %w", err)
	}
	memoryMode := "enabled"
	if creationMemoryMode != nil {
		memoryMode = *creationMemoryMode
	}
	args := threadInsertArgs(metadata, updatedAt, memoryMode)
	const stmt = insertThreadColumns + `
ON CONFLICT(id) DO UPDATE SET
    rollout_path = excluded.rollout_path,
    created_at = excluded.created_at,
    updated_at = excluded.updated_at,
    created_at_ms = excluded.created_at_ms,
    updated_at_ms = excluded.updated_at_ms,
    source = excluded.source,
    thread_source = excluded.thread_source,
    agent_nickname = excluded.agent_nickname,
    agent_role = excluded.agent_role,
    agent_path = excluded.agent_path,
    model_provider = excluded.model_provider,
    model = excluded.model,
    reasoning_effort = excluded.reasoning_effort,
    cwd = excluded.cwd,
    cli_version = excluded.cli_version,
    title = excluded.title,
    preview = COALESCE(NULLIF(excluded.preview, ''), threads.preview),
    sandbox_policy = excluded.sandbox_policy,
    approval_mode = excluded.approval_mode,
    tokens_used = excluded.tokens_used,
    first_user_message = excluded.first_user_message,
    archived = excluded.archived,
    archived_at = excluded.archived_at,
    git_sha = COALESCE(threads.git_sha, excluded.git_sha),
    git_branch = COALESCE(threads.git_branch, excluded.git_branch),
    git_origin_url = COALESCE(threads.git_origin_url, excluded.git_origin_url)
`
	if _, err := r.pool.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("upsert thread: %w", err)
	}
	return nil
}

// SetThreadMemoryMode updates the memory_mode of an existing thread row.
func (r *StateRuntime) SetThreadMemoryMode(ctx context.Context, threadID protocol.ThreadID, memoryMode string) (bool, error) {
	res, err := r.pool.ExecContext(ctx, "UPDATE threads SET memory_mode = ? WHERE id = ?", memoryMode, threadID.String())
	if err != nil {
		return false, fmt.Errorf("set thread memory mode: %w", err)
	}
	return rowsAffectedGT0(res)
}

// UpdateThreadTitle updates the title of an existing thread row.
func (r *StateRuntime) UpdateThreadTitle(ctx context.Context, threadID protocol.ThreadID, title string) (bool, error) {
	res, err := r.pool.ExecContext(ctx, "UPDATE threads SET title = ? WHERE id = ?", title, threadID.String())
	if err != nil {
		return false, fmt.Errorf("update thread title: %w", err)
	}
	return rowsAffectedGT0(res)
}

// SetThreadPreviewIfEmpty sets a thread's preview only when it is currently
// empty, trimming the supplied value, mirroring `set_thread_preview_if_empty`.
func (r *StateRuntime) SetThreadPreviewIfEmpty(ctx context.Context, threadID protocol.ThreadID, preview string) (bool, error) {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return false, nil
	}
	res, err := r.pool.ExecContext(ctx,
		"UPDATE threads SET preview = ? WHERE id = ? AND preview = ''",
		preview, threadID.String())
	if err != nil {
		return false, fmt.Errorf("set thread preview if empty: %w", err)
	}
	return rowsAffectedGT0(res)
}

// DeleteThread deletes a thread metadata row by id, returning the number of
// rows removed.
func (r *StateRuntime) DeleteThread(ctx context.Context, threadID protocol.ThreadID) (int64, error) {
	res, err := r.pool.ExecContext(ctx, "DELETE FROM threads WHERE id = ?", threadID.String())
	if err != nil {
		return 0, fmt.Errorf("delete thread: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return affected, nil
}

// ApplyRolloutItems applies rollout items incrementally to a thread's metadata
// and upserts the result, mirroring the Rust `apply_rollout_items`.
//
// updatedAtOverride, when non-nil, overrides the persisted updated_at; otherwise
// the caller is expected to set metadata via the builder's UpdatedAt.
func (r *StateRuntime) ApplyRolloutItems(
	ctx context.Context,
	builder *ThreadMetadataBuilder,
	items []rollout.RolloutItem,
	newThreadMemoryMode *string,
	updatedAtOverride *time.Time,
) error {
	if len(items) == 0 {
		return nil
	}
	existing, err := r.GetThread(ctx, builder.ID)
	if err != nil {
		return fmt.Errorf("apply rollout items: %w", err)
	}
	var metadata ThreadMetadata
	if existing != nil {
		metadata = *existing
	} else {
		metadata = builder.Build(r.defaultProvider)
	}
	metadata.RolloutPath = builder.RolloutPath
	for i := range items {
		ApplyRolloutItem(&metadata, &items[i], r.defaultProvider)
	}
	if existing != nil {
		metadata.PreferExistingGitInfo(existing)
	}
	if updatedAtOverride != nil {
		metadata.UpdatedAt = *updatedAtOverride
	}
	if existing == nil {
		err = r.upsertThreadWithCreationMemoryMode(ctx, &metadata, newThreadMemoryMode)
	} else {
		err = r.UpsertThread(ctx, &metadata)
	}
	if err != nil {
		return err
	}
	if mode := ExtractMemoryMode(items); mode != nil {
		if _, err := r.SetThreadMemoryMode(ctx, builder.ID, *mode); err != nil {
			return err
		}
	}
	return nil
}

// ExtractMemoryMode returns the last memory_mode found across session_meta
// items, mirroring the Rust `extract_memory_mode`.
func ExtractMemoryMode(items []rollout.RolloutItem) *string {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Kind == rollout.RolloutItemKindSessionMeta && item.SessionMeta != nil {
			if mode := item.SessionMeta.Meta.MemoryMode; mode != nil {
				v := *mode
				return &v
			}
		}
	}
	return nil
}

func pushThreadFilters(sb *strings.Builder, args *[]any, options ThreadFilterOptions) {
	sb.WriteString(" WHERE 1 = 1")
	if options.ArchivedOnly {
		sb.WriteString(" AND threads.archived = 1")
	} else {
		sb.WriteString(" AND threads.archived = 0")
	}
	sb.WriteString(" AND threads.preview <> ''")
	if len(options.AllowedSources) > 0 {
		sb.WriteString(" AND threads.source IN (")
		writePlaceholders(sb, args, anySlice(options.AllowedSources))
		sb.WriteString(")")
	}
	if options.ModelProviders != nil && len(*options.ModelProviders) > 0 {
		sb.WriteString(" AND threads.model_provider IN (")
		writePlaceholders(sb, args, anySlice(*options.ModelProviders))
		sb.WriteString(")")
	}
	if options.CwdFilters != nil {
		if len(*options.CwdFilters) == 0 {
			sb.WriteString(" AND 1 = 0")
		} else {
			sb.WriteString(" AND threads.cwd IN (")
			writePlaceholders(sb, args, anySlice(*options.CwdFilters))
			sb.WriteString(")")
		}
	}
	if options.SearchTerm != nil {
		sb.WriteString(" AND (instr(threads.title, ?) > 0 OR instr(threads.preview, ?) > 0)")
		*args = append(*args, *options.SearchTerm, *options.SearchTerm)
	}
	if options.Anchor != nil {
		anchorTS := datetimeToEpochMillis(options.Anchor.TS)
		column := "threads.created_at_ms"
		if options.SortKey == SortKeyUpdatedAt {
			column = "threads.updated_at_ms"
		}
		operator := "<"
		if options.SortDirection == SortDirectionAsc {
			operator = ">"
		}
		sb.WriteString(" AND (")
		sb.WriteString(column)
		sb.WriteString(" ")
		sb.WriteString(operator)
		sb.WriteString(" ?)")
		*args = append(*args, anchorTS)
	}
}

func pushThreadOrderAndLimit(sb *strings.Builder, sortKey SortKey, sortDirection SortDirection, limit int, args *[]any) {
	orderColumn := "threads.created_at_ms"
	if sortKey == SortKeyUpdatedAt {
		orderColumn = "threads.updated_at_ms"
	}
	orderDirection := "DESC"
	if sortDirection == SortDirectionAsc {
		orderDirection = "ASC"
	}
	sb.WriteString(" ORDER BY ")
	sb.WriteString(orderColumn)
	sb.WriteString(" ")
	sb.WriteString(orderDirection)
	sb.WriteString(" LIMIT ?")
	*args = append(*args, int64(limit))
}

func writePlaceholders(sb *strings.Builder, args *[]any, values []any) {
	for i, v := range values {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("?")
		*args = append(*args, v)
	}
}

func anySlice(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func rowsAffectedGT0(res sql.Result) (bool, error) {
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return affected > 0, nil
}
