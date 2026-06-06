package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// keyDown advances the popup selection by one.
func keyDown(c Composer) Composer {
	return c.HandleKey(tea.KeyMsg{Type: tea.KeyDown}).Composer
}

// TestSlashPopupScrollFollowsSelection verifies the slash popup scrolls its
// visible window with the selection (Rust ScrollState::ensure_visible) instead
// of truncating at the top: moving past the 8th row must shift the window so
// the selected row stays visible rather than vanishing below it.
func TestSlashPopupScrollFollowsSelection(t *testing.T) {
	c := typeString(NewComposer(testTheme(), nil), "/")
	all := len(c.popup.items)
	if all <= maxPopupVisibleRows {
		t.Fatalf("slash popup has %d items; need more than %d to exercise scrolling", all, maxPopupVisibleRows)
	}

	// Walk the selection through every row; at each step the selected row must
	// be inside the visible window.
	for step := 0; step < all; step++ {
		rows, selected, ok := c.PopupRows()
		if !ok {
			t.Fatalf("step %d: popup unexpectedly closed", step)
		}
		if len(rows) != maxPopupVisibleRows {
			t.Fatalf("step %d: visible rows = %d, want %d", step, len(rows), maxPopupVisibleRows)
		}
		if selected < 0 || selected >= len(rows) {
			t.Fatalf("step %d: selected index %d outside visible window [0,%d) — selection vanished",
				step, selected, len(rows))
		}
		// The highlighted label must be the absolute selected item.
		if want := c.popup.items[c.popup.selected].label; rows[selected].Label != want {
			t.Fatalf("step %d: highlighted label = %q, want %q", step, rows[selected].Label, want)
		}
		c = keyDown(c)
	}

	// After the 9th row the window must have scrolled off the first item.
	c2 := typeString(NewComposer(testTheme(), nil), "/")
	firstLabel := c2.popup.items[0].label
	for i := 0; i < maxPopupVisibleRows; i++ { // selection -> index 8 (9th row)
		c2 = keyDown(c2)
	}
	rows, selected, _ := c2.PopupRows()
	if rows[0].Label == firstLabel {
		t.Errorf("window did not scroll: first visible row still %q", firstLabel)
	}
	if selected != maxPopupVisibleRows-1 {
		t.Errorf("selected relative index = %d, want %d (bottom of window)", selected, maxPopupVisibleRows-1)
	}

	// Wrap-around from the last item back to the top resets the window.
	c3 := typeString(NewComposer(testTheme(), nil), "/")
	for i := 0; i < len(c3.popup.items); i++ {
		c3 = keyDown(c3)
	}
	rows3, selected3, _ := c3.PopupRows()
	if rows3[0].Label != firstLabel || selected3 != 0 {
		t.Errorf("wrap-around: first visible = %q selected = %d, want %q / 0",
			rows3[0].Label, selected3, firstLabel)
	}
}
