package core

// Deferred MCP tool runtimes, porting the deferred branch of the Rust
// spec_plan::add_mcp_runtime_tools (deferred_mcp_tools ->
// add_with_exposure(McpHandler, ToolExposure::Deferred)) plus the
// McpHandler::search_info collected by append_tool_search_executor.
//
// A `defer_loading` MCP tool is registered dispatch-only: its Spec is hidden
// (ToolExposure::Deferred), it is reachable directly only after the model loads
// it via tool_search, and it contributes a tool_search entry + per-server source
// when namespace tools are enabled this turn. This mirrors the collab deferred
// runtimes (see collab_executors.go); the spec/search text construction lives in
// internal/tools (mcp_search.go).

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// deferredMcpExecutor is a single deferred (defer_loading) MCP tool runtime. It
// is registered dispatch-only with a hidden spec and routes direct invocations
// through the same MCP caller as the eagerly-advertised [mcpExecutor].
type deferredMcpExecutor struct {
	info  tools.McpToolInfo
	name  protocol.ToolName
	inner mcpExecutor
}

// newDeferredMcpExecutor builds a deferred MCP runtime for the given tool info,
// wired to the supplied caller. The dispatch path reuses [mcpExecutor] (built
// against the tool's eager namespace spec) so a loaded deferred tool executes
// identically to an eager one.
func newDeferredMcpExecutor(caller McpToolCaller, info tools.McpToolInfo, spec tools.ToolSpec) deferredMcpExecutor {
	name := info.CanonicalToolName()
	return deferredMcpExecutor{
		info: info,
		name: name,
		inner: mcpExecutor{
			caller: caller,
			spec:   spec,
			name:   name,
		},
	}
}

func (e deferredMcpExecutor) Name() protocol.ToolName { return e.name }

// Spec reports a hidden spec: a deferred MCP tool is never directly
// model-visible; it is advertised solely through tool_search. Mirrors
// ToolExposure::Deferred.
func (deferredMcpExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	return tools.ToolSpec{}, false
}

// MatchesPayload accepts function payloads (the model calls a deferred MCP tool
// by name after loading it via tool_search).
func (deferredMcpExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

// Handle routes a direct invocation through the underlying MCP caller, the same
// path the eager [mcpExecutor] uses.
func (e deferredMcpExecutor) Handle(ctx context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	return e.inner.Handle(ctx, h)
}

// searchInfo returns the deferred tool_search entry for this turn when namespace
// tools are enabled (the same gate the collab runtimes use). Mirrors the Rust
// McpHandler::search_info collected by append_tool_search_executor for Deferred
// runtimes.
func (e deferredMcpExecutor) searchInfo(tc *TurnContext) (tools.ToolSearchInfo, bool) {
	if !turnNamespaceToolsEnabled(tc) {
		return tools.ToolSearchInfo{}, false
	}
	return tools.McpToolSearchInfo(e.info)
}

// deferredMcpExecutors builds the deferred MCP runtimes for the configured
// DeferredMcpTools, in dependency order (which fixes the BM25 corpus document
// ids relative to the other deferred sources). A nil caller or empty slice
// yields no runtimes. Mirrors the deferred branch of add_mcp_runtime_tools:
// tools whose spec fails to build are skipped (the Rust warn! path).
func deferredMcpExecutors(deps BuiltinToolDeps) []deferredMcpExecutor {
	if deps.Mcp == nil || len(deps.DeferredMcpTools) == 0 {
		return nil
	}
	execs := make([]deferredMcpExecutor, 0, len(deps.DeferredMcpTools))
	for _, info := range deps.DeferredMcpTools {
		spec, err := tools.McpToolSpec(info)
		if err != nil {
			// Mirror the Rust warn!+skip on a tool whose spec fails to build.
			continue
		}
		execs = append(execs, newDeferredMcpExecutor(deps.Mcp, info, spec))
	}
	return execs
}
