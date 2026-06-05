package core

// tool_search executor, porting spec_plan's append_tool_search_executor and
// the ToolSearchHandler advertisement: the tool advertises when the model
// supports the search tool, the provider exposes namespace tools, and at least
// one deferred-exposure tool source exists. In a default run the deferred
// sources are the collab agent tools (spawn_agent et al.), which register
// deferred exactly when Feature::Collab is enabled without MultiAgentV2 (under
// V2 they are direct, and with collab off they do not register at all).
//
// STUB: the deferred tool registry itself (the collab agent tool specs, MCP
// defer_loading tools, and the BM25 search engine over their metadata) is
// owned by the multi-agent / MCP area agents. Until those runtimes land,
// dispatch validates arguments exactly like codex and returns an empty result
// set (codex's empty-entries fast path).

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// multiAgentToolSearchSource is the search-source descriptor the collab agent
// tools contribute. Mirrors the Rust MULTI_AGENT_TOOL_SEARCH_SOURCE_NAME /
// _DESCRIPTION constants in handlers/multi_agents.rs.
func multiAgentToolSearchSource() tools.ToolSearchSourceInfo {
	description := "Spawn and manage sub-agents."
	return tools.ToolSearchSourceInfo{Name: "Multi-agent tools", Description: &description}
}

// turnSearchToolEnabled mirrors spec_plan's search_tool_enabled:
// model_info.supports_search_tool.
func turnSearchToolEnabled(tc *TurnContext) bool {
	return turnModelInfo(tc).SupportsSearchTool
}

// turnNamespaceToolsEnabled mirrors spec_plan's namespace_tools_enabled
// (provider.capabilities().namespace_tools).
//
// STUB: provider capabilities are not yet modeled; every built-in provider in
// codex defaults namespace_tools to true, so the bridge does too. The
// provider-capabilities surface is owned by the model-provider area agent.
func turnNamespaceToolsEnabled(_ *TurnContext) bool {
	return true
}

// turnDeferredCollabSources returns the deferred-exposure search sources for
// the turn. In spec_plan the collab agent tools take ToolExposure::Deferred
// when search + namespace tools are available and collab tools are enabled
// (Feature::Collab) outside multi-agent v2; those deferred runtimes are what
// feed append_tool_search_executor's search_infos in a bare run.
func turnDeferredCollabSources(tc *TurnContext) []tools.ToolSearchSourceInfo {
	feats := turnFeatures(tc)
	if !feats.Enabled(features.FeatureCollab) || feats.Enabled(features.FeatureMultiAgentV2) {
		return nil
	}
	// The five collab agent tools all share the multi-agent source; the spec
	// builder deduplicates by name, so one descriptor represents them.
	return []tools.ToolSearchSourceInfo{multiAgentToolSearchSource()}
}

// toolSearchExecutor advertises and handles the tool_search tool.
type toolSearchExecutor struct{}

func (toolSearchExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName(tools.ToolSearchToolName)
}

// Spec advertises tool_search when the append_tool_search_executor conditions
// hold for this turn.
func (toolSearchExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if !turnSearchToolEnabled(tc) || !turnNamespaceToolsEnabled(tc) {
		return tools.ToolSpec{}, false
	}
	sources := turnDeferredCollabSources(tc)
	if len(sources) == 0 {
		return tools.ToolSpec{}, false
	}
	return tools.CreateToolSearchTool(sources, tools.ToolSearchDefaultLimit), true
}

func (toolSearchExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindToolSearch
}

// Handle validates the query/limit exactly like the Rust ToolSearchHandler and
// returns the empty-entries result (no deferred tool registry yet — STUB).
func (toolSearchExecutor) Handle(_ context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
	if h.Payload.Kind != tools.ToolPayloadKindToolSearch {
		return nil, tools.FatalError(tools.ToolSearchToolName + " handler received unsupported payload")
	}
	args := h.Payload.SearchArguments
	if strings.TrimSpace(args.Query) == "" {
		return nil, tools.RespondToModelError("query must not be empty")
	}
	if args.Limit != nil && *args.Limit == 0 {
		return nil, tools.RespondToModelError("limit must be greater than zero")
	}
	return toolSearchOutput{}, nil
}

// toolSearchOutput is the tool_search result body. Mirrors the Rust
// `ToolSearchOutput` (empty-entries form).
type toolSearchOutput struct {
	tools.DefaultToolOutput
}

func (toolSearchOutput) LogPreview() string { return telemetryPreview("[]") }

func (toolSearchOutput) SuccessForLogging() bool { return true }

func (toolSearchOutput) ToResponseItem(callID string, _ tools.ToolPayload) tools.ResponseInputItem {
	return tools.ToolSearchOutputInput(callID, "completed", "client", nil)
}

// CodeModeResult surfaces the (empty) matched-tools array to a code-mode
// runtime, the projection of the trait-default
// response_input_to_code_mode_result for a tool_search output.
func (toolSearchOutput) CodeModeResult(tools.ToolPayload) json.RawMessage {
	return json.RawMessage(`[]`)
}
