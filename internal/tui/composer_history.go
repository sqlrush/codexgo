package tui

import "strings"

// ComposerHistory is the shell-style recall state for the composer: Up/Down
// navigation through previously submitted entries and Ctrl+R reverse incremental
// search.
//
// It is a focused port of codex-rs/tui/src/bottom_pane/chat_composer_history.rs
// `ChatComposerHistory`, reduced to in-session local history. The async
// persistent cross-session history fetch (LookupMessageHistoryEntry) is omitted
// as a deliberate simplification; recall over the current session matches the
// Rust local-history semantics exactly (newest at end, adjacent duplicates
// collapsed, cursor reset on new submissions, boundary-gated navigation).
//
// ComposerHistory is a value type; every mutating method returns a new copy.
type ComposerHistory struct {
	// entries are submissions in chronological order (newest at end).
	entries []string
	// cursor is the current navigation index; -1 means "not browsing".
	cursor int
	// lastRecalled is the text of the last entry inserted by navigation, used to
	// gate Up/Down vs. ordinary cursor movement.
	lastRecalled string
	// hasLast reports whether lastRecalled is set.
	hasLast bool
	// searchSeen tracks unique texts already returned by the active search so
	// repeated Ctrl+R skips duplicates.
	searchOffset int
}

// NewComposerHistory builds an empty history with no active navigation.
func NewComposerHistory() ComposerHistory {
	return ComposerHistory{cursor: -1, searchOffset: -1}
}

// Record appends a submission, collapsing adjacent duplicates and ignoring empty
// entries, and resets navigation.
//
// Port of ChatComposerHistory::record_local_submission.
func (h ComposerHistory) Record(text string) ComposerHistory {
	text = strings.TrimSpace(text)
	if text == "" {
		return h.ResetNavigation()
	}
	h = h.ResetNavigation()
	if n := len(h.entries); n > 0 && h.entries[n-1] == text {
		return h
	}
	h.entries = append(append([]string(nil), h.entries...), text)
	return h
}

// ResetNavigation returns a copy with navigation/search cursors cleared so the
// next Up resumes from the newest entry.
//
// Port of ChatComposerHistory::reset_navigation.
func (h ComposerHistory) ResetNavigation() ComposerHistory {
	h.cursor = -1
	h.lastRecalled = ""
	h.hasLast = false
	h.searchOffset = -1
	return h
}

// Browsing reports whether the user is currently navigating history.
func (h ComposerHistory) Browsing() bool { return h.cursor >= 0 }

// ShouldNavigate reports whether an Up/Down press should recall history for the
// given buffer text and cursor offset.
//
// Port of ChatComposerHistory::should_handle_navigation: empty text always
// navigates; non-empty text navigates only when it matches the last recalled
// entry and the cursor is at a line boundary (start or end).
func (h ComposerHistory) ShouldNavigate(text string, cursor int) bool {
	if len(h.entries) == 0 {
		return false
	}
	if text == "" {
		return true
	}
	if cursor != 0 && cursor != len(text) {
		return false
	}
	return h.hasLast && h.lastRecalled == text
}

// Up moves toward older entries, returning the recalled text. The bool is false
// when already at the oldest entry.
//
// Port of ChatComposerHistory::navigate_up.
func (h *ComposerHistory) Up() (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	switch {
	case h.cursor < 0:
		h.cursor = len(h.entries) - 1
	case h.cursor == 0:
		return "", false
	default:
		h.cursor--
	}
	entry := h.entries[h.cursor]
	h.lastRecalled = entry
	h.hasLast = true
	return entry, true
}

// Down moves toward newer entries, returning the recalled text. Past the newest
// entry it returns ("", true) so the caller clears the composer; not browsing
// returns ("", false).
//
// Port of ChatComposerHistory::navigate_down.
func (h *ComposerHistory) Down() (string, bool) {
	if h.cursor < 0 {
		return "", false
	}
	if h.cursor+1 >= len(h.entries) {
		h.cursor = -1
		h.lastRecalled = ""
		h.hasLast = false
		return "", true
	}
	h.cursor++
	entry := h.entries[h.cursor]
	h.lastRecalled = entry
	h.hasLast = true
	return entry, true
}

// Search performs a case-insensitive reverse-incremental search for query.
// advance moves to the next older match (subsequent Ctrl+R); otherwise the
// search restarts from the newest entry. It returns the matched text and whether
// a match was found.
//
// Port of the older-direction path of ChatComposerHistory::search, scoped to
// local history.
func (h *ComposerHistory) Search(query string, advance bool) (string, bool) {
	q := strings.ToLower(query)
	start := len(h.entries) - 1
	if advance && h.searchOffset >= 0 {
		start = h.searchOffset - 1
	}
	for i := start; i >= 0; i-- {
		if q == "" || strings.Contains(strings.ToLower(h.entries[i]), q) {
			h.searchOffset = i
			return h.entries[i], true
		}
	}
	return "", false
}

// Entries returns a copy of the recorded entries (oldest first). Test helper.
func (h ComposerHistory) Entries() []string {
	return append([]string(nil), h.entries...)
}
