package core

// tool_search executor, porting spec_plan's append_tool_search_executor and the
// ToolSearchHandler: the tool advertises when the model supports the search
// tool, the provider exposes namespace tools, and at least one deferred-exposure
// tool source exists. In a default run the deferred sources are the collab agent
// tools (spawn_agent et al.), which register deferred exactly when
// Feature::Collab is enabled without MultiAgentV2 (under V2 they are direct, and
// with collab off they do not register at all).
//
// Handle runs the real BM25 search over the deferred entries (see
// internal/tools/bm25.go and tool_search_engine.go) and feeds back the coalesced
// LoadableToolSpecs in hit order.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

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

// turnDeferredCollabSources returns the deferred-exposure search sources for the
// turn. In spec_plan the collab agent tools take ToolExposure::Deferred when
// search + namespace tools are available and collab tools are enabled
// (Feature::Collab) outside multi-agent v2; those deferred runtimes feed
// append_tool_search_executor's search_infos in a bare run.
func turnDeferredCollabSources(tc *TurnContext) []tools.ToolSearchSourceInfo {
	feats := turnFeatures(tc)
	if !feats.Enabled(features.FeatureCollab) || feats.Enabled(features.FeatureMultiAgentV2) {
		return nil
	}
	infos := turnDeferredSearchInfos(tc)
	sources := make([]tools.ToolSearchSourceInfo, 0, len(infos))
	for _, info := range infos {
		if info.SourceInfo != nil {
			sources = append(sources, *info.SourceInfo)
		}
	}
	return sources
}

// turnDeferredSearchInfos collects the tool_search entries from the turn's
// deferred runtimes, in registration order. Mirrors append_tool_search_executor
// filtering the planned runtimes to ToolExposure::Deferred and calling
// search_info() on each.
func turnDeferredSearchInfos(tc *TurnContext) []tools.ToolSearchInfo {
	var infos []tools.ToolSearchInfo
	for _, ex := range collabExecutors() {
		if info, ok := ex.searchInfo(tc); ok {
			infos = append(infos, info)
		}
	}
	return infos
}

// toolSearchExecutor advertises and handles the tool_search tool.
type toolSearchExecutor struct{}

func (toolSearchExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName(tools.ToolSearchToolName)
}

// Spec advertises tool_search when the append_tool_search_executor conditions
// hold for this turn (search-capable model, namespace tools, and at least one
// deferred search source).
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

// Handle validates the query/limit exactly like the Rust ToolSearchHandler, then
// runs BM25 over the turn's deferred entries and returns the coalesced matches.
func (toolSearchExecutor) Handle(_ context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
	if h.Payload.Kind != tools.ToolPayloadKindToolSearch {
		return nil, tools.FatalError(tools.ToolSearchToolName + " handler received unsupported payload")
	}
	args := h.Payload.SearchArguments
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return nil, tools.RespondToModelError("query must not be empty")
	}
	limit := tools.ToolSearchDefaultLimit
	if args.Limit != nil {
		limit = *args.Limit
	}
	// Rust's limit is a usize, so non-positive values are impossible there;
	// reject them here with the same model-facing zero message.
	if limit <= 0 {
		return nil, tools.RespondToModelError("limit must be greater than zero")
	}

	engine := tools.NewToolSearchEngine(turnDeferredSearchInfos(h.Turn))
	if engine.Empty() {
		return toolSearchOutput{}, nil
	}

	specs := engine.Search(query, limit)
	raws := make([]json.RawMessage, 0, len(specs))
	for _, spec := range specs {
		raw, err := json.Marshal(spec)
		if err != nil {
			return nil, tools.FatalError("failed to serialize tool_search result: " + err.Error())
		}
		raws = append(raws, raw)
	}
	return toolSearchOutput{tools: raws}, nil
}

// toolSearchOutput is the tool_search result body. Mirrors the Rust
// `ToolSearchOutput` carrying the matched LoadableToolSpecs.
type toolSearchOutput struct {
	tools.DefaultToolOutput
	tools []json.RawMessage
}

func (o toolSearchOutput) LogPreview() string {
	raw, err := json.Marshal(o.tools)
	if err != nil {
		return telemetryPreview("[]")
	}
	return telemetryPreview(string(raw))
}

func (toolSearchOutput) SuccessForLogging() bool { return true }

func (o toolSearchOutput) ToResponseItem(callID string, _ tools.ToolPayload) tools.ResponseInputItem {
	return tools.ToolSearchOutputInput(callID, "completed", "client", o.tools)
}

// CodeModeResult surfaces the matched-tools array to a code-mode runtime, the
// projection of the trait-default response_input_to_code_mode_result for a
// tool_search output.
func (o toolSearchOutput) CodeModeResult(tools.ToolPayload) json.RawMessage {
	if len(o.tools) == 0 {
		return json.RawMessage(`[]`)
	}
	raw, err := json.Marshal(o.tools)
	if err != nil {
		return json.RawMessage(`[]`)
	}
	return raw
}
