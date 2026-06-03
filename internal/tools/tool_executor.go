package tools

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ToolExposure controls where a tool is exposed to the model. Mirrors Rust
// `ToolExposure`.
type ToolExposure int

// ToolExposure variants.
const (
	// ToolExposureDirect includes the tool in the initial model-visible list and,
	// under code mode, as a nested code-mode tool.
	ToolExposureDirect ToolExposure = iota

	// ToolExposureDeferred registers the tool for later discovery but omits it
	// from the initial model-visible list.
	ToolExposureDeferred

	// ToolExposureDirectModelOnly includes the tool in the initial model-visible
	// list only, excluding it from the nested code-mode surface.
	ToolExposureDirectModelOnly

	// ToolExposureHidden keeps the tool registered for dispatch without exposing
	// it to the model.
	ToolExposureHidden
)

// IsDirect reports whether the exposure surfaces the tool in the initial
// model-visible tool list. Mirrors Rust `ToolExposure::is_direct`.
func (e ToolExposure) IsDirect() bool {
	return e == ToolExposureDirect || e == ToolExposureDirectModelOnly
}

// ToolExecutor is the shared runtime contract for model-visible tools. It ties a
// model-visible spec to an executable runtime. Mirrors the Rust
// `ToolExecutor<Invocation>` trait.
//
// The type parameter Invocation is the host-specific invocation context passed
// to Handle. SupportsParallelToolCalls drives the host's parallel-vs-sequential
// dispatch decision; the default in Rust is false, so sequential.
type ToolExecutor[Invocation any] interface {
	// ToolName returns the concrete tool name handled by this runtime instance.
	ToolName() protocol.ToolName

	// Spec returns the model-visible tool spec.
	Spec() ToolSpec

	// Exposure reports how the tool is surfaced to the model.
	Exposure() ToolExposure

	// SupportsParallelToolCalls reports whether the host may run this tool
	// concurrently with other parallel-capable tool calls.
	SupportsParallelToolCalls() bool

	// Handle executes the invocation and returns the tool output.
	Handle(ctx context.Context, invocation Invocation) (ToolOutput, error)
}
