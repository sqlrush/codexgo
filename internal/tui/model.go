package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/internal/config"
)

// Model is the root bubbletea model: the TUI spine that owns the view-region
// layout, the transcript and bottom-pane seams, the theme, terminal
// capabilities, and the wiring to the engine.
//
// It is the Go/Elm analogue of the Rust App + ChatWidget pair: App owns the
// event loop and engine, ChatWidget owns the transcript/composer split. Here a
// single Model holds both via the [TranscriptView] and [BottomPane] seams that
// area agents implement.
//
// Model follows the immutability convention: Update returns a new Model value
// rather than mutating the receiver.
type Model struct {
	caps   Capabilities
	theme  Theme
	sender *AppEventSender
	engine *Engine

	transcript TranscriptView
	bottom     BottomPane

	width  int
	height int

	// quitting is set once an exit has been requested so the View can render a
	// final frame before the program tears down.
	quitting bool
	// exitMode records the requested exit strategy.
	exitMode ExitMode
}

// ModelConfig parameterizes a root [Model].
type ModelConfig struct {
	// Caps are the detected terminal capabilities.
	Caps Capabilities
	// Tui is the resolved [tui] config block; the theme is loaded from it. May be
	// nil for defaults.
	Tui *config.Tui
	// Sender delivers async app events into the program. May be nil (a detached
	// sender is created).
	Sender *AppEventSender
	// Engine drives the conversation. May be nil for view-only/test models.
	Engine *Engine
	// Transcript / Bottom are the area seams. When nil, no-op placeholders are
	// used so the spine builds and runs before area agents land.
	Transcript TranscriptView
	Bottom     BottomPane
}

// NewModel constructs the root model from configuration, applying defaults for
// any omitted seam.
func NewModel(cfg ModelConfig) Model {
	sender := cfg.Sender
	if sender == nil {
		sender = NewAppEventSender()
	}
	var transcript TranscriptView = noopTranscript{}
	if cfg.Transcript != nil {
		transcript = cfg.Transcript
	}
	var bottom BottomPane = noopBottomPane{}
	if cfg.Bottom != nil {
		bottom = cfg.Bottom
	}
	return Model{
		caps:       cfg.Caps,
		theme:      LoadTheme(cfg.Tui, cfg.Caps),
		sender:     sender,
		engine:     cfg.Engine,
		transcript: transcript,
		bottom:     bottom,
	}
}

// Theme returns the model's resolved theme.
func (m Model) Theme() Theme { return m.theme }

// Capabilities returns the model's terminal capabilities.
func (m Model) Capabilities() Capabilities { return m.caps }

// Init implements tea.Model. It schedules an initial repaint; the engine pump
// and startup work are launched by the caller (cmd/codex-tui) after the program
// is attached so async sends are delivered.
func (m Model) Init() tea.Cmd {
	return EventCmd(RedrawEvent{})
}

// Update implements tea.Model. It routes spine messages (resize, key, app
// events, engine events) and delegates everything else to the area seams.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m.delegate(msg)

	case tea.KeyMsg:
		if cmd, handled := m.handleGlobalKey(msg); handled {
			return m, cmd
		}
		return m.delegate(msg)

	case CoreEventMsg:
		m.transcript = m.transcript.AppendCoreEvent(msg)
		return m.delegate(msg)

	case EngineClosedMsg:
		m.quitting = true
		return m, tea.Quit

	case AppEvent:
		return m.handleAppEvent(msg)

	default:
		return m.delegate(msg)
	}
}

// handleGlobalKey handles the few keybindings the app loop owns globally. It
// returns (cmd, true) when the key was consumed.
//
// Port of the global key handling in app.rs (Ctrl+C interrupt/quit). Area-owned
// keybindings (composer editing, popup navigation) are handled by the bottom
// pane and are not intercepted here.
func (m Model) handleGlobalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		// First Ctrl+C interrupts the active turn; the bottom pane decides when a
		// second press should quit. The foundation forwards an interrupt op and
		// lets the engine/turn state settle, matching the Rust default.
		if m.engine != nil {
			m.sender.Interrupt()
		}
		return nil, true
	default:
		return nil, false
	}
}

// handleAppEvent routes an [AppEvent] emitted by a widget or a Cmd.
func (m Model) handleAppEvent(ev AppEvent) (tea.Model, tea.Cmd) {
	switch ev := ev.(type) {
	case ExitEvent:
		m.quitting = true
		m.exitMode = ev.Mode
		return m, tea.Quit

	case FatalExitEvent:
		m.quitting = true
		m.exitMode = ExitImmediate
		return m, tea.Quit

	case SubmitUserMessageEvent:
		return m, m.submitUserMessage(ev.Text)

	case CodexOpEvent:
		return m, m.submitCommand(ev.Command, "")

	case SubmitThreadOpEvent:
		return m, m.submitCommand(ev.Command, ev.ThreadID)

	case RedrawEvent:
		return m, nil

	default:
		// Unhandled spine/area event: delegate so area seams can react.
		return m.delegate(ev)
	}
}

// submitUserMessage forwards a plain-text turn to the engine off the UI loop and
// returns nil (the result arrives as streamed CoreEventMsgs).
func (m Model) submitUserMessage(text string) tea.Cmd {
	if m.engine == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	cmd := NewTextTurnCommand(text)
	return m.submitCommand(cmd, "")
}

// submitCommand forwards a command to the engine on a background goroutine so the
// UI loop never blocks on a turn-driving RPC. Errors surface as EngineErrorMsg.
func (m Model) submitCommand(cmd AppCommand, threadID string) tea.Cmd {
	engine := m.engine
	sender := m.sender
	if engine == nil {
		return nil
	}
	return func() tea.Msg {
		go func() {
			if err := engine.SubmitCommand(context.Background(), cmd, threadID); err != nil {
				sender.SendMsg(EngineErrorMsg{Err: err})
			}
		}()
		return nil
	}
}

// delegate forwards a message to both area seams and batches their commands.
func (m Model) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.transcript, cmd = m.transcript.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	m.bottom, cmd = m.bottom.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// View implements tea.Model. It computes the transcript/bottom split and renders
// each region, stacking them vertically.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	bottomHeight := m.bottom.DesiredHeight(m.width)
	layout := ComputeLayout(m.width, m.height, bottomHeight)

	transcript := m.transcript.View(layout.Transcript)
	bottom := m.bottom.View(layout.Bottom)

	if bottom == "" {
		return transcript
	}
	if transcript == "" {
		return bottom
	}
	return transcript + "\n" + bottom
}

// compile-time assertion that Model satisfies tea.Model.
var _ tea.Model = Model{}
