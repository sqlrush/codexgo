package tui

// MaxPopupRows is the maximum number of rows any popup attempts to display.
// Keep this consistent across all popups for a uniform feel.
//
// Port of bottom_pane/popup_consts.rs MAX_POPUP_ROWS.
const MaxPopupRows = 8

// ScrollState is the generic scroll/selection state for a vertical list menu.
//
// It encapsulates the common behavior of a selectable list that supports:
//   - Optional selection (Selected < 0 when the list is empty)
//   - Wrap-around navigation on up/down
//   - Maintaining a scroll window (ScrollTop) so the selected row stays visible
//
// Callers own the filtered row count and visible window size; every mutation
// method takes those values instead of caching them so list views can apply
// filters or density changes without this helper knowing their data model.
//
// Port of bottom_pane/scroll_state.rs ScrollState. The Rust Option<usize>
// selection is modeled here as an int that is negative when there is no
// selection (HasSelection reports presence).
type ScrollState struct {
	// Selected is the selected index, or -1 when nothing is selected.
	Selected int
	// ScrollTop is the index of the first visible row.
	ScrollTop int
}

// NewScrollState returns an empty scroll state with no selection.
func NewScrollState() ScrollState {
	return ScrollState{Selected: -1, ScrollTop: 0}
}

// HasSelection reports whether a row is currently selected.
func (s ScrollState) HasSelection() bool { return s.Selected >= 0 }

// Reset clears selection and scroll.
func (s *ScrollState) Reset() {
	s.Selected = -1
	s.ScrollTop = 0
}

// ClampSelection clamps the selection into [0, length-1], or clears it when the
// list is empty.
func (s *ScrollState) ClampSelection(length int) {
	if s.clearIfEmpty(length) {
		return
	}
	cur := s.Selected
	if cur < 0 {
		cur = 0
	}
	if cur > length-1 {
		cur = length - 1
	}
	s.Selected = cur
}

// MoveUpWrap moves the selection up by one, wrapping to the bottom.
func (s *ScrollState) MoveUpWrap(length int) {
	if s.clearIfEmpty(length) {
		return
	}
	switch {
	case s.Selected > 0:
		s.Selected--
	case s.Selected == 0:
		s.Selected = length - 1
	default:
		s.Selected = 0
	}
}

// MoveDownWrap moves the selection down by one, wrapping to the top.
func (s *ScrollState) MoveDownWrap(length int) {
	if s.clearIfEmpty(length) {
		return
	}
	if s.Selected >= 0 && s.Selected+1 < length {
		s.Selected++
		return
	}
	s.Selected = 0
}

// PageUpClamped moves the selection up by one visible page, clamping at the
// first row (no wrap).
func (s *ScrollState) PageUpClamped(length, visibleRows int) {
	if s.clearIfEmpty(length) {
		return
	}
	step := visibleRows
	if step < 1 {
		step = 1
	}
	cur := s.clampedCurrent(length)
	cur -= step
	if cur < 0 {
		cur = 0
	}
	s.Selected = cur
	s.EnsureVisible(length, visibleRows)
}

// PageDownClamped moves the selection down by one visible page, clamping at the
// last row (no wrap).
func (s *ScrollState) PageDownClamped(length, visibleRows int) {
	if s.clearIfEmpty(length) {
		return
	}
	step := visibleRows
	if step < 1 {
		step = 1
	}
	cur := s.clampedCurrent(length)
	cur += step
	if cur > length-1 {
		cur = length - 1
	}
	s.Selected = cur
	s.EnsureVisible(length, visibleRows)
}

// JumpTop selects the first row.
func (s *ScrollState) JumpTop(length, visibleRows int) {
	if s.clearIfEmpty(length) {
		return
	}
	s.Selected = 0
	s.EnsureVisible(length, visibleRows)
}

// JumpBottom selects the last row.
func (s *ScrollState) JumpBottom(length, visibleRows int) {
	if s.clearIfEmpty(length) {
		return
	}
	s.Selected = length - 1
	s.EnsureVisible(length, visibleRows)
}

// EnsureVisible adjusts ScrollTop so the selected row sits within the window of
// visibleRows.
func (s *ScrollState) EnsureVisible(length, visibleRows int) {
	if length == 0 || visibleRows == 0 {
		s.ScrollTop = 0
		return
	}
	if s.Selected < 0 {
		s.ScrollTop = 0
		return
	}
	if s.Selected < s.ScrollTop {
		s.ScrollTop = s.Selected
		return
	}
	bottom := s.ScrollTop + visibleRows - 1
	if s.Selected > bottom {
		s.ScrollTop = s.Selected + 1 - visibleRows
	}
}

func (s *ScrollState) clampedCurrent(length int) int {
	cur := s.Selected
	if cur < 0 {
		cur = 0
	}
	if cur > length-1 {
		cur = length - 1
	}
	return cur
}

func (s *ScrollState) clearIfEmpty(length int) bool {
	if length != 0 {
		return false
	}
	s.Selected = -1
	s.ScrollTop = 0
	return true
}
