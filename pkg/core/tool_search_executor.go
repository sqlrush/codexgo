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

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// turnSearchToolEnabled mirrors spec_plan's search_tool_enabled:
// model_info.supports_search_tool.
func turnSearchToolEnabled(tc *TurnContext) bool {
	return turnModelInfo(tc).SupportsSearchTool
}

// turnNamespaceToolsEnabled mirrors spec_plan's namespace_tools_enabled
// (turn_context.provider.capabilities().namespace_tools). The capability upper
// bound is resolved from the turn's provider (all-true for OpenAI/configured
// providers, see [turnProviderCapabilities]).
func turnNamespaceToolsEnabled(tc *TurnContext) bool {
	return turnProviderCapabilities(tc).NamespaceTools
}

// toolSearchExecutor advertises and handles the tool_search tool. It carries the
// ordered deferred search sources collected at registration time (collab agent
// tools, then deferred MCP tools), mirroring the search_infos handed to the Rust
// ToolSearchHandler::new by append_tool_search_executor.
type toolSearchExecutor struct {
	// deferredSources are the deferred-exposure runtimes that contribute
	// tool_search entries, in spec_plan registration order.
	deferredSources []deferredToolSource
}

func (toolSearchExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName(tools.ToolSearchToolName)
}

// turnDeferredSearchInfos collects the tool_search entries from the carried
// deferred runtimes for the turn, in registration order. Mirrors
// append_tool_search_executor filtering the planned runtimes to
// ToolExposure::Deferred and calling search_info() on each.
func (e toolSearchExecutor) turnDeferredSearchInfos(tc *TurnContext) []tools.ToolSearchInfo {
	var infos []tools.ToolSearchInfo
	for _, source := range e.deferredSources {
		if info, ok := source.searchInfo(tc); ok {
			infos = append(infos, info)
		}
	}
	return infos
}

// Spec advertises tool_search when the append_tool_search_executor conditions
// hold for this turn (search-capable model, namespace tools, and at least one
// deferred search source).
func (e toolSearchExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if !turnSearchToolEnabled(tc) || !turnNamespaceToolsEnabled(tc) {
		return tools.ToolSpec{}, false
	}
	infos := e.turnDeferredSearchInfos(tc)
	if len(infos) == 0 {
		return tools.ToolSpec{}, false
	}
	sources := make([]tools.ToolSearchSourceInfo, 0, len(infos))
	for _, info := range infos {
		if info.SourceInfo != nil {
			sources = append(sources, *info.SourceInfo)
		}
	}
	return tools.CreateToolSearchTool(sources, tools.ToolSearchDefaultLimit), true
}

func (toolSearchExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindToolSearch
}

// Handle validates the query/limit exactly like the Rust ToolSearchHandler, then
// runs BM25 over the turn's deferred entries and returns the coalesced matches.
func (e toolSearchExecutor) Handle(_ context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
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

	engine := tools.NewToolSearchEngine(e.turnDeferredSearchInfos(h.Turn))
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
		return TelemetryPreview("[]")
	}
	return TelemetryPreview(string(raw))
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
