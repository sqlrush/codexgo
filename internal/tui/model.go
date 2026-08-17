package tui

import (
	"context"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/pkg/config"
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

	// output is the terminal writer used for raw control sequences bubbletea's
	// managed renderer does not expose (e.g. the /clear scrollback purge). It
	// defaults to os.Stdout, matching tea.NewProgram's default output. Tests may
	// override it to capture the emitted bytes.
	output io.Writer

	width  int
	height int

	// inline runs the TUI in inline-scrollback mode (no alternate screen):
	// completed history cells are printed into the terminal's native scrollback
	// via tea.Println and the live viewport renders only the composer/status
	// region (plus a one-row top inset). This mirrors codex's inline
	// architecture. When false the legacy alt-screen path renders the whole
	// transcript inside the viewport.
	inline bool

	// quitting is set once an exit has been requested so the View can render a
	// final frame before the program tears down.
	quitting bool
	// exitMode records the requested exit strategy.
	exitMode ExitMode

	// persistModel persists a /model picker selection (host callback writing
	// `model = "<slug>"` into config.toml). Nil disables persistence.
	persistModel func(slug string) error

	// mcpTools maps a lowercased MCP tool name to its descriptor, fetched at
	// startup. It powers the deterministic slash→tool-call entry: typing
	// /<tool-name> invokes the tool directly. Empty when no MCP servers connect.
	mcpTools map[string]appserverproto.McpToolDescriptor
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
	// Inline enables inline-scrollback rendering (no alternate screen). The
	// interactive launcher (Run) sets it; test/view-only models leave it false
	// to keep the legacy stacked-region View behavior.
	Inline bool
	// Output is the terminal writer for raw control sequences (the /clear
	// scrollback purge). When nil it defaults to os.Stdout, matching
	// tea.NewProgram's default output. Tests override it to capture the bytes.
	Output io.Writer
	// PersistModelSelection persists a /model picker selection to the host's
	// configuration. May be nil to disable persistence.
	PersistModelSelection func(slug string) error
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
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}
	return Model{
		caps:         cfg.Caps,
		theme:        LoadTheme(cfg.Tui, cfg.Caps),
		sender:       sender,
		engine:       cfg.Engine,
		transcript:   transcript,
		bottom:       bottom,
		output:       output,
		inline:       cfg.Inline,
		persistModel: cfg.PersistModelSelection,
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
	// Fetch the connected MCP tools so /<tool-name> deterministic commands work.
	// engine.Start (initialize + thread/start) has already run, so the request is
	// safe here; a failure degrades to no dynamic commands.
	return tea.Batch(EventCmd(RedrawEvent{}), m.loadMcpToolsCmd())
}

// Update implements tea.Model. It routes spine messages (resize, key, app
// events, engine events) and delegates everything else to the area seams. In
// inline mode any newly committed history cells are drained into scrollback
// after routing (see drainScrollback).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		model, cmd := m.delegate(msg)
		return model.(Model).withScrollback(cmd)

	case tea.KeyMsg:
		if cmd, handled := m.handleGlobalKey(msg); handled {
			return m, cmd
		}
		model, cmd := m.delegate(msg)
		return model.(Model).withScrollback(cmd)

	case CoreEventMsg:
		m.transcript = m.transcript.AppendCoreEvent(msg)
		model, cmd := m.delegate(msg)
		return model.(Model).withScrollback(cmd)

	case EngineClosedMsg:
		m.quitting = true
		return m, tea.Quit

	case mcpToolsLoadedMsg:
		// Index for the deterministic dispatch intercept, and forward to the bottom
		// pane so the composer's slash popup lists the MCP commands.
		m.mcpTools = indexMcpTools(msg.tools)
		var cmd tea.Cmd
		m.bottom, cmd = m.bottom.Update(msg)
		return m, cmd

	case AppEvent:
		model, cmd := m.handleAppEvent(msg)
		return model.(Model).withScrollback(cmd)

	default:
		model, cmd := m.delegate(msg)
		return model.(Model).withScrollback(cmd)
	}
}

// withScrollback drains any newly committed history cells into native terminal
// scrollback (inline mode only) and batches the resulting tea.Println commands
// ahead of the supplied follow-up command. It is a no-op in alt-screen mode or
// when the transcript does not support draining.
func (m Model) withScrollback(next tea.Cmd) (tea.Model, tea.Cmd) {
	if !m.inline || m.width <= 0 {
		return m, next
	}
	drainer, ok := m.transcript.(ScrollbackDrainer)
	if !ok {
		return m, next
	}
	lines, view := drainer.DrainScrollback(m.width)
	m.transcript = view
	if len(lines) == 0 {
		return m, next
	}
	// tea.Println splits its argument on newlines into the renderer's queued
	// scrollback lines (printed above the live viewport in one batch), so a
	// single call with the joined block preserves insertion order.
	print := tea.Println(strings.Join(lines, "\n"))
	if next == nil {
		return m, print
	}
	return m, tea.Batch(print, next)
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
		// Deterministic dual-entry: a "/<tool-name>" that matches a connected MCP
		// tool runs that tool directly (no LLM turn) and renders its result. A
		// built-in slash never reaches here (it is dispatched earlier), and an
		// unknown slash falls through to a normal user turn as before.
		if desc, args, ok := m.matchMcpSlash(ev.Text); ok {
			return m.runMcpTool(desc, args, ev.Text)
		}
		// Native-psql dual-entry: bare SQL (SELECT …) or a backslash meta-command
		// (\d, \dt, …) routes deterministically to the connected `sql` tool
		// (read-only), bypassing the LLM. CJK natural language never matches.
		if desc, stmt, ok := m.matchDbRaw(ev.Text); ok {
			return m.runDbRaw(desc, stmt, ev.Text)
		}
		// Echo the submitted message into history TUI-side (codex's
		// on_user_message_display), then forward the turn to the engine. The
		// withScrollback wrapper applied to the returned cmd drains the new user
		// cell into native scrollback.
		if strings.TrimSpace(ev.Text) != "" {
			m.transcript = m.transcript.AppendUserMessage(ev.Text)
		}
		return m, m.submitUserMessage(ev.Text)

	case ClearUIEvent:
		return m.handleClearUI()

	case NewSessionEvent:
		return m.handleNewSession()

	case ModelSelectedEvent:
		return m.handleModelSelected(ev)

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

// View implements tea.Model. In inline mode it renders only the live region
// (the still-active transcript tail, a one-row top inset, then the
// composer/status bottom pane); finalized history lives in native scrollback. In
// alt-screen mode it computes the transcript/bottom split and stacks both
// regions to fill the screen.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.inline {
		return m.inlineView()
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

// inlineView renders the inline live viewport: the not-yet-flushed transcript
// tail (e.g. an in-flight streaming cell) on top, a single blank top-inset row
// (codex insets the bottom pane by top=1, chatwidget/rendering.rs), then the
// bottom pane. Its height is the natural content height; bubbletea sizes the
// inline viewport to the number of lines returned.
func (m Model) inlineView() string {
	bottomHeight := m.bottom.DesiredHeight(m.width)
	bottom := m.bottom.View(Rect{X: 0, Y: 0, Width: m.width, Height: bottomHeight})

	// The live transcript tail (active stream) renders above the composer; on a
	// fresh first frame it is empty because the header card is already in
	// scrollback.
	tail := m.transcript.View(Rect{X: 0, Y: 0, Width: m.width, Height: m.height})
	tail = strings.TrimRight(tail, "\n")

	var b strings.Builder
	if tail != "" {
		b.WriteString(tail)
		b.WriteByte('\n')
	}
	b.WriteByte('\n') // top inset (codex bottom-pane inset: top=1)
	b.WriteString(bottom)
	return b.String()
}

// compile-time assertion that Model satisfies tea.Model.
var _ tea.Model = Model{}
