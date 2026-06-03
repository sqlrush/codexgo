package tui

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// AppCommandKind discriminates the [AppCommand] union.
//
// Port of codex-rs/tui/src/app_command.rs `AppCommand`. Variants tied to
// area-specific features (realtime audio, elicitations, guardian) are present so
// area agents can construct them, but the foundation event loop only needs to
// route them to the engine.
type AppCommandKind int

const (
	// AppCommandInterrupt aborts the active turn.
	AppCommandInterrupt AppCommandKind = iota
	// AppCommandUserTurn submits user input as a new turn.
	AppCommandUserTurn
	// AppCommandExecApproval answers an exec approval request.
	AppCommandExecApproval
	// AppCommandPatchApproval answers a patch approval request.
	AppCommandPatchApproval
	// AppCommandRequestPermissionsResponse answers a permissions request.
	AppCommandRequestPermissionsResponse
	// AppCommandUserInputAnswer answers a tool's request-user-input prompt.
	AppCommandUserInputAnswer
	// AppCommandResolveElicitation resolves an MCP elicitation.
	AppCommandResolveElicitation
	// AppCommandCompact compacts the conversation history.
	AppCommandCompact
	// AppCommandSetThreadName renames the active thread.
	AppCommandSetThreadName
	// AppCommandReview starts a review turn.
	AppCommandReview
	// AppCommandThreadRollback rolls back the thread by N turns.
	AppCommandThreadRollback
	// AppCommandReloadUserConfig reloads config from disk.
	AppCommandReloadUserConfig
	// AppCommandRunUserShellCommand runs a user shell command.
	AppCommandRunUserShellCommand
	// AppCommandShutdown asks the engine to shut down.
	AppCommandShutdown
)

// AppCommand is a typed outbound command forwarded to the engine. Exactly one
// payload field is populated, selected by Kind.
//
// AppCommand is constructed with the New* helpers and treated as immutable.
type AppCommand struct {
	Kind AppCommandKind

	// UserTurn payload.
	Items []appserverproto.UserInput
	// OutputSchema constrains the model's final response when non-nil (UserTurn).
	OutputSchema json.RawMessage

	// Approval payloads.
	ID       string
	TurnID   string
	Decision string

	// SetThreadName payload.
	Name string

	// RunUserShellCommand payload.
	ShellCommand string

	// ThreadRollback payload.
	NumTurns uint32

	// Review payload (target carried opaquely; resolved by the review area).
	ReviewTarget json.RawMessage

	// Generic typed response payloads for area-owned variants.
	Response json.RawMessage
}

// NewInterruptCommand builds an interrupt command.
//
// Port of AppCommand::interrupt.
func NewInterruptCommand() AppCommand { return AppCommand{Kind: AppCommandInterrupt} }

// NewCompactCommand builds a compact command.
//
// Port of AppCommand::compact.
func NewCompactCommand() AppCommand { return AppCommand{Kind: AppCommandCompact} }

// NewShutdownCommand builds a shutdown command.
//
// Port of AppCommand::shutdown.
func NewShutdownCommand() AppCommand { return AppCommand{Kind: AppCommandShutdown} }

// NewReloadUserConfigCommand builds a config-reload command.
//
// Port of AppCommand::reload_user_config.
func NewReloadUserConfigCommand() AppCommand { return AppCommand{Kind: AppCommandReloadUserConfig} }

// NewUserTurnCommand builds a user-turn command from the supplied input items
// and optional output schema.
//
// Port of AppCommand::user_turn (reduced to the fields the in-process turn/start
// path consumes; area agents may set additional turn options on the engine
// submission directly).
func NewUserTurnCommand(items []appserverproto.UserInput, outputSchema json.RawMessage) AppCommand {
	return AppCommand{Kind: AppCommandUserTurn, Items: items, OutputSchema: outputSchema}
}

// NewTextTurnCommand is a convenience for submitting a plain-text user turn.
func NewTextTurnCommand(text string) AppCommand {
	return NewUserTurnCommand([]appserverproto.UserInput{{
		Kind: appserverproto.UserInputKindText,
		Text: text,
	}}, nil)
}

// NewSetThreadNameCommand builds a rename command.
//
// Port of AppCommand::set_thread_name.
func NewSetThreadNameCommand(name string) AppCommand {
	return AppCommand{Kind: AppCommandSetThreadName, Name: name}
}

// NewThreadRollbackCommand builds a rollback command.
//
// Port of AppCommand::thread_rollback.
func NewThreadRollbackCommand(numTurns uint32) AppCommand {
	return AppCommand{Kind: AppCommandThreadRollback, NumTurns: numTurns}
}

// NewRunUserShellCommand builds a user-shell-command command.
//
// Port of AppCommand::run_user_shell_command.
func NewRunUserShellCommand(command string) AppCommand {
	return AppCommand{Kind: AppCommandRunUserShellCommand, ShellCommand: command}
}

// NewExecApprovalCommand builds an exec-approval response command.
//
// Port of AppCommand::exec_approval.
func NewExecApprovalCommand(id, turnID, decision string) AppCommand {
	return AppCommand{Kind: AppCommandExecApproval, ID: id, TurnID: turnID, Decision: decision}
}

// NewPatchApprovalCommand builds a patch-approval response command.
//
// Port of AppCommand::patch_approval.
func NewPatchApprovalCommand(id, decision string) AppCommand {
	return AppCommand{Kind: AppCommandPatchApproval, ID: id, Decision: decision}
}

// IsReview reports whether this is a review command.
//
// Port of AppCommand::is_review.
func (c AppCommand) IsReview() bool { return c.Kind == AppCommandReview }

// compile-time use of protocol to keep the import meaningful for area agents
// that decode ReviewTarget / Response payloads against protocol types.
var _ = protocol.AskForApprovalKind("")
