package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

// fakeStore is a minimal ThreadStore for picker tests: it serves a fixed page
// from ListThreads and reports every other operation as unsupported.
type fakeStore struct {
	threadstore.ThreadStore // nil embedded: unimplemented methods panic if called
	page                    threadstore.ThreadPage
	err                     error
	lastParams              threadstore.ListThreadsParams
}

func (f *fakeStore) ListThreads(_ context.Context, p threadstore.ListThreadsParams) (threadstore.ThreadPage, error) {
	f.lastParams = p
	if f.err != nil {
		return threadstore.ThreadPage{}, f.err
	}
	return f.page, nil
}

func storedThread(id, preview, cwd, branch string, created time.Time) threadstore.StoredThread {
	st := threadstore.StoredThread{
		ThreadID:  protocol.NewThreadID(id),
		Preview:   preview,
		Cwd:       cwd,
		CreatedAt: created,
		UpdatedAt: created,
	}
	if branch != "" {
		b := branch
		st.GitInfo = &threadstore.GitInfo{Branch: &b}
	}
	return st
}

func loadedPicker(t *testing.T, store *fakeStore, cfg ResumePickerConfig) ResumePicker {
	t.Helper()
	cfg.Store = store
	p := NewResumePicker(cfg)
	cmd := p.Init()
	if cmd == nil {
		t.Fatalf("Init returned nil command")
	}
	msg := cmd()
	next, _ := p.Update(msg)
	return next
}

func TestResumePickerLoadsRows(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{page: threadstore.ThreadPage{Items: []threadstore.StoredThread{
		storedThread("11111111-1111-1111-1111-111111111111", "first session", "/work/a", "main", now),
		storedThread("22222222-2222-2222-2222-222222222222", "second session", "/work/b", "dev", now),
	}}}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, ShowAll: true})
	if len(p.filteredRows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(p.filteredRows))
	}
}

func TestResumePickerSearchFilters(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{page: threadstore.ThreadPage{Items: []threadstore.StoredThread{
		storedThread("11111111-1111-1111-1111-111111111111", "fix the parser", "/work/a", "main", now),
		storedThread("22222222-2222-2222-2222-222222222222", "write docs", "/work/b", "dev", now),
	}}}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, ShowAll: true})

	p, _ = p.Update(keyMsg("d")) // "d"
	p, _ = p.Update(keyMsg("o")) // "do"
	p, _ = p.Update(keyMsg("c")) // "doc"
	if len(p.filteredRows) != 1 {
		t.Fatalf("expected 1 match for 'doc', got %d", len(p.filteredRows))
	}
	if p.filteredRows[0].preview != "write docs" {
		t.Fatalf("unexpected match: %q", p.filteredRows[0].preview)
	}

	// Escape with a non-empty query clears the query rather than exiting.
	p, _ = p.Update(keyMsg("esc"))
	if p.query != "" {
		t.Fatalf("esc should clear query, got %q", p.query)
	}
	if p.Selection() != nil {
		t.Fatalf("esc with query should not resolve a selection")
	}
}

func TestResumePickerEscEmptyStartsFresh(t *testing.T) {
	store := &fakeStore{}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, ShowAll: true})
	p, _ = p.Update(keyMsg("esc"))
	sel := p.Selection()
	if sel == nil || sel.Kind != SelectionStartFresh {
		t.Fatalf("esc with empty query should StartFresh, got %+v", sel)
	}
}

func TestResumePickerCtrlCExits(t *testing.T) {
	store := &fakeStore{}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, ShowAll: true})
	p, _ = p.Update(keyMsg("ctrl+c"))
	sel := p.Selection()
	if sel == nil || sel.Kind != SelectionExit {
		t.Fatalf("ctrl+c should Exit, got %+v", sel)
	}
}

func TestResumePickerAcceptResume(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{page: threadstore.ThreadPage{Items: []threadstore.StoredThread{
		storedThread("11111111-1111-1111-1111-111111111111", "first", "/work/a", "main", now),
	}}}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, ShowAll: true})
	p, _ = p.Update(keyMsg("enter"))
	sel := p.Selection()
	if sel == nil || sel.Kind != SelectionResume {
		t.Fatalf("enter should Resume, got %+v", sel)
	}
	if sel.Target.ThreadID.String() != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected target thread: %s", sel.Target.ThreadID)
	}
}

func TestResumePickerAcceptFork(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{page: threadstore.ThreadPage{Items: []threadstore.StoredThread{
		storedThread("11111111-1111-1111-1111-111111111111", "first", "/work/a", "main", now),
	}}}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionFork, ShowAll: true})
	p, _ = p.Update(keyMsg("enter"))
	sel := p.Selection()
	if sel == nil || sel.Kind != SelectionFork {
		t.Fatalf("enter should Fork, got %+v", sel)
	}
}

func TestResumePickerNavigation(t *testing.T) {
	now := time.Now().UTC()
	var items []threadstore.StoredThread
	for _, id := range []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"33333333-3333-3333-3333-333333333333",
	} {
		items = append(items, storedThread(id, "session "+id[:1], "/w", "main", now))
	}
	store := &fakeStore{page: threadstore.ThreadPage{Items: items}}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, ShowAll: true})
	if p.selected != 0 {
		t.Fatalf("selection should start at 0")
	}
	p, _ = p.Update(keyMsg("down"))
	p, _ = p.Update(keyMsg("down"))
	if p.selected != 2 {
		t.Fatalf("expected selection 2, got %d", p.selected)
	}
	p, _ = p.Update(keyMsg("down")) // clamp at last
	if p.selected != 2 {
		t.Fatalf("selection should clamp at last, got %d", p.selected)
	}
	p, _ = p.Update(keyMsg("up"))
	if p.selected != 1 {
		t.Fatalf("expected selection 1 after up, got %d", p.selected)
	}
}

func TestResumePickerToolbarToggles(t *testing.T) {
	store := &fakeStore{}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, FilterCwd: "/work"})
	if p.filterMode != FilterCwd {
		t.Fatalf("cwd-filter picker should default to FilterCwd")
	}
	if p.sortKey != SortUpdatedAt {
		t.Fatalf("default sort should be updated-at")
	}
	// Focus is Filter by default; left/right toggles its value.
	p, _ = p.Update(keyMsg("left"))
	if p.filterMode != FilterAll {
		t.Fatalf("toggling filter should switch to FilterAll")
	}
	// Tab to Sort, then toggle.
	p, _ = p.Update(keyMsg("tab"))
	if p.toolbar != toolbarSort {
		t.Fatalf("tab should focus the Sort control")
	}
	p, _ = p.Update(keyMsg("right"))
	if p.sortKey != SortCreatedAt {
		t.Fatalf("toggling sort should switch to created-at")
	}
}

func TestResumePickerDensityToggle(t *testing.T) {
	store := &fakeStore{}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, ShowAll: true})
	if p.density != DensityComfortable {
		t.Fatalf("default density should be comfortable")
	}
	p, _ = p.Update(keyMsg("ctrl+o"))
	if p.density != DensityDense {
		t.Fatalf("ctrl+o should switch to dense")
	}
}

func TestResumePickerLoadError(t *testing.T) {
	store := &fakeStore{err: errors.New("boom")}
	p := NewResumePicker(ResumePickerConfig{Store: store, Action: ActionResume, ShowAll: true})
	msg := p.Init()()
	p, _ = p.Update(msg)
	if p.inlineError == "" {
		t.Fatalf("load error should surface as inline error")
	}
}

func TestResumePickerViewRenders(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeStore{page: threadstore.ThreadPage{Items: []threadstore.StoredThread{
		storedThread("11111111-1111-1111-1111-111111111111", "hello world", "/work/a", "main", now),
	}}}
	p := loadedPicker(t, store, ResumePickerConfig{Action: ActionResume, ShowAll: true})
	out := p.View(DefaultTheme(Capabilities{}))
	if !tuiTextContains(out, "Resume a previous session") {
		t.Fatalf("view should contain the title, got:\n%s", out)
	}
	if !tuiTextContains(out, "hello world") {
		t.Fatalf("view should contain the session preview, got:\n%s", out)
	}
}

func TestFormatRelativeTime(t *testing.T) {
	ref := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ts   time.Time
		want string
	}{
		{time.Time{}, "-"},
		{ref, "now"},
		{ref.Add(-30 * time.Second), "30s ago"},
		{ref.Add(-5 * time.Minute), "5m ago"},
		{ref.Add(-3 * time.Hour), "3h ago"},
		{ref.Add(-48 * time.Hour), "2d ago"},
	}
	for _, tc := range tests {
		if got := FormatRelativeTime(ref, tc.ts); got != tc.want {
			t.Errorf("FormatRelativeTime(%v) = %q, want %q", tc.ts, got, tc.want)
		}
	}
}

func TestSessionTargetDisplayLabel(t *testing.T) {
	withPath := SessionTarget{Path: "/x/rollout.jsonl", ThreadID: protocol.NewThreadID("abc")}
	if withPath.DisplayLabel() != "/x/rollout.jsonl" {
		t.Fatalf("path target label = %q", withPath.DisplayLabel())
	}
	noPath := SessionTarget{ThreadID: protocol.NewThreadID("abc")}
	if noPath.DisplayLabel() != "thread abc" {
		t.Fatalf("no-path target label = %q", noPath.DisplayLabel())
	}
}
