package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Rect is a view region in terminal cells, the analogue of ratatui's `Rect`.
// Coordinates are zero-based; (X, Y) is the top-left corner.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Bottom returns the row just past the rect (Y + Height).
func (r Rect) Bottom() int { return r.Y + r.Height }

// Right returns the column just past the rect (X + Width).
func (r Rect) Right() int { return r.X + r.Width }

// IsEmpty reports whether the rect has no area.
func (r Rect) IsEmpty() bool { return r.Width <= 0 || r.Height <= 0 }

// Layout describes the two stacked view regions of the chat screen: the
// transcript area (scrollback of history cells) on top and the bottom pane
// (composer, popups, approval modals, status line) beneath it.
//
// This mirrors the Rust ChatWidget split where the bottom pane requests a
// desired height and the remainder is the transcript area. Layout is pure
// geometry; the rendering of each region is owned by the [TranscriptView] and
// [BottomPane] seams.
type Layout struct {
	// Transcript is the region above the bottom pane.
	Transcript Rect
	// Bottom is the region the bottom pane renders into.
	Bottom Rect
}

// ComputeLayout splits a terminal of the given size into transcript and bottom
// regions. bottomHeight is the bottom pane's desired height; it is clamped so at
// least one transcript row remains when possible.
//
// Cosmetic deviation: the Rust TUI renders history into the terminal's native
// scrollback above an inline viewport. Bubbletea owns the full screen, so the
// transcript is rendered as a scrollable region inside the program instead. The
// region math below is the bubbletea equivalent of that inline-viewport split.
func ComputeLayout(width, height, bottomHeight int) Layout {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	if bottomHeight < 0 {
		bottomHeight = 0
	}
	// Reserve at least one transcript row when the screen is tall enough.
	maxBottom := height
	if height > 1 {
		maxBottom = height - 1
	}
	if bottomHeight > maxBottom {
		bottomHeight = maxBottom
	}
	transcriptHeight := height - bottomHeight
	if transcriptHeight < 0 {
		transcriptHeight = 0
	}
	return Layout{
		Transcript: Rect{X: 0, Y: 0, Width: width, Height: transcriptHeight},
		Bottom:     Rect{X: 0, Y: transcriptHeight, Width: width, Height: bottomHeight},
	}
}

// TranscriptView is the seam the transcript/history area agent implements. It
// owns the scrollback of rendered history cells (agent/user messages, exec
// cells, diffs, reasoning) and the streaming animation state.
//
// The foundation only depends on this small interface; the concrete
// implementation is supplied by the transcript area agent in a sibling file.
type TranscriptView interface {
	// Update handles a message routed to the transcript area and returns the
	// possibly-updated view plus any follow-up command.
	Update(msg tea.Msg) (TranscriptView, tea.Cmd)
	// View renders the transcript into a region of the given size, returning a
	// string of at most Height rows.
	View(area Rect) string
	// AppendCoreEvent feeds a decoded engine event into the transcript so it can
	// build/stream history cells. It returns the updated view.
	AppendCoreEvent(ev CoreEventMsg) TranscriptView
}

// ScrollbackDrainer is the optional capability a [TranscriptView] implements to
// support inline rendering: completed history cells are written into the
// terminal's native scrollback (above the live viewport) rather than re-rendered
// inside the program's viewport each frame. The root [Model] detects this seam
// when running inline and prints the drained lines via tea.Println.
//
// It mirrors codex's inline architecture, where finalized history cells are
// inserted into scrollback with insert_history_lines (codex-rs/tui/src/tui.rs)
// while the live viewport renders only the composer/status region.
type ScrollbackDrainer interface {
	// DrainScrollback returns the lines for cells committed since the last drain
	// (ready for tea.Println) and the updated view with those cells marked
	// flushed. It returns (nil, self) when nothing is pending.
	DrainScrollback(width int) ([]string, TranscriptView)
}

// BottomPane is the seam the bottom-pane area agent implements. It owns the
// composer, slash-command/file-search popups, approval modals, and the status
// line, and reports the height it currently needs.
type BottomPane interface {
	// Update handles a message routed to the bottom pane and returns the
	// possibly-updated pane plus any follow-up command.
	Update(msg tea.Msg) (BottomPane, tea.Cmd)
	// View renders the bottom pane into a region of the given size.
	View(area Rect) string
	// DesiredHeight reports how many rows the pane wants for the given width.
	DesiredHeight(width int) int
}

// noopTranscript is the placeholder transcript used until the area agent's
// implementation is wired in. It renders nothing and ignores all input.
type noopTranscript struct{}

func (n noopTranscript) Update(tea.Msg) (TranscriptView, tea.Cmd)    { return n, nil }
func (noopTranscript) View(Rect) string                              { return "" }
func (n noopTranscript) AppendCoreEvent(CoreEventMsg) TranscriptView { return n }

// noopBottomPane is the placeholder bottom pane used until the area agent's
// implementation is wired in. It reserves a single row and renders nothing.
type noopBottomPane struct{}

func (n noopBottomPane) Update(tea.Msg) (BottomPane, tea.Cmd) { return n, nil }
func (noopBottomPane) View(Rect) string                       { return "" }
func (noopBottomPane) DesiredHeight(int) int                  { return 1 }

// compile-time assertions for the placeholders.
var (
	_ TranscriptView = noopTranscript{}
	_ BottomPane     = noopBottomPane{}
)
