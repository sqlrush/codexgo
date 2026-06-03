package tui

import tea "github.com/charmbracelet/bubbletea"

// FileMatch is one result of an @file search; Path is relative to the search
// directory and Indices are char offsets to highlight.
//
// Port of codex_file_search::FileMatch (reduced to TUI-relevant fields).
type FileMatch struct {
	Path    string
	Indices []int
}

// FileSearchPopup is the @file mention search popup.
//
// Port of file_search_popup.rs FileSearchPopup. It tracks the pending query (the
// latest typed), the display query (the query whose matches are shown), and a
// waiting flag while a search is in-flight.
type FileSearchPopup struct {
	displayQuery string
	pendingQuery string
	waiting      bool
	matches      []FileMatch
	state        ScrollState
	complete     bool
	accepted     string
	hasAccepted  bool
}

// NewFileSearchPopup builds a popup in the initial waiting state.
//
// Port of FileSearchPopup::new.
func NewFileSearchPopup() *FileSearchPopup {
	return &FileSearchPopup{waiting: true, state: NewScrollState()}
}

// SetQuery updates the pending query and resets to the waiting state.
//
// Port of FileSearchPopup::set_query.
func (p *FileSearchPopup) SetQuery(query string) {
	if query == p.pendingQuery {
		return
	}
	p.pendingQuery = query
	p.waiting = true
}

// SetEmptyPrompt puts the popup into the idle "just @" state showing a hint.
//
// Port of FileSearchPopup::set_empty_prompt.
func (p *FileSearchPopup) SetEmptyPrompt() {
	p.displayQuery = ""
	p.pendingQuery = ""
	p.waiting = false
	p.matches = nil
	p.state.Reset()
}

// SetMatches replaces the matches when a search result arrives. The update is
// dropped (stale) unless query matches the pending query.
//
// Port of FileSearchPopup::set_matches.
func (p *FileSearchPopup) SetMatches(query string, matches []FileMatch) {
	if query != p.pendingQuery {
		return
	}
	p.displayQuery = query
	if len(matches) > MaxPopupRows {
		matches = matches[:MaxPopupRows]
	}
	p.matches = matches
	p.waiting = false
	n := len(p.matches)
	p.state.ClampSelection(n)
	p.state.EnsureVisible(n, minInt(n, MaxPopupRows))
}

// MoveUp moves the selection cursor up.
func (p *FileSearchPopup) MoveUp() {
	n := len(p.matches)
	p.state.MoveUpWrap(n)
	p.state.EnsureVisible(n, minInt(n, MaxPopupRows))
}

// MoveDown moves the selection cursor down.
func (p *FileSearchPopup) MoveDown() {
	n := len(p.matches)
	p.state.MoveDownWrap(n)
	p.state.EnsureVisible(n, minInt(n, MaxPopupRows))
}

// SelectedMatch returns the selected path, if any.
//
// Port of FileSearchPopup::selected_match.
func (p *FileSearchPopup) SelectedMatch() (string, bool) {
	if !p.state.HasSelection() || p.state.Selected >= len(p.matches) {
		return "", false
	}
	return p.matches[p.state.Selected].Path, true
}

// Accepted reports the accepted path, if a selection was confirmed.
func (p *FileSearchPopup) Accepted() (string, bool) {
	if !p.hasAccepted {
		return "", false
	}
	return p.accepted, true
}

// HandleKey implements OverlayView.
func (p *FileSearchPopup) HandleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "ctrl+p":
		p.MoveUp()
	case "down", "ctrl+n":
		p.MoveDown()
	case "enter", "tab":
		if path, ok := p.SelectedMatch(); ok {
			p.accepted = path
			p.hasAccepted = true
			p.complete = true
		}
	case "esc":
		p.complete = true
	}
	return nil
}

// IsComplete implements OverlayView.
func (p *FileSearchPopup) IsComplete() bool { return p.complete }

// DesiredHeight implements OverlayView.
//
// Port of FileSearchPopup::calculate_required_height (clamp to [1, MaxPopupRows]).
func (p *FileSearchPopup) DesiredHeight(width int) int {
	n := len(p.matches)
	if n < 1 {
		return 1
	}
	if n > MaxPopupRows {
		return MaxPopupRows
	}
	return n
}

// View implements OverlayView.
func (p *FileSearchPopup) View(theme Theme, area Rect) string {
	rows := make([]DisplayRow, 0, len(p.matches))
	for _, m := range p.matches {
		rows = append(rows, DisplayRow{Name: m.Path, MatchIndices: m.Indices})
	}
	empty := "no matches"
	if p.waiting {
		empty = "loading..."
	}
	return renderRows(theme, rows, p.state, MaxPopupRows, empty)
}

var _ OverlayView = (*FileSearchPopup)(nil)
