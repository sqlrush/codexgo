package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/state"
	"github.com/sqlrush/codexgo/pkg/rollout"
	"github.com/sqlrush/codexgo/pkg/threadstore"
)

// listScanFallbackPageMax bounds how many rollout files the scan fallback reads
// when no state DB is available, mirroring the bounded scan budget used in the
// Rust search path.
const listScanFallbackPageMax = 2048

// listThreads lists stored thread summaries, mirroring the Rust `list_threads`.
//
// It prefers the state DB (when present and populated) for ordering and keyset
// paging, and falls back to scanning the on-disk sessions tree when the store
// has no DB or the DB returns no rows. The result honors the requested ordering
// (created_at/updated_at, asc/desc), the archived filter, and paging.
func (s *LocalThreadStore) listThreads(ctx context.Context, params threadstore.ListThreadsParams) (threadstore.ThreadPage, error) {
	pageSize := normalizePageSize(params.PageSize)

	if page, ok, err := s.listThreadsFromStateDB(ctx, params, pageSize); err != nil {
		return threadstore.ThreadPage{}, err
	} else if ok {
		return page, nil
	}

	return s.listThreadsFromScan(ctx, params, pageSize)
}

// listThreadsFromStateDB lists summaries from the state DB. The second return is
// false (with no error) when the store has no DB or the DB yields no rows, so
// the caller can fall back to a filesystem scan.
func (s *LocalThreadStore) listThreadsFromStateDB(ctx context.Context, params threadstore.ListThreadsParams, pageSize int) (threadstore.ThreadPage, bool, error) {
	if s.stateDB == nil {
		return threadstore.ThreadPage{}, false, nil
	}

	anchor, err := anchorFromCursor(params.Cursor)
	if err != nil {
		return threadstore.ThreadPage{}, false, err
	}

	filters := state.ThreadFilterOptions{
		ArchivedOnly:   params.Archived,
		AllowedSources: sessionSourceStrings(params.AllowedSources),
		ModelProviders: params.ModelProviders,
		CwdFilters:     params.CwdFilters,
		Anchor:         anchor,
		SortKey:        toStateSortKey(params.SortKey),
		SortDirection:  toStateSortDirection(params.SortDirection),
		SearchTerm:     params.SearchTerm,
	}

	dbPage, err := s.stateDB.ListThreads(ctx, pageSize, filters)
	if err != nil {
		return threadstore.ThreadPage{}, false, threadstore.NewInternalError(err, "failed to list threads")
	}
	if dbPage == nil || len(dbPage.Items) == 0 {
		return threadstore.ThreadPage{}, false, nil
	}

	items := make([]threadstore.StoredThread, 0, len(dbPage.Items))
	for i := range dbPage.Items {
		items = append(items, s.storedThreadFromSQLiteMetadata(&dbPage.Items[i]))
	}

	return threadstore.ThreadPage{Items: items, NextCursor: cursorFromAnchor(dbPage.NextAnchor)}, true, nil
}

// listThreadsFromScan lists summaries by scanning the on-disk sessions tree,
// mirroring the Rust local rollout-summary path used when no SQLite index is
// available. It applies the source/provider/cwd filters, ordering, and paging in
// memory.
func (s *LocalThreadStore) listThreadsFromScan(ctx context.Context, params threadstore.ListThreadsParams, pageSize int) (threadstore.ThreadPage, error) {
	subdir := rollout.SessionsSubdir
	if params.Archived {
		subdir = rollout.ArchivedSessionsSubdir
	}
	paths, err := scanRolloutPaths(filepath.Join(s.config.CodexHome, subdir))
	if err != nil {
		return threadstore.ThreadPage{}, threadstore.NewInternalError(err, "failed to scan sessions for thread listing")
	}

	threads := make([]threadstore.StoredThread, 0, len(paths))
	for _, path := range paths {
		thread, rerr := s.readThreadFromRolloutPath(ctx, path)
		if rerr != nil {
			continue
		}
		if !threadMatchesListFilters(thread, params) {
			continue
		}
		threads = append(threads, thread)
	}

	sortStoredThreads(threads, params.SortKey, params.SortDirection)
	return paginateStoredThreads(threads, params, pageSize), nil
}

// threadSearchSnippet reports whether thread matches the lowered query in its
// title, preview, or cwd, returning the matching field value as the snippet.
func threadSearchSnippet(thread threadstore.StoredThread, loweredTerm string) (string, bool) {
	if thread.Name != nil {
		if strings.Contains(strings.ToLower(*thread.Name), loweredTerm) {
			return *thread.Name, true
		}
	}
	if strings.Contains(strings.ToLower(thread.Preview), loweredTerm) {
		return thread.Preview, true
	}
	if strings.Contains(strings.ToLower(thread.Cwd), loweredTerm) {
		return thread.Cwd, true
	}
	if thread.FirstUserMessage != nil {
		if strings.Contains(strings.ToLower(*thread.FirstUserMessage), loweredTerm) {
			return *thread.FirstUserMessage, true
		}
	}
	return "", false
}

// threadMatchesListFilters reports whether thread satisfies the source,
// model-provider, cwd, archived, and search filters supplied in params.
func threadMatchesListFilters(thread threadstore.StoredThread, params threadstore.ListThreadsParams) bool {
	if params.Archived != (thread.ArchivedAt != nil) {
		return false
	}
	// The state DB only lists rows with a non-empty preview; mirror that so the
	// scan fallback and the DB path agree.
	if strings.TrimSpace(thread.Preview) == "" {
		return false
	}
	if len(params.AllowedSources) > 0 && !sourceAllowed(thread.Source, params.AllowedSources) {
		return false
	}
	if params.ModelProviders != nil && !containsString(*params.ModelProviders, thread.ModelProvider) {
		return false
	}
	if params.CwdFilters != nil && !containsString(*params.CwdFilters, thread.Cwd) {
		return false
	}
	if params.SearchTerm != nil {
		term := strings.ToLower(strings.TrimSpace(*params.SearchTerm))
		if term != "" {
			if _, ok := threadSearchSnippet(thread, term); !ok {
				return false
			}
		}
	}
	return true
}

// sortStoredThreads sorts threads in place by the requested key and direction.
func sortStoredThreads(threads []threadstore.StoredThread, key threadstore.ThreadSortKey, direction threadstore.SortDirection) {
	sort.SliceStable(threads, func(i, j int) bool {
		a := sortTimestamp(threads[i], key)
		b := sortTimestamp(threads[j], key)
		if a.Equal(b) {
			// Stable tie-break on id keeps paging deterministic.
			if direction == threadstore.SortDirectionAsc {
				return threads[i].ThreadID.String() < threads[j].ThreadID.String()
			}
			return threads[i].ThreadID.String() > threads[j].ThreadID.String()
		}
		if direction == threadstore.SortDirectionAsc {
			return a.Before(b)
		}
		return a.After(b)
	})
}

// paginateStoredThreads applies the cursor and page size to a sorted slice and
// returns the resulting page with the next cursor.
func paginateStoredThreads(threads []threadstore.StoredThread, params threadstore.ListThreadsParams, pageSize int) threadstore.ThreadPage {
	start := 0
	if params.Cursor != nil {
		if anchor, err := anchorFromCursor(params.Cursor); err == nil && anchor != nil {
			start = cursorStartIndex(threads, anchor.TS, params.SortKey, params.SortDirection)
		}
	}
	if start > len(threads) {
		start = len(threads)
	}

	end := start + pageSize
	var nextCursor *string
	if end < len(threads) {
		cursor := cursorFromStoredThread(threads[end-1], params.SortKey)
		nextCursor = &cursor
	} else {
		end = len(threads)
	}

	page := make([]threadstore.StoredThread, end-start)
	copy(page, threads[start:end])
	return threadstore.ThreadPage{Items: page, NextCursor: nextCursor}
}

// cursorStartIndex returns the index of the first thread strictly after the
// cursor timestamp under the given ordering.
func cursorStartIndex(threads []threadstore.StoredThread, cursorTS time.Time, key threadstore.ThreadSortKey, direction threadstore.SortDirection) int {
	for i := range threads {
		ts := sortTimestamp(threads[i], key)
		if direction == threadstore.SortDirectionAsc {
			if ts.After(cursorTS) {
				return i
			}
		} else if ts.Before(cursorTS) {
			return i
		}
	}
	return len(threads)
}

// sortTimestamp returns the timestamp a thread is ordered by for the given key.
func sortTimestamp(thread threadstore.StoredThread, key threadstore.ThreadSortKey) time.Time {
	if key == threadstore.ThreadSortKeyUpdatedAt {
		return thread.UpdatedAt
	}
	return thread.CreatedAt
}

// cursorFromStoredThread encodes a page cursor from the sort timestamp of a
// stored thread.
func cursorFromStoredThread(thread threadstore.StoredThread, key threadstore.ThreadSortKey) string {
	return encodeCursor(sortTimestamp(thread, key))
}

// scanRolloutPaths walks root and returns the absolute paths of every
// `rollout-*.jsonl` file found, bounded by listScanFallbackPageMax.
func scanRolloutPaths(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat sessions root: %w", err)
	}

	var paths []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") {
			paths = append(paths, path)
			if len(paths) >= listScanFallbackPageMax {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk sessions tree: %w", walkErr)
	}
	return paths, nil
}

// normalizePageSize clamps a requested page size to a sane minimum.
func normalizePageSize(pageSize int) int {
	if pageSize < 1 {
		return 1
	}
	return pageSize
}

// clampInt clamps v to the inclusive range [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// toStateSortKey converts a thread-store sort key to the state package key.
func toStateSortKey(key threadstore.ThreadSortKey) state.SortKey {
	if key == threadstore.ThreadSortKeyUpdatedAt {
		return state.SortKeyUpdatedAt
	}
	return state.SortKeyCreatedAt
}

// toStateSortDirection converts a thread-store direction to the state direction.
func toStateSortDirection(direction threadstore.SortDirection) state.SortDirection {
	if direction == threadstore.SortDirectionAsc {
		return state.SortDirectionAsc
	}
	return state.SortDirectionDesc
}

// sessionSourceStrings renders allowed session sources as their stored strings.
func sessionSourceStrings(sources []rollout.SessionSource) []string {
	if len(sources) == 0 {
		return nil
	}
	out := make([]string, 0, len(sources))
	for _, source := range sources {
		out = append(out, source.String())
	}
	return out
}

// sourceAllowed reports whether source's stored string is in allowed.
func sourceAllowed(source rollout.SessionSource, allowed []rollout.SessionSource) bool {
	target := source.String()
	for _, candidate := range allowed {
		if candidate.String() == target {
			return true
		}
	}
	return false
}

// containsString reports whether values contains target.
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// anchorFromCursor decodes an optional list cursor into a state Anchor.
func anchorFromCursor(cursor *string) (*state.Anchor, error) {
	if cursor == nil {
		return nil, nil
	}
	ts, ok := decodeCursor(*cursor)
	if !ok {
		return nil, threadstore.NewInvalidRequestError("invalid cursor: %s", *cursor)
	}
	return &state.Anchor{TS: ts}, nil
}

// cursorFromAnchor encodes a state Anchor as an opaque list cursor.
func cursorFromAnchor(anchor *state.Anchor) *string {
	if anchor == nil {
		return nil
	}
	cursor := encodeCursor(anchor.TS)
	return &cursor
}

// encodeCursor renders a timestamp as the opaque RFC3339Nano cursor used to
// continue listing, matching the codex cursor-as-timestamp convention.
func encodeCursor(ts time.Time) string {
	return ts.UTC().Format(time.RFC3339Nano)
}

// decodeCursor parses an opaque cursor back into a timestamp.
func decodeCursor(cursor string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, cursor)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}
