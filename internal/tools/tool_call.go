package tools

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

// ConversationHistory is a raw response-history snapshot available when an
// extension tool is invoked. Mirrors Rust `ConversationHistory`. The stored
// items are copied defensively so the snapshot is immutable.
type ConversationHistory struct {
	items []protocol.ResponseItem
}

// NewConversationHistory builds a history snapshot from response items, copying
// the slice so later mutation of the caller's slice does not affect the snapshot.
func NewConversationHistory(items []protocol.ResponseItem) ConversationHistory {
	copied := make([]protocol.ResponseItem, len(items))
	copy(copied, items)
	return ConversationHistory{items: copied}
}

// Items returns the snapshot's response items. The returned slice must not be
// mutated by callers.
func (h ConversationHistory) Items() []protocol.ResponseItem {
	return h.items
}

// ExtensionTurnItemKind discriminates an ExtensionTurnItem. Mirrors the Rust
// enum, which currently has a single variant.
type ExtensionTurnItemKind int

// ExtensionTurnItemKind variants.
const (
	// ExtensionTurnItemKindWebSearch wraps a completed web-search turn item.
	ExtensionTurnItemKindWebSearch ExtensionTurnItemKind = iota
)

// ExtensionTurnItem is a visible turn item that an extension fully owns and may
// emit as-is. Mirrors Rust `ExtensionTurnItem`.
type ExtensionTurnItem struct {
	Kind ExtensionTurnItemKind
	// WebSearch is set for the WebSearch variant.
	WebSearch protocol.WebSearchItem
}

// WebSearchTurnItem constructs a WebSearch ExtensionTurnItem.
func WebSearchTurnItem(item protocol.WebSearchItem) ExtensionTurnItem {
	return ExtensionTurnItem{Kind: ExtensionTurnItemKindWebSearch, WebSearch: item}
}

// TurnItemEmitter is the host-provided capability for extension tools to emit
// finalized visible turn items. Mirrors the Rust `TurnItemEmitter` trait;
// lifecycle events route through the host's normal item-event pipeline.
type TurnItemEmitter interface {
	// EmitStarted emits the beginning of one visible turn item.
	EmitStarted(ctx context.Context, item ExtensionTurnItem)

	// EmitCompleted emits the completion of one visible turn item.
	EmitCompleted(ctx context.Context, item ExtensionTurnItem)

	// ImageGenerationCompleted publishes image bytes for host persistence and
	// visible completion, returning persisted-artifact context for the
	// extension's model-facing output, or false when no artifact was saved.
	ImageGenerationCompleted(ctx context.Context, callID, prompt, result string) (string, bool)
}

// NoopTurnItemEmitter is a TurnItemEmitter that performs no visible emission.
// Mirrors Rust `NoopTurnItemEmitter`.
type NoopTurnItemEmitter struct{}

// EmitStarted does nothing.
func (NoopTurnItemEmitter) EmitStarted(context.Context, ExtensionTurnItem) {}

// EmitCompleted does nothing.
func (NoopTurnItemEmitter) EmitCompleted(context.Context, ExtensionTurnItem) {}

// ImageGenerationCompleted returns no artifact.
func (NoopTurnItemEmitter) ImageGenerationCompleted(context.Context, string, string, string) (string, bool) {
	return "", false
}

// ToolCall carries everything a tool runtime needs to handle one invocation.
// Mirrors Rust `ToolCall`.
type ToolCall struct {
	TurnID              string
	CallID              string
	ToolName            protocol.ToolName
	Model               string
	TruncationPolicy    truncation.TruncationPolicy
	ConversationHistory ConversationHistory
	TurnItemEmitter     TurnItemEmitter
	Payload             ToolPayload
}

// FunctionArguments returns the raw function-call arguments, or a fatal error if
// the payload is not a Function payload. Mirrors Rust
// `ToolCall::function_arguments`.
func (c ToolCall) FunctionArguments() (string, error) {
	if c.Payload.Kind == ToolPayloadKindFunction {
		return c.Payload.Arguments, nil
	}
	return "", FatalError(
		"tool " + c.ToolName.String() + " invoked with incompatible payload",
	)
}
