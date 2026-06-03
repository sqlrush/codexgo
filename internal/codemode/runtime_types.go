package codemode

import (
	"github.com/sqlrush/codexgo/internal/protocol"
)

// Public tool names and runtime defaults, mirroring codex.
const (
	// PublicToolName is the name of the exec tool.
	PublicToolName = "exec"
	// WaitToolName is the name of the wait tool.
	WaitToolName = "wait"

	// DefaultExecYieldTimeMS is the default exec yield window.
	DefaultExecYieldTimeMS uint64 = 10_000
	// DefaultWaitYieldTimeMS is the default wait yield window.
	DefaultWaitYieldTimeMS uint64 = 10_000
	// DefaultMaxOutputTokensPerExecCall is the default exec result token budget.
	DefaultMaxOutputTokensPerExecCall int = 10_000
)

// exitSentinel is the sentinel exception thrown by exit() to unwind the script.
const exitSentinel = "__codex_code_mode_exit__"

// ExecuteRequest mirrors codex's `ExecuteRequest`.
type ExecuteRequest struct {
	ToolCallID      string
	EnabledTools    []ToolDefinition
	Source          string
	YieldTimeMS     *uint64
	MaxOutputTokens *int
}

// WaitRequest mirrors codex's `WaitRequest`.
type WaitRequest struct {
	CellID      CellID
	YieldTimeMS uint64
}

// WaitToPendingRequest mirrors codex's `WaitToPendingRequest`.
type WaitToPendingRequest struct {
	CellID CellID
}

// RuntimeResponseKind enumerates RuntimeResponse variants.
type RuntimeResponseKind int

// RuntimeResponseKind variants.
const (
	// RuntimeResponseYielded indicates the cell yielded control while running.
	RuntimeResponseYielded RuntimeResponseKind = iota
	// RuntimeResponseTerminated indicates the cell was terminated.
	RuntimeResponseTerminated
	// RuntimeResponseResult indicates the cell finished (with or without error).
	RuntimeResponseResult
)

// RuntimeResponse mirrors codex's externally-tagged `RuntimeResponse` enum.
// Kind selects which fields are meaningful; ErrorText only applies to Result.
type RuntimeResponse struct {
	Kind         RuntimeResponseKind
	CellID       CellID
	ContentItems []FunctionCallOutputContentItem
	// ErrorText is only populated for Result responses. A pointer mirrors
	// serde's Option<String>, preserving the present-but-empty distinction.
	ErrorText *string
}

// WaitOutcomeKind enumerates WaitOutcome variants.
type WaitOutcomeKind int

// WaitOutcomeKind variants.
const (
	// WaitOutcomeLiveCell means the cell was live when the wait was accepted.
	WaitOutcomeLiveCell WaitOutcomeKind = iota
	// WaitOutcomeMissingCell means the cell was not live.
	WaitOutcomeMissingCell
)

// WaitOutcome mirrors codex's `WaitOutcome`. It carries the model-facing
// RuntimeResponse plus the live/missing lifecycle provenance.
type WaitOutcome struct {
	Kind     WaitOutcomeKind
	Response RuntimeResponse
}

// Response returns the wrapped RuntimeResponse regardless of variant, mirroring
// codex's `From<WaitOutcome> for RuntimeResponse`.
func (o WaitOutcome) Into() RuntimeResponse {
	return o.Response
}

// ExecuteToPendingOutcomeKind enumerates ExecuteToPendingOutcome variants.
type ExecuteToPendingOutcomeKind int

// ExecuteToPendingOutcomeKind variants.
const (
	// ExecuteToPendingPending means the cell went quiescent awaiting input.
	ExecuteToPendingPending ExecuteToPendingOutcomeKind = iota
	// ExecuteToPendingCompleted means the cell reached a terminal response.
	ExecuteToPendingCompleted
)

// ExecuteToPendingOutcome mirrors codex's `ExecuteToPendingOutcome`.
type ExecuteToPendingOutcome struct {
	Kind RuntimeResponseKind // unused for Pending; kept zero
	// Variant selects Pending vs Completed.
	Variant ExecuteToPendingOutcomeKind
	// Pending fields.
	CellID             CellID
	ContentItems       []FunctionCallOutputContentItem
	PendingToolCallIDs []string
	// Completed field.
	CompletedResponse RuntimeResponse
}

// WaitToPendingOutcomeKind enumerates WaitToPendingOutcome variants.
type WaitToPendingOutcomeKind int

// WaitToPendingOutcomeKind variants.
const (
	// WaitToPendingLiveCell means the cell was live when accepted.
	WaitToPendingLiveCell WaitToPendingOutcomeKind = iota
	// WaitToPendingMissingCell means the cell was not live.
	WaitToPendingMissingCell
)

// WaitToPendingOutcome mirrors codex's `WaitToPendingOutcome`.
type WaitToPendingOutcome struct {
	Kind WaitToPendingOutcomeKind
	// LiveCell payload.
	LiveCell ExecuteToPendingOutcome
	// MissingCell payload.
	MissingCell RuntimeResponse
}

// CodeModeNestedToolCall mirrors codex's `CodeModeNestedToolCall`. It describes a
// nested tool request emitted by a running cell.
type CodeModeNestedToolCall struct {
	CellID            CellID
	RuntimeToolCallID string
	ToolName          protocol.ToolName
	ToolKind          CodeModeToolKind
	// Input is the decoded JSON argument, or nil when the JS caller passed none.
	Input any
}

// pendingRuntimeMode controls whether the runtime pauses when it goes quiescent.
type pendingRuntimeMode int

const (
	pendingModeContinue pendingRuntimeMode = iota
	pendingModePauseUntilResumed
)
