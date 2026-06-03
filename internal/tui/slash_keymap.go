// Package-internal integration of the slash-command set with the keymap layer
// and the bottom-pane pickers. This file ties the [SlashCommand] metadata (in
// slash_command.go) to declarative dispatch outcomes and resolves popup
// completion against the layered [RuntimeKeymap].
//
// It ports the behavioral core of codex-rs/tui/src/chatwidget/slash_dispatch.rs:
// each command maps to one [SlashOutcome] describing what the app loop should do
// (emit an event, open a picker, submit a turn, toggle state, or show output).
// The chat area consumes these outcomes; the keymap glue here is intentionally
// declarative so behavior stays testable without the full chat widget.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// SlashCommandDispatchSource distinguishes a freshly-typed command from one
// replayed out of the queued-input drain.
//
// Port of slash_dispatch.rs `SlashCommandDispatchSource`.
type SlashCommandDispatchSource int

const (
	// SlashSourceLive is a command dispatched as the user submits it.
	SlashSourceLive SlashCommandDispatchSource = iota
	// SlashSourceQueued is a command replayed from the queued-input drain.
	SlashSourceQueued
)

// SlashOutcomeKind classifies the effect of dispatching a slash command. It is
// the discriminant of [SlashOutcome].
type SlashOutcomeKind int

const (
	// OutcomeEmitEvent forwards a foundation [AppEvent] (e.g. NewSession, ClearUi).
	OutcomeEmitEvent SlashOutcomeKind = iota
	// OutcomeOpenPicker opens a bottom-pane picker/menu (model, theme, review...).
	OutcomeOpenPicker
	// OutcomeSubmitTurn submits text as a new user turn (e.g. /init prompt).
	OutcomeSubmitTurn
	// OutcomeToggleState flips a session toggle (vim, raw output, realtime).
	OutcomeToggleState
	// OutcomeShowOutput appends an informational/output cell (status, mcp, diff...).
	OutcomeShowOutput
	// OutcomeInsertText inserts literal text into the composer (e.g. /mention -> "@").
	OutcomeInsertText
	// OutcomeRunCommand runs an async side effect (e.g. /diff git diff).
	OutcomeRunCommand
	// OutcomeNoop does nothing (gated-off feature or unrecognized in context).
	OutcomeNoop
)

// PickerID names the bottom-pane picker an [OutcomeOpenPicker] opens. Port of the
// distinct open_*_popup / show_selection_view sites in slash_dispatch.rs.
type PickerID string

const (
	PickerModel        PickerID = "model"
	PickerTheme        PickerID = "theme"
	PickerReview       PickerID = "review"
	PickerPermissions  PickerID = "permissions"
	PickerKeymap       PickerID = "keymap"
	PickerPersonality  PickerID = "personality"
	PickerAgent        PickerID = "agent"
	PickerResume       PickerID = "resume"
	PickerArchive      PickerID = "archive"
	PickerSkills       PickerID = "skills"
	PickerMemories     PickerID = "memories"
	PickerAutoReview   PickerID = "auto-review"
	PickerExperimental PickerID = "experimental"
	PickerPets         PickerID = "pets"
	PickerFeedback     PickerID = "feedback"
	PickerTitle        PickerID = "terminal-title"
	PickerStatusline   PickerID = "status-line"
	PickerRealtime     PickerID = "realtime-audio"
)

// ToggleID names a session toggle an [OutcomeToggleState] flips.
type ToggleID string

const (
	ToggleVim       ToggleID = "vim"
	ToggleRawOutput ToggleID = "raw-output"
	ToggleRealtime  ToggleID = "realtime"
)

// OutputID names the kind of informational cell an [OutcomeShowOutput] appends.
type OutputID string

const (
	OutputStatus      OutputID = "status"
	OutputDebugConfig OutputID = "debug-config"
	OutputMcp         OutputID = "mcp"
	OutputApps        OutputID = "apps"
	OutputPlugins     OutputID = "plugins"
	OutputHooks       OutputID = "hooks"
	OutputPs          OutputID = "ps"
	OutputRollout     OutputID = "rollout"
	OutputCopy        OutputID = "copy"
	OutputStop        OutputID = "stop"
	OutputIde         OutputID = "ide"
	OutputGoal        OutputID = "goal"
	OutputMemoryStub  OutputID = "memory-stub"
	OutputSide        OutputID = "side"
	OutputRename      OutputID = "rename"
)

// SlashOutcome is the declarative result of dispatching a slash command. Exactly
// one payload field is meaningful per Kind. SlashOutcome is immutable: build a
// new value rather than mutating one.
//
// Port of the per-arm effects in slash_dispatch.rs::dispatch_command, lifted into
// data so the keymap layer can describe behavior without the chat widget.
type SlashOutcome struct {
	Kind    SlashOutcomeKind
	Command SlashCommand

	// Event is set for [OutcomeEmitEvent].
	Event AppEvent
	// Picker is set for [OutcomeOpenPicker].
	Picker PickerID
	// Toggle is set for [OutcomeToggleState].
	Toggle ToggleID
	// Output is set for [OutcomeShowOutput].
	Output OutputID
	// Text is set for [OutcomeSubmitTurn] and [OutcomeInsertText].
	Text string
	// RunID is set for [OutcomeRunCommand] (e.g. "diff").
	RunID string
}

// ResolveSlashOutcome maps a parsed command (and its trailing inline args) to the
// declarative [SlashOutcome] the chat area should apply.
//
// Port of slash_dispatch.rs::dispatch_command's effect selection. Feature-gating
// (whether a command is visible/usable at all) is the caller's responsibility via
// [BuiltinsForInput]/[FindBuiltinCommand]; this function assumes the command is
// permitted and selects its effect. args is the text after the command word,
// already trimmed; it matters only for commands that [SlashCommand.SupportsInlineArgs].
func ResolveSlashOutcome(cmd SlashCommand, args string, source SlashCommandDispatchSource) SlashOutcome {
	out := SlashOutcome{Command: cmd}
	switch cmd {
	case SlashNew:
		out.Kind, out.Event = OutcomeEmitEvent, NewSessionEvent{}
	case SlashClear:
		out.Kind, out.Event = OutcomeEmitEvent, ClearUIEvent{}
	case SlashQuit, SlashExit:
		out.Kind, out.Event = OutcomeEmitEvent, ExitEvent{Mode: ExitShutdownFirst}
	case SlashCompact:
		out.Kind, out.Event = OutcomeEmitEvent, CodexOpEvent{Command: NewCompactCommand()}
	case SlashInit:
		out.Kind, out.Text = OutcomeSubmitTurn, InitPrompt

	// Pickers / menus.
	case SlashModel:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerModel
	case SlashTheme:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerTheme
	case SlashReview:
		out.Kind, out.Picker, out.Text = OutcomeOpenPicker, PickerReview, args
	case SlashPermissions:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerPermissions
	case SlashKeymap:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerKeymap
	case SlashPersonality:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerPersonality
	case SlashAgent, SlashMultiAgents:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerAgent
	case SlashResume:
		out.Kind, out.Picker, out.Text = OutcomeOpenPicker, PickerResume, args
	case SlashArchive:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerArchive
	case SlashSkills:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerSkills
	case SlashMemories:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerMemories
	case SlashAutoReview:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerAutoReview
	case SlashExperimental:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerExperimental
	case SlashPets:
		out.Kind, out.Picker, out.Text = OutcomeOpenPicker, PickerPets, args
	case SlashFeedback:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerFeedback
	case SlashTitle:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerTitle
	case SlashStatusline:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerStatusline
	case SlashSettings:
		out.Kind, out.Picker = OutcomeOpenPicker, PickerRealtime

	// Toggles.
	case SlashVim:
		out.Kind, out.Toggle = OutcomeToggleState, ToggleVim
	case SlashRaw:
		out.Kind, out.Toggle = OutcomeToggleState, ToggleRawOutput
	case SlashRealtime:
		out.Kind, out.Toggle = OutcomeToggleState, ToggleRealtime

	// Composer insertion.
	case SlashMention:
		out.Kind, out.Text = OutcomeInsertText, "@"

	// Async side effects.
	case SlashDiff:
		out.Kind, out.RunID = OutcomeRunCommand, "diff"

	// Informational output cells.
	case SlashStatus:
		out.Kind, out.Output = OutcomeShowOutput, OutputStatus
	case SlashDebugConfig:
		out.Kind, out.Output = OutcomeShowOutput, OutputDebugConfig
	case SlashMcp:
		out.Kind, out.Output, out.Text = OutcomeShowOutput, OutputMcp, args
	case SlashApps:
		out.Kind, out.Output = OutcomeShowOutput, OutputApps
	case SlashPlugins:
		out.Kind, out.Output = OutcomeShowOutput, OutputPlugins
	case SlashHooks:
		out.Kind, out.Output = OutcomeShowOutput, OutputHooks
	case SlashPs:
		out.Kind, out.Output = OutcomeShowOutput, OutputPs
	case SlashStop:
		out.Kind, out.Output = OutcomeShowOutput, OutputStop
	case SlashRollout:
		out.Kind, out.Output = OutcomeShowOutput, OutputRollout
	case SlashCopy:
		out.Kind, out.Output = OutcomeShowOutput, OutputCopy
	case SlashIde:
		out.Kind, out.Output, out.Text = OutcomeShowOutput, OutputIde, args
	case SlashGoal:
		out.Kind, out.Output, out.Text = OutcomeShowOutput, OutputGoal, args
	case SlashRename:
		out.Kind, out.Output, out.Text = OutcomeShowOutput, OutputRename, args
	case SlashSide, SlashBtw:
		out.Kind, out.Output, out.Text = OutcomeShowOutput, OutputSide, args
	case SlashMemoryDrop, SlashMemoryUpdate:
		out.Kind, out.Output = OutcomeShowOutput, OutputMemoryStub

	default:
		out.Kind = OutcomeNoop
	}
	return out
}

// InitPrompt is the prompt submitted by /init. Port of the include_str! of
// prompt_for_init_command.md; the chat area may substitute the canonical prompt.
const InitPrompt = "Please explore this codebase and create an AGENTS.md file with " +
	"instructions for Codex, including build/lint/test commands and code style guidelines."

// EmitOutcomeCmd returns a [tea.Cmd] for the subset of outcomes the foundation
// can fulfill directly: those that emit a foundation [AppEvent]. Other outcome
// kinds (pickers, toggles, output) are area-specific and return nil so the chat
// area handles them.
//
// This is the keymap/slash glue point: the chat widget calls
// [ResolveSlashOutcome] then [EmitOutcomeCmd], handling area-specific kinds
// itself when this returns (nil, false).
func EmitOutcomeCmd(out SlashOutcome) (tea.Cmd, bool) {
	switch out.Kind {
	case OutcomeEmitEvent:
		return EventCmd(out.Event), true
	case OutcomeSubmitTurn:
		return SubmitUserMessageCmd(out.Text), true
	default:
		return nil, false
	}
}
