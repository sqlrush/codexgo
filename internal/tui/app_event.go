package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// AppEvent is the internal message bus value type between UI components and the
// top-level app loop. Widgets emit AppEvents to request actions handled at the
// app layer (forwarding commands to the engine, opening pickers, persisting
// config, shutting down) without reaching into [Model] internals.
//
// Port of codex-rs/tui/src/app_event.rs `AppEvent`. Rust models this as one
// large enum; in Go it is an interface implemented by small event structs. This
// is deliberately extensible: area agents add their own AppEvent structs in
// their own files (same package) without editing this file, and the model's
// Update routes on a type switch. Each AppEvent is also a [tea.Msg] so it flows
// through the bubbletea runtime unchanged.
//
// The marker method keeps the set closed to this package while remaining open to
// new variants from sibling files.
type AppEvent interface {
	tea.Msg
	isAppEvent()
}

// ExitMode is the requested shutdown strategy.
//
// Port of app_event.rs `ExitMode`.
type ExitMode int

const (
	// ExitShutdownFirst shuts down core and exits after completion. Preferred for
	// user-initiated quits so cleanup runs.
	ExitShutdownFirst ExitMode = iota
	// ExitImmediate exits the UI loop immediately, skipping shutdown. In-flight
	// work may be dropped.
	ExitImmediate
)

// --- Spine event variants ---------------------------------------------------
//
// The foundation defines the variants the root loop must handle. Area-specific
// variants (pickers, plugins, keymap editing, realtime audio, etc.) live in the
// area agents' files and embed [BaseAppEvent] to satisfy the interface.

// BaseAppEvent can be embedded by sibling-file event structs to satisfy the
// [AppEvent] marker without re-declaring it.
type BaseAppEvent struct{}

func (BaseAppEvent) isAppEvent() {}

// CodexOpEvent forwards a command to the engine for the active thread.
//
// Port of AppEvent::CodexOp.
type CodexOpEvent struct {
	BaseAppEvent
	Command AppCommand
}

// SubmitThreadOpEvent forwards a command to a specific thread regardless of
// focus.
//
// Port of AppEvent::SubmitThreadOp.
type SubmitThreadOpEvent struct {
	BaseAppEvent
	ThreadID string
	Command  AppCommand
}

// SubmitUserMessageEvent submits a plain user message as a new turn. It is a
// convenience the composer emits; the app loop turns it into a UserTurn op.
type SubmitUserMessageEvent struct {
	BaseAppEvent
	Text string
}

// ExitEvent requests application exit with the given strategy.
//
// Port of AppEvent::Exit.
type ExitEvent struct {
	BaseAppEvent
	Mode ExitMode
}

// FatalExitEvent requests exit due to a fatal error.
//
// Port of AppEvent::FatalExitRequest.
type FatalExitEvent struct {
	BaseAppEvent
	Message string
}

// NewSessionEvent starts a new session.
//
// Port of AppEvent::NewSession.
type NewSessionEvent struct {
	BaseAppEvent
}

// ClearUIEvent clears the screen and scrollback, starting a fresh session while
// keeping the previous chat resumable.
//
// Port of AppEvent::ClearUi.
type ClearUIEvent struct {
	BaseAppEvent
}

// RedrawEvent requests a repaint without any state change. Mirrors the scheduled
// Draw TuiEvent in the Rust loop.
type RedrawEvent struct {
	BaseAppEvent
}

// StartCommitAnimationEvent / StopCommitAnimationEvent / CommitTickEvent drive
// the streaming "commit" animation.
//
// Port of AppEvent::StartCommitAnimation / StopCommitAnimation / CommitTick.
type StartCommitAnimationEvent struct{ BaseAppEvent }

// StopCommitAnimationEvent stops the streaming commit animation.
type StopCommitAnimationEvent struct{ BaseAppEvent }

// CommitTickEvent advances the streaming commit animation by one frame.
type CommitTickEvent struct{ BaseAppEvent }

// DiffResultEvent carries the rendered result of a `/diff` command.
//
// Port of AppEvent::DiffResult.
type DiffResultEvent struct {
	BaseAppEvent
	Diff string
}

// OpenURLInBrowserEvent opens the provided URL in the user's browser.
//
// Port of AppEvent::OpenUrlInBrowser.
type OpenURLInBrowserEvent struct {
	BaseAppEvent
	URL string
}

// --- Engine-stream messages -------------------------------------------------
//
// These are not AppEvents (they originate from the engine, not widgets) but they
// are tea.Msg values delivered into the model the same way. They live here so
// the message taxonomy is discoverable in one place.

// CoreEventMsg carries one decoded engine event (`codex/event`) into the model.
// It is produced by the engine pump in engine.go.
type CoreEventMsg struct {
	// Event is the decoded core engine event.
	Event protocol.Event
}

// EngineErrorMsg reports a transport/decode error from the engine pump.
type EngineErrorMsg struct {
	// Err is the underlying error.
	Err error
}

// EngineClosedMsg signals the engine event stream has closed.
type EngineClosedMsg struct{}

// compile-time assertions that the spine variants satisfy AppEvent.
var (
	_ AppEvent = CodexOpEvent{}
	_ AppEvent = SubmitThreadOpEvent{}
	_ AppEvent = SubmitUserMessageEvent{}
	_ AppEvent = ExitEvent{}
	_ AppEvent = FatalExitEvent{}
	_ AppEvent = NewSessionEvent{}
	_ AppEvent = ClearUIEvent{}
	_ AppEvent = RedrawEvent{}
	_ AppEvent = StartCommitAnimationEvent{}
	_ AppEvent = StopCommitAnimationEvent{}
	_ AppEvent = CommitTickEvent{}
	_ AppEvent = DiffResultEvent{}
	_ AppEvent = OpenURLInBrowserEvent{}
)
