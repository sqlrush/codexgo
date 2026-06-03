package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/threadstore"
)

// This file ports codex-rs/tui/src/resume_picker.rs: the interactive session
// picker used by `/resume`, `--resume`, and `--fork`. It lists stored threads
// (via internal/threadstore), supports incremental search, cwd/all filtering,
// created/updated sort, comfortable/dense density, and selection that yields a
// resume- or fork-targeted [SessionSelection].
//
// Scope: this is a behavioral port of the deterministic picker state machine and
// rendering. Upstream extras that depend on the app-server transport (lazy
// transcript previews, transcript pager overlay, cursor-based pagination with a
// background loader) are simplified to a single synchronous page load via the
// store, which is sufficient for the local threadstore backend. These omissions
// are documented as deviations rather than stubbed UI.

const (
	// PickerPageSize is the per-page thread count (port of PAGE_SIZE).
	PickerPageSize = 25

	sessionMetaBranchIcon = "" // port of SESSION_META_BRANCH_ICON (nerd-font branch glyph)
	sessionMetaCwdIcon    = "⌁"

	sessionMetaIndentWidth  = 2
	sessionMetaDateWidth    = 12
	sessionMetaFieldGap     = 2
	sessionMetaMinCwdWidth  = 30
	sessionMetaMaxCwdWidth  = 72
	pickerListHorizInset    = 4
	pickerDefaultPageStride = 10
)

// --- selection result types (port of SessionTarget/SessionSelection) ---------

// SessionTarget identifies a chosen session.
type SessionTarget struct {
	// Path is the local rollout path when known.
	Path string
	// ThreadID is the thread id of the session.
	ThreadID protocol.ThreadID
}

// DisplayLabel returns a human label for the target (port of
// SessionTarget::display_label).
func (t SessionTarget) DisplayLabel() string {
	if t.Path != "" {
		return t.Path
	}
	return fmt.Sprintf("thread %s", t.ThreadID)
}

// SessionSelectionKind enumerates the picker outcomes.
type SessionSelectionKind int

const (
	// SelectionStartFresh starts a new session.
	SelectionStartFresh SessionSelectionKind = iota
	// SelectionResume resumes the target session.
	SelectionResume
	// SelectionFork forks the target session.
	SelectionFork
	// SelectionExit exits without starting a session.
	SelectionExit
)

// SessionSelection is the picker's outcome (port of SessionSelection).
type SessionSelection struct {
	Kind   SessionSelectionKind
	Target SessionTarget
}

// SessionPickerAction selects whether the picker resumes or forks (port of
// SessionPickerAction).
type SessionPickerAction int

const (
	// ActionResume yields a resume selection on accept.
	ActionResume SessionPickerAction = iota
	// ActionFork yields a fork selection on accept.
	ActionFork
)

// Title returns the picker header title (port of SessionPickerAction::title).
func (a SessionPickerAction) Title() string {
	if a == ActionFork {
		return "Fork a previous session"
	}
	return "Resume a previous session"
}

// selection builds the outcome for the action (port of
// SessionPickerAction::selection).
func (a SessionPickerAction) selection(t SessionTarget) SessionSelection {
	if a == ActionFork {
		return SessionSelection{Kind: SelectionFork, Target: t}
	}
	return SessionSelection{Kind: SelectionResume, Target: t}
}

// --- filter / sort / density toggles (port of the toolbar enums) -------------

// SessionFilterMode is the cwd-vs-all filter (port of SessionFilterMode).
type SessionFilterMode int

const (
	// FilterCwd shows sessions for the current working directory.
	FilterCwd SessionFilterMode = iota
	// FilterAll shows sessions for all directories.
	FilterAll
)

// filterModeFromShowAll mirrors SessionFilterMode::from_show_all.
func filterModeFromShowAll(showAll bool, hasCwd bool) SessionFilterMode {
	if showAll || !hasCwd {
		return FilterAll
	}
	return FilterCwd
}

// toggle mirrors SessionFilterMode::toggle: All->Cwd only when a cwd exists.
func (m SessionFilterMode) toggle(hasCwd bool) SessionFilterMode {
	switch m {
	case FilterCwd:
		return FilterAll
	default:
		if hasCwd {
			return FilterCwd
		}
		return FilterAll
	}
}

// SessionSortKey is the picker sort (port of ThreadSortKey usage in the picker).
type SessionSortKey int

const (
	// SortUpdatedAt sorts by last-update time (the picker default).
	SortUpdatedAt SessionSortKey = iota
	// SortCreatedAt sorts by creation time.
	SortCreatedAt
)

func (k SessionSortKey) toggle() SessionSortKey {
	if k == SortUpdatedAt {
		return SortCreatedAt
	}
	return SortUpdatedAt
}

func (k SessionSortKey) toStoreKey() threadstore.ThreadSortKey {
	if k == SortCreatedAt {
		return threadstore.ThreadSortKeyCreatedAt
	}
	return threadstore.ThreadSortKeyUpdatedAt
}

// SessionListDensity is the row density (port of SessionListDensity).
type SessionListDensity int

const (
	// DensityComfortable renders multi-line rows (default).
	DensityComfortable SessionListDensity = iota
	// DensityDense renders single-line rows.
	DensityDense
)

func (d SessionListDensity) toggle() SessionListDensity {
	if d == DensityComfortable {
		return DensityDense
	}
	return DensityComfortable
}

// toolbarControl is the focused toolbar control (port of ToolbarControl).
type toolbarControl int

const (
	toolbarFilter toolbarControl = iota
	toolbarSort
)

func (c toolbarControl) next() toolbarControl {
	if c == toolbarFilter {
		return toolbarSort
	}
	return toolbarFilter
}

// --- rows (port of resume_picker.rs Row) -------------------------------------

// sessionRow is one listed session (port of Row).
type sessionRow struct {
	path      string
	preview   string
	threadID  protocol.ThreadID
	hasThread bool
	name      string
	createdAt time.Time
	updatedAt time.Time
	cwd       string
	gitBranch string
}

// rowFromStored builds a row from a stored thread record.
func rowFromStored(t threadstore.StoredThread) sessionRow {
	r := sessionRow{
		preview:   t.Preview,
		threadID:  t.ThreadID,
		hasThread: true,
		createdAt: t.CreatedAt,
		updatedAt: t.UpdatedAt,
		cwd:       t.Cwd,
	}
	if t.RolloutPath != nil {
		r.path = *t.RolloutPath
	}
	if t.Name != nil {
		r.name = *t.Name
	}
	if t.GitInfo != nil && t.GitInfo.Branch != nil {
		r.gitBranch = *t.GitInfo.Branch
	}
	return r
}

// displayPreview returns the thread name when set, else the preview (port of
// Row::display_preview).
func (r sessionRow) displayPreview() string {
	if r.name != "" {
		return r.name
	}
	return r.preview
}

// matchesQuery reports whether the row matches a lowercased query (port of
// Row::matches_query).
func (r sessionRow) matchesQuery(query string) bool {
	if strings.Contains(strings.ToLower(r.preview), query) {
		return true
	}
	if r.name != "" && strings.Contains(strings.ToLower(r.name), query) {
		return true
	}
	if r.hasThread && strings.Contains(strings.ToLower(r.threadID.String()), query) {
		return true
	}
	if r.gitBranch != "" && strings.Contains(strings.ToLower(r.gitBranch), query) {
		return true
	}
	if r.cwd != "" && strings.Contains(strings.ToLower(r.cwd), query) {
		return true
	}
	return false
}

// --- picker model (port of PickerState) --------------------------------------

// ResumePicker is the bubbletea model for the session picker. It is a value
// type; Update returns a new copy (immutability).
type ResumePicker struct {
	store  threadstore.ThreadStore
	action SessionPickerAction
	keymap ListKeymap

	allRows      []sessionRow
	filteredRows []sessionRow
	selected     int
	scrollTop    int
	query        string

	filterMode SessionFilterMode
	filterCwd  string
	sortKey    SessionSortKey
	density    SessionListDensity
	toolbar    toolbarControl

	viewRows    int
	viewWidth   int
	nextCursor  *string
	loadedAll   bool
	relativeRef time.Time

	inlineError string
	expanded    map[protocol.ThreadID]bool

	// done holds the final selection once the picker resolves.
	done *SessionSelection
}

// ResumePickerConfig parameterizes a new picker.
type ResumePickerConfig struct {
	// Store provides the listed sessions.
	Store threadstore.ThreadStore
	// Action selects resume vs fork behavior.
	Action SessionPickerAction
	// ShowAll lists sessions for all directories (otherwise cwd-filtered).
	ShowAll bool
	// FilterCwd is the working directory used for the cwd filter; empty disables
	// cwd filtering.
	FilterCwd string
	// Keymap supplies list navigation bindings; the zero value uses defaults.
	Keymap *ListKeymap
}

// NewResumePicker constructs a picker (port of PickerState::new with the picker
// defaults: comfortable density, updated-at sort, Filter toolbar focus).
func NewResumePicker(cfg ResumePickerConfig) ResumePicker {
	keymap := DefaultRuntimeKeymap().List
	if cfg.Keymap != nil {
		keymap = *cfg.Keymap
	}
	return ResumePicker{
		store:       cfg.Store,
		action:      cfg.Action,
		keymap:      keymap,
		filterMode:  filterModeFromShowAll(cfg.ShowAll, cfg.FilterCwd != ""),
		filterCwd:   cfg.FilterCwd,
		sortKey:     SortUpdatedAt,
		density:     DensityComfortable,
		toolbar:     toolbarFilter,
		viewRows:    pickerDefaultPageStride,
		relativeRef: time.Now().UTC(),
		expanded:    map[protocol.ThreadID]bool{},
	}
}

// Selection returns the resolved selection, or nil while the picker is active.
func (p ResumePicker) Selection() *SessionSelection { return p.done }

// Init implements tea.Model: it triggers the initial session load.
func (p ResumePicker) Init() tea.Cmd {
	return p.loadCmd()
}

// pickerPageMsg carries a loaded page back into the model.
type pickerPageMsg struct {
	rows []sessionRow
	next *string
	err  error
}

// loadCmd loads the first page of sessions from the store off the UI loop.
func (p ResumePicker) loadCmd() tea.Cmd {
	store := p.store
	if store == nil {
		return nil
	}
	params := threadstore.ListThreadsParams{
		PageSize:      PickerPageSize,
		SortKey:       p.sortKey.toStoreKey(),
		SortDirection: threadstore.SortDirectionDesc,
	}
	if p.filterMode == FilterCwd && p.filterCwd != "" {
		cwds := []string{p.filterCwd}
		params.CwdFilters = &cwds
	}
	return func() tea.Msg {
		page, err := store.ListThreads(context.Background(), params)
		if err != nil {
			return pickerPageMsg{err: err}
		}
		rows := make([]sessionRow, 0, len(page.Items))
		for _, item := range page.Items {
			rows = append(rows, rowFromStored(item))
		}
		return pickerPageMsg{rows: rows, next: page.NextCursor}
	}
}

// Update advances the picker for a message, returning the updated picker and any
// follow-up command. It is the picker analogue of tea.Model.Update but returns
// the concrete type so the host can read Selection(); View takes a [Theme], so
// the picker is not itself a bare tea.Model.
func (p ResumePicker) Update(msg tea.Msg) (ResumePicker, tea.Cmd) {
	switch msg := msg.(type) {
	case pickerPageMsg:
		if msg.err != nil {
			p.inlineError = fmt.Sprintf("Failed to load sessions: %v", msg.err)
			return p, nil
		}
		p.allRows = append(p.allRows, msg.rows...)
		p.nextCursor = msg.next
		p.loadedAll = msg.next == nil
		p.recomputeFiltered()
		return p, nil
	case tea.WindowSizeMsg:
		p.viewWidth = msg.Width
		if msg.Height > 4 {
			p.viewRows = msg.Height - 4
		}
		return p, nil
	case tea.KeyMsg:
		return p.handleKey(msg)
	default:
		return p, nil
	}
}

// handleKey routes a key event (port of PickerState::handle_key). It returns the
// updated picker and, when the picker resolves, schedules tea.Quit so a host
// loop can observe Selection().
func (p ResumePicker) handleKey(msg tea.KeyMsg) (ResumePicker, tea.Cmd) {
	p.inlineError = ""
	s := msg.String()

	switch {
	case s == "ctrl+c":
		sel := SessionSelection{Kind: SelectionExit}
		p.done = &sel
		return p, tea.Quit
	case p.keymap.Cancel.IsPressed(msg):
		if p.query == "" {
			sel := SessionSelection{Kind: SelectionStartFresh}
			p.done = &sel
			return p, tea.Quit
		}
		p.query = ""
		p.recomputeFiltered()
		return p, nil
	case s == "ctrl+e", s == "ctrl+t":
		p.toggleExpansion()
		return p, nil
	case s == "ctrl+o":
		p.density = p.density.toggle()
		return p, nil
	case p.keymap.Accept.IsPressed(msg):
		return p.accept()
	case s == "tab":
		p.toolbar = p.toolbar.next()
		return p, nil
	case s == "shift+tab":
		p.toolbar = p.toolbar.next()
		return p, nil
	case s == "backspace":
		r := []rune(p.query)
		if len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.recomputeFiltered()
		}
		return p, nil
	}

	// Navigation is allowed only when the key is not plain text destined for the
	// search query (port of allow_plain_char_navigation).
	allowNav := !IsPlainTextKeyEvent(msg)
	if allowNav {
		switch {
		case p.keymap.MoveUp.IsPressed(msg):
			if p.selected > 0 {
				p.selected--
				p.ensureVisible()
			}
			return p, nil
		case p.keymap.MoveDown.IsPressed(msg):
			if p.selected+1 < len(p.filteredRows) {
				p.selected++
				p.ensureVisible()
			}
			return p, p.maybeLoadMore()
		case p.keymap.PageUp.IsPressed(msg):
			step := max(p.viewRows, 1)
			p.selected = clampInt(p.selected-step, 0, maxInt(len(p.filteredRows)-1, 0))
			p.ensureVisible()
			return p, nil
		case p.keymap.PageDown.IsPressed(msg):
			step := max(p.viewRows, 1)
			p.selected = clampInt(p.selected+step, 0, maxInt(len(p.filteredRows)-1, 0))
			p.ensureVisible()
			return p, p.maybeLoadMore()
		case p.keymap.JumpTop.IsPressed(msg):
			if len(p.filteredRows) > 0 {
				p.selected = 0
				p.ensureVisible()
			}
			return p, nil
		case p.keymap.JumpBottom.IsPressed(msg):
			if len(p.filteredRows) > 0 {
				p.selected = len(p.filteredRows) - 1
				p.ensureVisible()
			}
			return p, p.maybeLoadMore()
		case p.keymap.MoveLeft.IsPressed(msg), p.keymap.MoveRight.IsPressed(msg):
			p.changeFocusedToolbar()
			return p, nil
		}
	}

	// Plain printable characters extend the search query (port of the Char arm).
	if isBarePrintable(msg) {
		p.query += s
		p.recomputeFiltered()
	}
	return p, nil
}

// accept resolves the current selection (port of the accept arm).
func (p ResumePicker) accept() (ResumePicker, tea.Cmd) {
	if p.selected < 0 || p.selected >= len(p.filteredRows) {
		return p, nil
	}
	row := p.filteredRows[p.selected]
	if !row.hasThread {
		if row.path != "" {
			p.inlineError = fmt.Sprintf("Failed to read session metadata from %s", row.path)
		} else {
			p.inlineError = "Failed to read session metadata from selected session"
		}
		return p, nil
	}
	sel := p.action.selection(SessionTarget{Path: row.path, ThreadID: row.threadID})
	p.done = &sel
	return p, tea.Quit
}

// changeFocusedToolbar toggles the focused toolbar value (port of
// change_focused_toolbar_value).
func (p *ResumePicker) changeFocusedToolbar() {
	switch p.toolbar {
	case toolbarFilter:
		p.filterMode = p.filterMode.toggle(p.filterCwd != "")
	case toolbarSort:
		p.sortKey = p.sortKey.toggle()
	}
	// Filter and sort change the backend query; reload from the store.
	p.allRows = nil
	p.nextCursor = nil
	p.loadedAll = false
	p.selected = 0
	p.scrollTop = 0
	p.recomputeFiltered()
}

// toggleExpansion flips the expanded preview for the selected row.
func (p *ResumePicker) toggleExpansion() {
	if p.selected < 0 || p.selected >= len(p.filteredRows) {
		return
	}
	row := p.filteredRows[p.selected]
	if !row.hasThread {
		return
	}
	p.expanded[row.threadID] = !p.expanded[row.threadID]
}

// recomputeFiltered rebuilds the visible rows from the query (port of the
// filtered_rows recompute in set_query).
func (p *ResumePicker) recomputeFiltered() {
	query := strings.ToLower(strings.TrimSpace(p.query))
	if query == "" {
		p.filteredRows = append([]sessionRow(nil), p.allRows...)
	} else {
		var out []sessionRow
		for _, r := range p.allRows {
			if r.matchesQuery(query) {
				out = append(out, r)
			}
		}
		p.filteredRows = out
	}
	if p.selected >= len(p.filteredRows) {
		p.selected = maxInt(len(p.filteredRows)-1, 0)
	}
	p.ensureVisible()
}

// ensureVisible scrolls so the selected row is within the viewport (port of
// ensure_selected_visible).
func (p *ResumePicker) ensureVisible() {
	if p.viewRows <= 0 {
		p.scrollTop = p.selected
		return
	}
	if p.selected < p.scrollTop {
		p.scrollTop = p.selected
	} else if p.selected >= p.scrollTop+p.viewRows {
		p.scrollTop = p.selected - p.viewRows + 1
	}
	if p.scrollTop < 0 {
		p.scrollTop = 0
	}
}

// maybeLoadMore loads the next page when near the bottom (port of
// maybe_load_more_for_scroll). With the synchronous store this loads the next
// page; the store cursor drives termination.
func (p ResumePicker) maybeLoadMore() tea.Cmd {
	if p.loadedAll || p.nextCursor == nil || p.store == nil {
		return nil
	}
	if p.selected < len(p.filteredRows)-5 {
		return nil
	}
	cursor := p.nextCursor
	store := p.store
	params := threadstore.ListThreadsParams{
		PageSize:      PickerPageSize,
		Cursor:        cursor,
		SortKey:       p.sortKey.toStoreKey(),
		SortDirection: threadstore.SortDirectionDesc,
	}
	if p.filterMode == FilterCwd && p.filterCwd != "" {
		cwds := []string{p.filterCwd}
		params.CwdFilters = &cwds
	}
	return func() tea.Msg {
		page, err := store.ListThreads(context.Background(), params)
		if err != nil {
			return pickerPageMsg{err: err}
		}
		rows := make([]sessionRow, 0, len(page.Items))
		for _, item := range page.Items {
			rows = append(rows, rowFromStored(item))
		}
		return pickerPageMsg{rows: rows, next: page.NextCursor}
	}
}

// --- small numeric helpers ---------------------------------------------------

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
