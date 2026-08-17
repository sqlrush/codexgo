package extensionapi

import (
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// ---------------------------------------------------------------------------
// Thread lifecycle inputs
// ---------------------------------------------------------------------------

// ThreadStartInput is supplied when the host starts a runtime for a thread.
// Mirrors Rust `ThreadStartInput<'a, C>`.
//
// Config is host-typed; the type parameter C carries the host configuration so
// thread-lifecycle contributors observe the same type the registry was built
// with.
type ThreadStartInput[C any] struct {
	// Config is the host configuration visible at thread start.
	Config C
	// SessionSource is the source that created the session for this thread.
	SessionSource rollout.SessionSource
	// PersistentThreadStateAvailable reports whether persistent thread-scoped
	// state is available for this thread.
	PersistentThreadStateAvailable bool
	// SessionStore is the store scoped to the host session runtime.
	SessionStore *ExtensionData
	// ThreadStore is the store scoped to this thread runtime.
	ThreadStore *ExtensionData
}

// ThreadResumeInput is supplied when the host resumes an existing thread.
// Mirrors Rust `ThreadResumeInput<'a>`.
type ThreadResumeInput struct {
	SessionStore *ExtensionData
	ThreadStore  *ExtensionData
}

// ThreadIdleInput is supplied when the host has no immediately pending thread
// work. Mirrors Rust `ThreadIdleInput<'a>`.
type ThreadIdleInput struct {
	SessionStore *ExtensionData
	ThreadStore  *ExtensionData
}

// ThreadStopInput is supplied when the host stops a thread runtime. Mirrors Rust
// `ThreadStopInput<'a>`.
type ThreadStopInput struct {
	SessionStore *ExtensionData
	ThreadStore  *ExtensionData
}

// ---------------------------------------------------------------------------
// Turn lifecycle inputs
// ---------------------------------------------------------------------------

// TurnStartInput is supplied when the host starts a turn. Mirrors Rust
// `TurnStartInput<'a>`.
type TurnStartInput struct {
	// TurnID is the stable host-owned turn identifier.
	TurnID string
	// CollaborationMode is the effective collaboration mode for this turn.
	CollaborationMode protocol.CollaborationMode
	// TokenUsageAtTurnStart is the total token usage snapshot captured when the
	// turn started.
	TokenUsageAtTurnStart protocol.TokenUsage
	SessionStore          *ExtensionData
	ThreadStore           *ExtensionData
	TurnStore             *ExtensionData
}

// TurnStopInput is supplied when the host completes a turn. Mirrors Rust
// `TurnStopInput<'a>`.
type TurnStopInput struct {
	SessionStore *ExtensionData
	ThreadStore  *ExtensionData
	TurnStore    *ExtensionData
}

// TurnAbortInput is supplied when the host aborts a turn. Mirrors Rust
// `TurnAbortInput<'a>`.
type TurnAbortInput struct {
	// Reason is why the host aborted the turn.
	Reason       protocol.TurnAbortReason
	SessionStore *ExtensionData
	ThreadStore  *ExtensionData
	TurnStore    *ExtensionData
}

// TurnErrorInput is supplied when the host observes an error for a turn. Mirrors
// Rust `TurnErrorInput<'a>`.
type TurnErrorInput struct {
	// TurnID is the stable host-owned turn identifier.
	TurnID string
	// Error is the error surfaced by the host for this turn.
	Error        protocol.CodexErrorInfo
	SessionStore *ExtensionData
	ThreadStore  *ExtensionData
	TurnStore    *ExtensionData
}

// ---------------------------------------------------------------------------
// Tool lifecycle inputs
// ---------------------------------------------------------------------------

// ToolCallSourceKind discriminates a [ToolCallSource]. Mirrors the Rust
// `ToolCallSource` enum.
type ToolCallSourceKind int

// ToolCallSourceKind variants.
const (
	// ToolCallSourceDirect indicates the model invoked the tool directly.
	ToolCallSourceDirect ToolCallSourceKind = iota
	// ToolCallSourceCodeMode indicates code mode invoked the tool while
	// executing a runtime cell.
	ToolCallSourceCodeMode
)

// ToolCallSource is the host-visible source for a model tool call. Mirrors the
// Rust `ToolCallSource` enum (struct + discriminator).
type ToolCallSource struct {
	Kind ToolCallSourceKind

	// CellID is the runtime cell that issued the nested tool request. Set for
	// the CodeMode variant.
	CellID string
	// RuntimeToolCallID is code-mode's per-cell tool invocation id. Set for the
	// CodeMode variant.
	RuntimeToolCallID string
}

// DirectToolCallSource constructs the Direct source.
func DirectToolCallSource() ToolCallSource {
	return ToolCallSource{Kind: ToolCallSourceDirect}
}

// CodeModeToolCallSource constructs the CodeMode source.
func CodeModeToolCallSource(cellID, runtimeToolCallID string) ToolCallSource {
	return ToolCallSource{
		Kind:              ToolCallSourceCodeMode,
		CellID:            cellID,
		RuntimeToolCallID: runtimeToolCallID,
	}
}

// ToolCallOutcomeKind discriminates a [ToolCallOutcome]. Mirrors the Rust
// `ToolCallOutcome` enum.
type ToolCallOutcomeKind int

// ToolCallOutcomeKind variants.
const (
	// ToolCallOutcomeCompleted indicates the tool returned a normal output.
	ToolCallOutcomeCompleted ToolCallOutcomeKind = iota
	// ToolCallOutcomeBlocked indicates host policy blocked the tool before the
	// handler ran.
	ToolCallOutcomeBlocked
	// ToolCallOutcomeFailed indicates the tool did not produce a normal output.
	ToolCallOutcomeFailed
	// ToolCallOutcomeAborted indicates the host cancelled the tool before
	// normal completion.
	ToolCallOutcomeAborted
)

// ToolCallOutcome is the extension-facing outcome for a finished tool call.
// Mirrors the Rust `ToolCallOutcome` enum (struct + discriminator).
type ToolCallOutcome struct {
	Kind ToolCallOutcomeKind

	// Success is the tool output's own success marker. Set for the Completed
	// variant.
	Success bool
	// HandlerExecuted reports whether the host reached the tool handler before
	// the failure. Set for the Failed variant.
	HandlerExecuted bool
}

// CompletedToolCallOutcome constructs the Completed outcome.
func CompletedToolCallOutcome(success bool) ToolCallOutcome {
	return ToolCallOutcome{Kind: ToolCallOutcomeCompleted, Success: success}
}

// BlockedToolCallOutcome constructs the Blocked outcome.
func BlockedToolCallOutcome() ToolCallOutcome {
	return ToolCallOutcome{Kind: ToolCallOutcomeBlocked}
}

// FailedToolCallOutcome constructs the Failed outcome.
func FailedToolCallOutcome(handlerExecuted bool) ToolCallOutcome {
	return ToolCallOutcome{Kind: ToolCallOutcomeFailed, HandlerExecuted: handlerExecuted}
}

// AbortedToolCallOutcome constructs the Aborted outcome.
func AbortedToolCallOutcome() ToolCallOutcome {
	return ToolCallOutcome{Kind: ToolCallOutcomeAborted}
}

// ToolStartInput is supplied when the host starts executing one tool call.
// Mirrors Rust `ToolStartInput<'a>`.
type ToolStartInput struct {
	SessionStore *ExtensionData
	ThreadStore  *ExtensionData
	TurnStore    *ExtensionData
	// TurnID is the current turn submission id.
	TurnID string
	// CallID is the model-visible tool call id.
	CallID string
	// ToolName is the tool name as routed by the host.
	ToolName protocol.ToolName
	// Source is the source that issued the tool call.
	Source ToolCallSource
}

// ToolFinishInput is supplied when the host finishes executing one tool call.
// Mirrors Rust `ToolFinishInput<'a>`.
type ToolFinishInput struct {
	SessionStore *ExtensionData
	ThreadStore  *ExtensionData
	TurnStore    *ExtensionData
	// TurnID is the current turn submission id.
	TurnID string
	// CallID is the model-visible tool call id.
	CallID string
	// ToolName is the tool name as routed by the host.
	ToolName protocol.ToolName
	// Source is the source that issued the tool call.
	Source ToolCallSource
	// Outcome is the host-observed result of the tool call.
	Outcome ToolCallOutcome
}
