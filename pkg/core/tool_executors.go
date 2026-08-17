package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/pkg/modelsmanager"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// This file ports the core tool-dispatch handlers from codex-core's
// tools/handlers/*. Each built-in tool is a small [ToolExecutor] that parses its
// arguments, performs its effect through an injected dependency, and returns a
// model-facing [tools.ToolOutput]. Approval/sandbox escalation, streaming output
// deltas, and parallel execution are deferred to area agents (noted as STUB).

// ----------------------------------------------------------------------------
// Injected dependencies + small interfaces (DI)
// ----------------------------------------------------------------------------

// McpToolCaller invokes a fully-qualified MCP tool. It is the small interface
// the MCP executor depends on, satisfied by internal/mcp's manager. Keeping it
// here (where it is consumed) avoids an import cycle with internal/mcp.
//
// STUB: server-required gating, OAuth status, and elicitation routing are owned
// by the MCP area; this surface only needs to call a tool and return its result.
type McpToolCaller interface {
	// CallQualifiedTool invokes "server__tool" with the given JSON arguments and
	// optional _meta, returning the raw MCP result.
	CallQualifiedTool(ctx context.Context, qualifiedName string, arguments, meta json.RawMessage) (protocol.CallToolResult, error)
}

// WebSearchRunner performs a web search on behalf of the web_search tool. It is
// the small interface the web-search executor depends on.
//
// STUB: the real flow streams begin/end events and folds a WebSearchItem into
// visible turn items; this surface returns the structured result that the
// executor serializes back to the model.
type WebSearchRunner interface {
	// Search runs a query and returns a JSON-serializable result value.
	Search(ctx context.Context, query string) (json.RawMessage, error)
}

// UserInputRequester routes a request_user_input call to the client and waits
// for the answer. It is satisfied by the session's interaction machinery (owned
// by the interaction area agent).
type UserInputRequester interface {
	// RequestUserInput poses the questions and blocks until the user responds or
	// the call is cancelled (returns ok=false on cancellation).
	RequestUserInput(ctx context.Context, callID string, args protocol.RequestUserInputArgs) (protocol.RequestUserInputResponse, bool)
}

// PermissionsRequester routes a request_permissions call to the client and waits
// for the decision.
type PermissionsRequester interface {
	// RequestPermissions requests the additional permissions and blocks until the
	// user responds or the call is cancelled (returns ok=false on cancellation).
	RequestPermissions(ctx context.Context, callID string, args protocol.RequestPermissionsArgs) (protocol.RequestPermissionsResponse, bool)
}

// BuiltinToolDeps bundles the injected dependencies the built-in executors need.
// Nil fields disable the corresponding tool: e.g. no ShellTools omits the shell
// family.
type BuiltinToolDeps struct {
	// ShellTools are the shell-family executors (exec_command / write_stdin /
	// shell_command), registered first so they lead the model-visible spec order
	// exactly like spec_plan::add_shell_tools. They are built by core/localexec
	// (localexec.ShellExecutors); an empty slice runs the session without any
	// local command execution, which is the shape a hosted, non-shell deployment
	// (airush) uses.
	ShellTools []ToolExecutor
	// ApplyPatch is the standalone apply_patch executor (localexec.NewApplyPatchExecutor);
	// nil omits the tool.
	ApplyPatch ToolExecutor
	// Mcp invokes MCP tools.
	Mcp McpToolCaller
	// McpTools are the eager (directly-advertised) MCP tools for this turn. Each
	// carries its server identity so the executor advertises the raw tool name to
	// the model while routing the call back via the canonical
	// "mcp__<server>__<tool>" name. The host discovers these from the connected
	// servers (Manager.ListAllToolInfos).
	McpTools []tools.McpToolInfo
	// DeferredMcpTools are the `defer_loading` MCP tools registered as deferred
	// runtimes: dispatch-only (hidden spec) and advertised solely through
	// tool_search, mirroring the deferred branch of
	// spec_plan::add_mcp_runtime_tools. Each carries the metadata the spec +
	// search text need (server/connector names, namespace description, plugin
	// display names, raw tool). A nil Mcp caller leaves them unable to dispatch.
	DeferredMcpTools []tools.McpToolInfo
	// WebSearch performs web searches.
	WebSearch WebSearchRunner
	// Collab is the multi-agent control plane the deferred collab tools route
	// through (spawn_agent/send_input/resume_agent/wait_agent/close_agent). A nil
	// value leaves the tools registered for tool_search but unable to execute
	// (direct calls report the manager unavailable), matching a run without the
	// multi-agent area assembled.
	Collab CollabControl
	// UserInput routes request_user_input.
	UserInput UserInputRequester
	// Permissions routes request_permissions.
	Permissions PermissionsRequester
}

// builtinExecutors assembles the executor list for the configured dependencies,
// in codex's spec_plan registration order (which fixes the model-visible spec
// order): shell tools, core utility tools (update_plan, request_user_input,
// request_permissions, apply_patch, view_image), MCP runtime tools, then the
// hosted specs (web_search) last.
func builtinExecutors(deps BuiltinToolDeps) []ToolExecutor {
	var execs []ToolExecutor

	// Shell family (spec_plan::add_shell_tools). All shell executors register;
	// the per-turn shell type (shell_type_for_model_and_features) decides which
	// of them advertises a spec — in UnifiedExec mode shell_command stays
	// registered dispatch-only, matching codex's add_dispatch_only. The
	// executors themselves are supplied by core/localexec.
	execs = append(execs, deps.ShellTools...)

	// Core utility tools (spec_plan::add_core_utility_tools order).
	execs = append(execs, planExecutor{})
	if deps.UserInput != nil {
		execs = append(execs, requestUserInputExecutor{req: deps.UserInput})
	}
	if deps.Permissions != nil {
		execs = append(execs, requestPermissionsExecutor{req: deps.Permissions})
	}
	// Token-budget tools (0.147 spec_plan `features.enabled(TokenBudget)`): always
	// registered, advertised only when the turn enables the token budget.
	execs = append(execs, newContextWindowExecutor{}, getContextRemainingExecutor{})
	// apply_patch remains available as a standalone tool for the direct
	// (non-shell) invocation form some models use.
	if deps.ApplyPatch != nil {
		execs = append(execs, deps.ApplyPatch)
	}
	execs = append(execs, viewImageExecutor{})

	// Deferred search sources are collected in spec_plan's add_tool_sources
	// order: add_collaboration_tools runs BEFORE add_mcp_runtime_tools, so the
	// collab agent tools come first in the tool_search corpus, then the deferred
	// MCP tools. This ordering fixes the BM25 document ids (tie-break order).
	var deferredSources []deferredToolSource

	// Deferred collab (multi-agent) runtimes (spec_plan::add_collaboration_tools).
	// They are always registered (dispatch-only) but only participate in
	// tool_search and direct dispatch when the per-turn deferred-exposure gate
	// holds (collabExecutor's Spec is hidden and searchInfo is gated on
	// collabDeferredEligible).
	for _, ce := range collabExecutorsWithDeps(deps) {
		execs = append(execs, ce)
		deferredSources = append(deferredSources, ce)
	}

	// MCP runtime tools (spec_plan::add_mcp_runtime_tools). Eager tools advertise
	// directly; `defer_loading` tools register dispatch-only (hidden spec) and
	// participate in tool_search, mirroring the deferred branch's
	// add_with_exposure(McpHandler, ToolExposure::Deferred).
	if deps.Mcp != nil {
		for _, info := range deps.McpTools {
			apiTool, err := tools.McpToolToResponsesApiTool(info.CanonicalToolName(), info.Tool)
			if err != nil {
				// Mirror the Rust warn!+skip on a tool whose spec fails to build.
				continue
			}
			execs = append(execs, mcpExecutor{
				caller: deps.Mcp,
				spec:   tools.FunctionToolSpec(apiTool),
				// Advertised (model-visible) name is the raw tool name; dispatch uses
				// the canonical "mcp__<server>__<tool>" so routing recovers the server.
				name:      protocol.PlainToolName(info.CallableName),
				qualified: info.CanonicalToolName(),
			})
		}
	}
	for _, de := range deferredMcpExecutors(deps) {
		execs = append(execs, de)
		deferredSources = append(deferredSources, de)
	}

	// tool_search appends after every other runtime
	// (spec_plan::append_tool_search_executor runs after add_tool_sources); its
	// per-turn gate (search-capable model + namespace tools + deferred sources)
	// lives in toolSearchExecutor.Spec. It carries the ordered deferred sources
	// collected above, mirroring ToolSearchHandler::new(search_infos).
	execs = append(execs, toolSearchExecutor{deferredSources: deferredSources})

	// Hosted specs come after every runtime in the model-visible order
	// (build_model_visible_specs_and_registry appends hosted_specs last).
	// web_search is provider-executed, so it registers without a local runner;
	// a wired WebSearch dep additionally enables the local dispatch path.
	execs = append(execs, webSearchExecutor{runner: deps.WebSearch})
	return execs
}

// ----------------------------------------------------------------------------
// Shared tool outputs
// ----------------------------------------------------------------------------

// TextToolOutput is a plain-text tool output, the Go analogue of the Rust
// `FunctionToolOutput::from_text`. A nil success defaults to true for logging.
type TextToolOutput struct {
	tools.DefaultToolOutput
	text    string
	success *bool
}

func NewTextToolOutput(text string, success *bool) TextToolOutput {
	return TextToolOutput{text: text, success: success}
}

func (o TextToolOutput) LogPreview() string { return TelemetryPreview(o.text) }

func (o TextToolOutput) SuccessForLogging() bool {
	if o.success == nil {
		return true
	}
	return *o.success
}

func (o TextToolOutput) ToResponseItem(callID string, payload tools.ToolPayload) tools.ResponseInputItem {
	out := protocol.FunctionCallOutputPayload{Text: &o.text, Success: o.success}
	if payload.Kind == tools.ToolPayloadKindCustom {
		return tools.CustomToolCallOutputInput(callID, nil, out)
	}
	return tools.FunctionCallOutputInput(callID, out)
}

func (o TextToolOutput) CodeModeResult(tools.ToolPayload) json.RawMessage {
	return mustJSON(map[string]any{})
}

// mcpToolOutput wraps an MCP CallToolResult, the Go analogue of the Rust
// `McpToolOutput` (reduced: wall-time header and image sanitization are
// deferred). The result is folded back as an mcp_tool_call_output item.
type mcpToolOutput struct {
	tools.DefaultToolOutput
	result protocol.CallToolResult
}

func (o mcpToolOutput) LogPreview() string {
	raw, err := json.Marshal(o.result.Content)
	if err != nil {
		return TelemetryPreview(fmt.Sprintf("failed to serialize mcp result: %v", err))
	}
	return TelemetryPreview(string(raw))
}

func (o mcpToolOutput) SuccessForLogging() bool {
	return o.result.IsError == nil || !*o.result.IsError
}

func (o mcpToolOutput) ToResponseItem(callID string, _ tools.ToolPayload) tools.ResponseInputItem {
	return tools.McpToolCallOutputInput(callID, o.result)
}

func (o mcpToolOutput) CodeModeResult(tools.ToolPayload) json.RawMessage {
	raw, err := json.Marshal(o.result)
	if err != nil {
		return mustJSON(fmt.Sprintf("failed to serialize mcp result: %v", err))
	}
	return raw
}

// ----------------------------------------------------------------------------
// view_image
// ----------------------------------------------------------------------------

type viewImageExecutor struct{}

func (viewImageExecutor) Name() protocol.ToolName { return protocol.PlainToolName("view_image") }

func (viewImageExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	// Only image-capable models see view_image (codex spec_plan gates it on the
	// model's input_modalities); a text-only catalog entry — or a host that
	// deliberately declares no image input, like airush — gets no spec.
	if !modelAcceptsImages(turnModelInfo(tc)) {
		return tools.ToolSpec{}, false
	}
	// Every model in the bundled 0.136.0 catalog sets
	// supports_image_detail_original = true, so the `detail` enum is always
	// offered (matching create_view_image_tool for the supported models).
	return tools.CreateViewImageTool(tools.ViewImageToolOptions{CanRequestOriginalImageDetail: true}), true
}

// modelAcceptsImages reports whether the model declares image input. An empty
// modality list keeps the historical default (image allowed).
func modelAcceptsImages(info modelsmanager.ModelInfo) bool {
	if len(info.InputModalities) == 0 {
		return true
	}
	for _, m := range info.InputModalities {
		if m == protocol.InputModalityImage {
			return true
		}
	}
	return false
}

func (viewImageExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (viewImageExecutor) Handle(_ context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	args, err := functionPayloadArguments(h.ToolName, h.Payload)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Path string `json:"path"`
	}
	if perr := json.Unmarshal([]byte(args), &parsed); perr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to parse function arguments: %v", perr))
	}
	if strings.TrimSpace(parsed.Path) == "" {
		return nil, tools.RespondToModelError("view_image requires a non-empty path")
	}
	// Emit the visible view-image event so the UI attaches the image.
	if h.Session != nil {
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type: protocol.EventMsgKindViewImageToolCall,
			ViewImageToolCall: &protocol.ViewImageToolCallEvent{
				CallID: h.CallID,
				Path:   protocol.AbsolutePath(parsed.Path),
			},
		})
	}
	// STUB: the real handler loads + attaches the image bytes as an input image
	// content item; here we acknowledge the attachment to the model.
	return NewTextToolOutput("attached local image at "+parsed.Path, boolPtr(true)), nil
}

// ----------------------------------------------------------------------------
// update_plan
// ----------------------------------------------------------------------------

type planExecutor struct{}

const planUpdatedMessage = "Plan updated"

func (planExecutor) Name() protocol.ToolName { return protocol.PlainToolName("update_plan") }

func (planExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	return tools.CreateUpdatePlanTool(), true
}

func (planExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (planExecutor) Handle(_ context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	args, err := functionPayloadArguments(h.ToolName, h.Payload)
	if err != nil {
		return nil, err
	}
	if h.Turn.CollaborationMode.Mode == protocol.ModeKindPlan {
		return nil, tools.RespondToModelError("update_plan is a TODO/checklist tool and is not allowed in Plan mode")
	}
	var parsed protocol.UpdatePlanArgs
	if perr := json.Unmarshal([]byte(args), &parsed); perr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to parse function arguments: %v", perr))
	}
	if h.Session != nil {
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type:       protocol.EventMsgKindPlanUpdate,
			PlanUpdate: &parsed,
		})
	}
	return NewTextToolOutput(planUpdatedMessage, boolPtr(true)), nil
}

// ----------------------------------------------------------------------------
// request_user_input
// ----------------------------------------------------------------------------

type requestUserInputExecutor struct {
	req UserInputRequester
}

func (requestUserInputExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName("request_user_input")
}

// Spec advertises request_user_input when the turn enables it (codex's
// config.experimental_request_user_input_enabled, default true). The
// description embeds the collaboration modes that allow the tool (Plan by
// default), mirroring request_user_input_spec.rs.
func (requestUserInputExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if !turnRequestUserInputEnabled(tc) {
		return tools.ToolSpec{}, false
	}
	modes := tools.RequestUserInputAvailableModes(turnFeatures(tc))
	return tools.CreateRequestUserInputTool(tools.RequestUserInputToolDescription(modes)), true
}

func (requestUserInputExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (e requestUserInputExecutor) Handle(ctx context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	args, err := functionPayloadArguments(h.ToolName, h.Payload)
	if err != nil {
		return nil, err
	}
	var parsed protocol.RequestUserInputArgs
	if perr := json.Unmarshal([]byte(args), &parsed); perr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to parse function arguments: %v", perr))
	}
	resp, ok := e.req.RequestUserInput(ctx, h.CallID, parsed)
	if !ok {
		return nil, tools.RespondToModelError("request_user_input was cancelled before receiving a response")
	}
	content, merr := json.Marshal(resp)
	if merr != nil {
		return nil, tools.FatalError(fmt.Sprintf("failed to serialize request_user_input response: %v", merr))
	}
	return NewTextToolOutput(string(content), boolPtr(true)), nil
}

// ----------------------------------------------------------------------------
// request_permissions
// ----------------------------------------------------------------------------

type requestPermissionsExecutor struct {
	req PermissionsRequester
}

func (requestPermissionsExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName("request_permissions")
}

func (requestPermissionsExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	return functionSpecStub("request_permissions", "Request additional sandbox permissions."), true
}

func (requestPermissionsExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (e requestPermissionsExecutor) Handle(ctx context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	args, err := functionPayloadArguments(h.ToolName, h.Payload)
	if err != nil {
		return nil, err
	}
	var parsed protocol.RequestPermissionsArgs
	if perr := json.Unmarshal([]byte(args), &parsed); perr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to parse function arguments: %v", perr))
	}
	// STUB: additional-permission normalization/validation
	// (normalize_additional_permissions) is owned by the permissions area.
	resp, ok := e.req.RequestPermissions(ctx, h.CallID, parsed)
	if !ok {
		return nil, tools.RespondToModelError("request_permissions was cancelled before receiving a response")
	}
	content, merr := json.Marshal(resp)
	if merr != nil {
		return nil, tools.FatalError(fmt.Sprintf("failed to serialize request_permissions response: %v", merr))
	}
	return NewTextToolOutput(string(content), boolPtr(true)), nil
}

// ----------------------------------------------------------------------------
// web_search
// ----------------------------------------------------------------------------

type webSearchExecutor struct {
	runner WebSearchRunner
}

func (webSearchExecutor) Name() protocol.ToolName { return protocol.PlainToolName("web_search") }

// Spec advertises the hosted web_search tool (executed server-side by the
// model provider). Mirrors hosted_model_tool_specs: the effective mode (codex
// config.web_search_mode, default cached) selects external_web_access, and the
// model's web_search_tool_type selects the content types.
//
// The provider capability gate (provider.capabilities().web_search) mirrors the
// Rust spec_plan: when the provider does not support hosted web search, the
// web_search_mode is dropped (None) and create_web_search_tool returns no spec.
// For OpenAI/configured providers the capability is true, so this is a no-op.
func (webSearchExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if !turnProviderCapabilities(tc).WebSearch {
		return tools.ToolSpec{}, false
	}
	mode := turnWebSearchMode(tc)
	return tools.CreateWebSearchTool(tools.WebSearchToolOptions{
		WebSearchMode:     &mode,
		WebSearchToolType: turnModelInfo(tc).WebSearchToolType,
	})
}

func (webSearchExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (e webSearchExecutor) Handle(ctx context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	// web_search is a hosted tool: the model provider executes it server-side
	// and no function call should reach the local registry. The runner-backed
	// path below serves clients that wire a local searcher (tests, OSS providers).
	if e.runner == nil {
		return nil, tools.RespondToModelError("web_search is executed by the model provider; no local handler is available")
	}
	args, err := functionPayloadArguments(h.ToolName, h.Payload)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Query string `json:"query"`
	}
	if perr := json.Unmarshal([]byte(args), &parsed); perr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to parse function arguments: %v", perr))
	}
	if strings.TrimSpace(parsed.Query) == "" {
		return nil, tools.RespondToModelError("web_search requires a non-empty query")
	}
	if h.Session != nil {
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type:           protocol.EventMsgKindWebSearchBegin,
			WebSearchBegin: &protocol.WebSearchBeginEvent{CallID: h.CallID},
		})
	}
	result, serr := e.runner.Search(ctx, parsed.Query)
	if serr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("web_search failed: %v", serr))
	}
	if h.Session != nil {
		query := parsed.Query
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type: protocol.EventMsgKindWebSearchEnd,
			WebSearchEnd: &protocol.WebSearchEndEvent{
				CallID: h.CallID,
				Query:  parsed.Query,
				Action: protocol.WebSearchAction{
					Type:  protocol.WebSearchActionKind("search"),
					Query: &query,
				},
			},
		})
	}
	return tools.NewJsonToolOutput(result), nil
}

// ----------------------------------------------------------------------------
// MCP tools
// ----------------------------------------------------------------------------

type mcpExecutor struct {
	caller McpToolCaller
	spec   tools.ToolSpec
	// name is the model-visible name the router matches an incoming call against.
	name protocol.ToolName
	// qualified is the fully-qualified "mcp__<server>__<tool>" name used to route
	// the call back to the owning server. When zero it falls back to name (the
	// deferred path sets name to the canonical namespaced form already).
	qualified protocol.ToolName
}

// dispatchName returns the fully-qualified name used to route the MCP call. It
// prefers the explicit qualified name (eager tools, whose model-visible name is
// the raw tool name) and falls back to name (deferred tools, whose name is the
// canonical namespaced form).
func (e mcpExecutor) dispatchName() protocol.ToolName {
	if e.qualified.Name != "" {
		return e.qualified
	}
	return e.name
}

func (e mcpExecutor) Name() protocol.ToolName { return e.name }

func (e mcpExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) { return e.spec, true }

func (mcpExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (e mcpExecutor) Handle(ctx context.Context, h *ToolHandlerContext) (tools.ToolOutput, error) {
	args, err := functionPayloadArguments(h.ToolName, h.Payload)
	if err != nil {
		return nil, err
	}
	arguments := json.RawMessage(args)
	if len(strings.TrimSpace(args)) == 0 {
		arguments = json.RawMessage("{}")
	}
	dispatch := e.dispatchName().String()
	server, tool := splitQualifiedToolName(dispatch)

	if h.Session != nil {
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type: protocol.EventMsgKindMcpToolCallBegin,
			McpToolCallBegin: &protocol.McpToolCallBeginEvent{
				CallID: h.CallID,
				Invocation: protocol.McpInvocation{
					Server:    server,
					Tool:      tool,
					Arguments: arguments,
				},
			},
		})
	}

	result, callErr := e.caller.CallQualifiedTool(ctx, dispatch, arguments, nil)
	if callErr != nil {
		if h.Session != nil {
			errMsg := callErr.Error()
			h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
				Type: protocol.EventMsgKindMcpToolCallEnd,
				McpToolCallEnd: &protocol.McpToolCallEndEvent{
					CallID: h.CallID,
					Invocation: protocol.McpInvocation{
						Server:    server,
						Tool:      tool,
						Arguments: arguments,
					},
					Result: mustJSON(map[string]string{"Err": errMsg}),
				},
			})
		}
		return nil, tools.RespondToModelError(fmt.Sprintf("mcp tool %s failed: %v", e.name, callErr))
	}

	if h.Session != nil {
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type: protocol.EventMsgKindMcpToolCallEnd,
			McpToolCallEnd: &protocol.McpToolCallEndEvent{
				CallID: h.CallID,
				Invocation: protocol.McpInvocation{
					Server:    server,
					Tool:      tool,
					Arguments: arguments,
				},
				Result: mustJSON(map[string]protocol.CallToolResult{"Ok": result}),
			},
		})
	}
	return mcpToolOutput{result: result}, nil
}

// splitQualifiedToolName splits a fully-qualified "mcp__<server>__<tool>" name
// into its server and tool components (used for tool-call event display). The
// leading "mcp__" prefix is stripped first so the split lands on the
// server/tool boundary rather than after "mcp". When no separator is present the
// whole (prefix-stripped) name is returned as the tool with an empty server.
func splitQualifiedToolName(qualified string) (server, tool string) {
	const prefix = "mcp__"
	const sep = "__"
	rest := strings.TrimPrefix(qualified, prefix)
	if idx := strings.Index(rest, sep); idx >= 0 {
		return rest[:idx], rest[idx+len(sep):]
	}
	return "", rest
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// functionSpecStub builds a minimal function ToolSpec for routing. The real
// per-tool specs (with JSON schemas) are owned by the spec_plan area; these
// stubs keep the model-visible list populated for the turn-running subset.
func functionSpecStub(name, description string) tools.ToolSpec {
	return tools.FunctionToolSpec(tools.ResponsesApiTool{
		Name:        name,
		Description: description,
		Strict:      false,
		Parameters:  emptyObjectSchema(),
	})
}

// emptyObjectSchema returns a permissive object schema for stub specs.
func emptyObjectSchema() tools.JsonSchema {
	return tools.JsonSchema{
		Type:       tools.SingleType(tools.JsonSchemaPrimitiveTypeObject),
		Properties: map[string]tools.JsonSchema{},
	}
}

// TelemetryPreview returns a short preview of content for log output. It mirrors
// the byte/line-bounded preview the tools package applies internally (which is
// unexported), keeping core's outputs log-friendly.
func TelemetryPreview(content string) string {
	const maxBytes = 2 * 1024
	if len(content) <= maxBytes {
		return content
	}
	return content[:maxBytes] + "\n[... telemetry preview truncated ...]"
}

// functionPayloadArguments returns the raw function-call arguments, mirroring
// the Rust `ToolCall::function_arguments` guard: it is a fatal error if the
// payload is not a Function payload.
func functionPayloadArguments(name protocol.ToolName, p tools.ToolPayload) (string, error) {
	if p.Kind == tools.ToolPayloadKindFunction {
		return p.Arguments, nil
	}
	return "", tools.FatalError(fmt.Sprintf("tool %s invoked with incompatible payload", name))
}

// PayloadArguments returns the raw argument string for a Function or Custom
// payload, erroring for other shapes.
func PayloadArguments(p tools.ToolPayload) (string, error) {
	switch p.Kind {
	case tools.ToolPayloadKindFunction:
		return p.Arguments, nil
	case tools.ToolPayloadKindCustom:
		return p.Input, nil
	default:
		return "", tools.FatalError("tool invoked with incompatible payload")
	}
}

// boolPtr returns a pointer to b.
func boolPtr(b bool) *bool { return &b }

// mustJSON marshals v, returning a JSON-string error value on failure so the
// caller always gets valid JSON.
func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		fallback, _ := json.Marshal(fmt.Sprintf("failed to serialize: %v", err))
		return fallback
	}
	return raw
}
