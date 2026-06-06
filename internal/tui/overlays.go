// Package tui — overlay/popup system.
//
// This file ports the dismissible overlay stack from
// codex-rs/tui/src/bottom_pane/. The Rust bottom pane keeps a `Vec<Box<dyn
// BottomPaneView>>` view stack on top of the composer; here [OverlayStack] holds
// the same stack of [OverlayView] values. Each view handles key events, reports
// completion, and renders into the bottom region.
//
// Concrete views ported here:
//   - [ListSelectionOverlay]: generic list picker (model/theme/permissions/...).
//   - [CommandPopup]: slash-command popup.
//   - [FileSearchPopup]: @file search popup.
//   - [ApprovalOverlay]: exec/patch/permissions/MCP-elicitation approval modal.
//   - [RequestUserInputOverlay]: tool request_user_input prompt.
//   - [ElicitationFormOverlay]: MCP server elicitation form.
//
// Decisions are routed back to the engine through [AppEventSender] op helpers,
// matching the Rust app_event_sender approval/elicitation methods.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// CancellationEvent is the result of offering a cancellation key (Ctrl+C / Esc)
// to an overlay. A view can consume the key to dismiss itself; an unconsumed key
// lets the caller take a higher-level action.
//
// Port of bottom_pane/mod.rs CancellationEvent.
type CancellationEvent int

const (
	// CancellationNotHandled means the view did not consume the key.
	CancellationNotHandled CancellationEvent = iota
	// CancellationHandled means the view consumed the key.
	CancellationHandled
)

// ViewCompletion is the reason an overlay finished.
//
// Port of bottom_pane/bottom_pane_view.rs ViewCompletion.
type ViewCompletion int

const (
	// CompletionNone means the view has not finished.
	CompletionNone ViewCompletion = iota
	// CompletionAccepted means a selection was accepted.
	CompletionAccepted
	// CompletionCancelled means the view was dismissed without accepting.
	CompletionCancelled
)

// OverlayView is the interface every view shown in the bottom pane overlay stack
// implements.
//
// Port of bottom_pane/bottom_pane_view.rs BottomPaneView. Methods that the Rust
// trait provides as defaults are split into the small required set plus optional
// behaviors expressed by the OverlayView* marker interfaces, keeping the core
// interface small (idiomatic Go).
type OverlayView interface {
	// HandleKey processes a key event while the view is active. A redraw is
	// always scheduled by the caller after this returns. It returns an optional
	// follow-up command.
	HandleKey(msg tea.KeyMsg) tea.Cmd
	// IsComplete reports whether the view has finished and should be removed.
	IsComplete() bool
	// View renders the overlay into a region of the given size.
	View(theme Theme, area Rect) string
	// DesiredHeight reports how many rows the view wants for the given width.
	DesiredHeight(width int) int
}

// overlayCtrlC is implemented by views that handle Ctrl+C specially.
//
// Port of BottomPaneView::on_ctrl_c.
type overlayCtrlC interface {
	OnCtrlC() CancellationEvent
}

// overlayPrefersEscToKey is implemented by views that want Esc routed through
// HandleKey instead of the OnCtrlC cancellation path.
//
// Port of BottomPaneView::prefer_esc_to_handle_key_event.
type overlayPrefersEscToKey interface {
	PrefersEscToHandleKey() bool
}

// overlayTerminalTitle is implemented by views that block on user input and want
// an "Action Required" terminal title.
//
// Port of BottomPaneView::terminal_title_requires_action.
type overlayTerminalTitle interface {
	TerminalTitleRequiresAction() bool
}

// overlayCompletion is implemented by views that report a [ViewCompletion]
// reason once finished (used by parent/child dismissal flows).
type overlayCompletion interface {
	Completion() ViewCompletion
}

// OverlayStack owns the stack of active overlay views shown instead of (or above)
// the composer.
//
// Port of the `view_stack: Vec<Box<dyn BottomPaneView>>` field of
// bottom_pane/mod.rs BottomPane together with its push/pop/route logic.
// OverlayStack follows the immutability convention: mutating methods return a new
// value rather than mutating the receiver.
type OverlayStack struct {
	views []OverlayView
}

// NewOverlayStack returns an empty overlay stack.
func NewOverlayStack() OverlayStack { return OverlayStack{} }

// IsEmpty reports whether the stack has no active views.
func (s OverlayStack) IsEmpty() bool { return len(s.views) == 0 }

// Len reports the number of active views.
func (s OverlayStack) Len() int { return len(s.views) }

// Top returns the active (top-of-stack) view and whether one exists.
func (s OverlayStack) Top() (OverlayView, bool) {
	if len(s.views) == 0 {
		return nil, false
	}
	return s.views[len(s.views)-1], true
}

// Push adds a view to the top of the stack, returning the new stack.
func (s OverlayStack) Push(view OverlayView) OverlayStack {
	next := make([]OverlayView, len(s.views), len(s.views)+1)
	copy(next, s.views)
	next = append(next, view)
	return OverlayStack{views: next}
}

// pop removes the top view, returning the new stack. Internal helper.
func (s OverlayStack) pop() OverlayStack {
	if len(s.views) == 0 {
		return s
	}
	next := make([]OverlayView, len(s.views)-1)
	copy(next, s.views[:len(s.views)-1])
	return OverlayStack{views: next}
}

// HandleKey routes a key event to the active view, then prunes any completed
// views from the top. It returns the new stack plus any follow-up command.
//
// Port of BottomPane::handle_key_event's view-stack branch plus the
// remove-when-complete cleanup.
func (s OverlayStack) HandleKey(msg tea.KeyMsg) (OverlayStack, tea.Cmd) {
	top, ok := s.Top()
	if !ok {
		return s, nil
	}

	var cmd tea.Cmd
	if isCancelKey(msg) && !prefersEscRouting(top, msg) {
		if cc, okCC := top.(overlayCtrlC); okCC {
			if cc.OnCtrlC() == CancellationHandled {
				// Views queue their cancel callbacks for deferred execution
				// (running them synchronously inside Update deadlocks the
				// unbuffered Program.Send); drain them into the returned cmd.
				if p, okP := top.(interface{ PendingCmd() tea.Cmd }); okP {
					cmd = p.PendingCmd()
				}
				return s.prune(), cmd
			}
		}
	}
	cmd = top.HandleKey(msg)
	return s.prune(), cmd
}

// prune removes completed views from the top of the stack.
func (s OverlayStack) prune() OverlayStack {
	out := s
	for {
		top, ok := out.Top()
		if !ok || !top.IsComplete() {
			break
		}
		out = out.pop()
	}
	return out
}

// View renders the active view into the area, or "" when the stack is empty.
func (s OverlayStack) View(theme Theme, area Rect) string {
	top, ok := s.Top()
	if !ok {
		return ""
	}
	return top.View(theme, area)
}

// DesiredHeight reports the active view's desired height, or 0 when empty.
func (s OverlayStack) DesiredHeight(width int) int {
	top, ok := s.Top()
	if !ok {
		return 0
	}
	return top.DesiredHeight(width)
}

// TerminalTitleRequiresAction reports whether the active view blocks on user
// input and should surface an "Action Required" terminal title.
func (s OverlayStack) TerminalTitleRequiresAction() bool {
	top, ok := s.Top()
	if !ok {
		return false
	}
	if t, okT := top.(overlayTerminalTitle); okT {
		return t.TerminalTitleRequiresAction()
	}
	return false
}

// isCancelKey reports whether the key is Esc or Ctrl+C.
func isCancelKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc", "ctrl+c":
		return true
	default:
		return false
	}
}

// prefersEscRouting reports whether an Esc key should be routed through HandleKey
// rather than the cancellation path, per the view's preference.
func prefersEscRouting(view OverlayView, msg tea.KeyMsg) bool {
	if msg.String() != "esc" {
		return false
	}
	if p, ok := view.(overlayPrefersEscToKey); ok {
		return p.PrefersEscToHandleKey()
	}
	return false
}
