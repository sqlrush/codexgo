package engine

import (
	"github.com/sqlrush/codexgo/internal/codemode"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// runtimeCommandKind enumerates the commands a host delivers to a running cell
// runtime, mirroring codex's `RuntimeCommand`.
type runtimeCommandKind int

const (
	// runtimeCmdToolResponse resolves a pending nested tool call promise.
	runtimeCmdToolResponse runtimeCommandKind = iota
	// runtimeCmdToolError rejects a pending nested tool call promise.
	runtimeCmdToolError
	// runtimeCmdTimeoutFired invokes a scheduled timeout callback.
	runtimeCmdTimeoutFired
	// runtimeCmdTerminate unwinds the runtime.
	runtimeCmdTerminate
)

// runtimeCommand mirrors codex's `RuntimeCommand` enum. Fields are populated
// based on Kind.
type runtimeCommand struct {
	Kind runtimeCommandKind
	// ID identifies the tool call (ToolResponse/ToolError).
	ID string
	// Result is the decoded JSON tool response (ToolResponse).
	Result any
	// ErrorText is the tool error message (ToolError).
	ErrorText string
	// TimeoutID identifies the fired timeout (TimeoutFired).
	TimeoutID uint64
}

// runtimeControlKind enumerates pause/resume controls, mirroring codex's
// `RuntimeControlCommand`.
type runtimeControlKind int

const (
	runtimeControlResume runtimeControlKind = iota
	runtimeControlTerminate
)

// runtimeControlCommand mirrors codex's `RuntimeControlCommand`.
type runtimeControlCommand struct {
	Kind runtimeControlKind
}

// runtimeEventKind enumerates events the runtime emits to the host, mirroring
// codex's `RuntimeEvent`.
type runtimeEventKind int

const (
	// runtimeEventStarted signals the module began evaluating.
	runtimeEventStarted runtimeEventKind = iota
	// runtimeEventPending signals the runtime went quiescent awaiting input.
	runtimeEventPending
	// runtimeEventContentItem carries a text/image output.
	runtimeEventContentItem
	// runtimeEventYieldRequested signals an explicit yield_control() call.
	runtimeEventYieldRequested
	// runtimeEventToolCall requests a nested tool invocation.
	runtimeEventToolCall
	// runtimeEventNotify carries a notify() message.
	runtimeEventNotify
	// runtimeEventResult signals the module reached a terminal state.
	runtimeEventResult
)

// runtimeEvent mirrors codex's `RuntimeEvent` enum. Fields are populated based
// on Kind.
type runtimeEvent struct {
	Kind runtimeEventKind
	// ContentItem payload (ContentItem).
	ContentItem FunctionCallOutputContentItem
	// ToolCall payload (ToolCall).
	ID       string
	ToolName protocol.ToolName
	ToolKind codemode.CodeModeToolKind
	Input    any
	// Notify payload (Notify).
	CallID string
	Text   string
	// Result payload (Result).
	StoredValueWrites map[string]any
	ErrorText         *string
}

// completionStateKind enumerates the terminal-vs-pending states of the main
// module promise, mirroring codex's `CompletionState`.
type completionStateKind int

const (
	completionPending completionStateKind = iota
	completionCompleted
)

// completionState mirrors codex's `CompletionState`.
type completionState struct {
	Kind              completionStateKind
	StoredValueWrites map[string]any
	ErrorText         *string
}
