package tui

import (
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// clearTerminalSequence is the exact ANSI sequence codex emits to clear the
// terminal on /clear (and Ctrl-L), captured byte-for-byte from codex 0.136.0 and
// matching custom_terminal::clear_scrollback_and_visible_screen_ansi:
//
//	\x1b[r  — reset scroll region (DECSTBM)
//	\x1b[0m — reset SGR / style state
//	\x1b[H  — cursor home
//	\x1b[2J — erase the entire visible screen (ED2)
//	\x1b[3J — erase the scrollback buffer (ED3)
//	\x1b[H  — cursor home again
//
// The order matches the common shell `clear && printf '\e[3J'` behavior so
// terminals (Terminal.app, Warp) that do not reliably drop scrollback when purge
// and clear are emitted separately still clear in one shot.
const clearTerminalSequence = "\x1b[r\x1b[0m\x1b[H\x1b[2J\x1b[3J\x1b[H"

// handleClearUI implements /clear (AppEvent::ClearUi): clear the terminal UI
// (scrollback + visible screen via the exact codex ANSI sequence), reset the
// transcript and scrollback-drain state, and re-render the fresh session so the
// session-header welcome card drains into scrollback again.
//
// Port of the AppEvent::ClearUi arm in app/event_dispatch.rs, which runs
// clear_terminal_ui (the ANSI clear) + reset_app_ui_state_after_clear +
// start_fresh_session_with_summary_hint (the fresh header). The engine thread is
// not torn down here — the post-clear UI is identical either way — keeping the
// behavior UI-side and side-effect free for the running conversation.
func (m Model) handleClearUI() (tea.Model, tea.Cmd) {
	// Reset the transcript to a fresh, header-seeded one (discarding history +
	// drain bookkeeping). When the transcript does not support the seam this is a
	// no-op and only the terminal clear runs.
	if resettable, ok := m.transcript.(ClearResettable); ok {
		m.transcript = resettable.ResetForClear()
	}

	// Build the commands in strict order so the fresh header lands AFTER the
	// clear: (1) raw codex clear bytes (incl. the \x1b[3J scrollback purge the
	// renderer's own clear omits), (2) tea.ClearScreen to reset bubbletea's
	// renderer for a full repaint, (3) the drained header re-printed into native
	// scrollback, (4) a redraw of the fresh live region.
	clear := m.rawWriteCmd(clearTerminalSequence)

	// Drain the fresh header NOW (marking it flushed) and capture it as an
	// explicit print command sequenced after the clear, instead of letting the
	// Update wrapper drain it concurrently with the clear bytes.
	var headerPrint tea.Cmd
	if m.inline && m.width > 0 {
		if drainer, ok := m.transcript.(ScrollbackDrainer); ok {
			lines, view := drainer.DrainScrollback(m.width)
			m.transcript = view
			if len(lines) > 0 {
				headerPrint = tea.Println(strings.Join(lines, "\n"))
			}
		}
	}

	cmds := []tea.Cmd{clear, tea.ClearScreen}
	if headerPrint != nil {
		cmds = append(cmds, headerPrint)
	}
	cmds = append(cmds, EventCmd(RedrawEvent{}))
	return m, tea.Sequence(cmds...)
}

// rawWriteCmd returns a command that writes the raw byte sequence to the model's
// output writer (the terminal). It is used to emit control sequences bubbletea's
// managed renderer does not expose (e.g. the scrollback-purge on /clear).
//
// When no output writer is configured (view-only/test models) it is a no-op.
func (m Model) rawWriteCmd(seq string) tea.Cmd {
	out := m.output
	return func() tea.Msg {
		if out != nil {
			_, _ = io.WriteString(out, seq)
		}
		return nil
	}
}
