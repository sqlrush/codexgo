package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/applypatch"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// This file ports the core tool-dispatch handlers from codex-core's
// tools/handlers/*. Each built-in tool is a small [toolExecutor] that parses its
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
// Nil fields disable the corresponding tool: e.g. a nil Exec omits exec_command.
type BuiltinToolDeps struct {
	// Exec runs sandboxed shell commands (exec_command / apply_patch fallback).
	Exec ExecService
	// Mcp invokes MCP tools.
	Mcp McpToolCaller
	// McpTools are the model-visible MCP tool specs to advertise this turn.
	// STUB: the real router discovers these from connected servers; here they
	// are injected so core can route to them.
	McpTools []tools.ToolSpec
	// WebSearch performs web searches.
	WebSearch WebSearchRunner
	// UserInput routes request_user_input.
	UserInput UserInputRequester
	// Permissions routes request_permissions.
	Permissions PermissionsRequester
	// PatchFS is the filesystem apply_patch writes to; nil uses the real OS FS.
	PatchFS applypatch.FileSystem
}

// builtinExecutors assembles the executor list for the configured dependencies.
func builtinExecutors(deps BuiltinToolDeps) []toolExecutor {
	var execs []toolExecutor
	// view_image and update_plan have no external dependency.
	execs = append(execs, viewImageExecutor{}, planExecutor{})

	if deps.Exec != nil {
		// shell_command is the gpt-5.5 default shell tool: it takes a `command`
		// STRING, wraps it in the user's shell, and intercepts apply_patch heredocs.
		execs = append(execs, newShellCommandExecutor(deps.Exec, deps.PatchFS))
		// exec_command is the PTY-oriented variant (takes a `cmd` STRING) kept for
		// models that use it; it shares the shell_command execution path.
		execs = append(execs, newExecCommandStringExecutor(deps.Exec, deps.PatchFS))
	}
	// apply_patch remains available as a standalone tool for the direct (non-shell)
	// invocation form some models use.
	execs = append(execs, applyPatchExecutor{fs: deps.PatchFS})
	if deps.UserInput != nil {
		execs = append(execs, requestUserInputExecutor{req: deps.UserInput})
	}
	if deps.Permissions != nil {
		execs = append(execs, requestPermissionsExecutor{req: deps.Permissions})
	}
	if deps.WebSearch != nil {
		execs = append(execs, webSearchExecutor{runner: deps.WebSearch})
	}
	if deps.Mcp != nil {
		for _, spec := range deps.McpTools {
			execs = append(execs, mcpExecutor{caller: deps.Mcp, spec: spec, name: protocol.PlainToolName(spec.Name())})
		}
	}
	return execs
}

// ----------------------------------------------------------------------------
// Shared tool outputs
// ----------------------------------------------------------------------------

// textToolOutput is a plain-text tool output, the Go analogue of the Rust
// `FunctionToolOutput::from_text`. A nil success defaults to true for logging.
type textToolOutput struct {
	tools.DefaultToolOutput
	text    string
	success *bool
}

func newTextToolOutput(text string, success *bool) textToolOutput {
	return textToolOutput{text: text, success: success}
}

func (o textToolOutput) LogPreview() string { return telemetryPreview(o.text) }

func (o textToolOutput) SuccessForLogging() bool {
	if o.success == nil {
		return true
	}
	return *o.success
}

func (o textToolOutput) ToResponseItem(callID string, payload tools.ToolPayload) tools.ResponseInputItem {
	out := protocol.FunctionCallOutputPayload{Text: &o.text, Success: o.success}
	if payload.Kind == tools.ToolPayloadKindCustom {
		return tools.CustomToolCallOutputInput(callID, nil, out)
	}
	return tools.FunctionCallOutputInput(callID, out)
}

func (o textToolOutput) CodeModeResult(tools.ToolPayload) json.RawMessage {
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
		return telemetryPreview(fmt.Sprintf("failed to serialize mcp result: %v", err))
	}
	return telemetryPreview(string(raw))
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

func (viewImageExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	// Every model in the bundled 0.136.0 catalog sets
	// supports_image_detail_original = true, so the `detail` enum is always
	// offered (matching create_view_image_tool for the supported models).
	return tools.CreateViewImageTool(tools.ViewImageToolOptions{CanRequestOriginalImageDetail: true}), true
}

func (viewImageExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (viewImageExecutor) Handle(_ context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
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
	return newTextToolOutput("attached local image at "+parsed.Path, boolPtr(true)), nil
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

func (planExecutor) Handle(_ context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
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
	return newTextToolOutput(planUpdatedMessage, boolPtr(true)), nil
}

// ----------------------------------------------------------------------------
// exec_command / shell_command (shell)
//
// The shell tool executors live in shell_command_executor.go. The gpt-5.5
// `shell_command` tool (a `command` STRING) and the PTY-oriented `exec_command`
// tool (a `cmd` STRING) both wrap their command string in the user's shell, run
// it through the ExecService, and intercept apply_patch heredocs.
// ----------------------------------------------------------------------------

// ----------------------------------------------------------------------------
// apply_patch
// ----------------------------------------------------------------------------

type applyPatchExecutor struct {
	fs applypatch.FileSystem
}

func (applyPatchExecutor) Name() protocol.ToolName { return protocol.PlainToolName("apply_patch") }

func (applyPatchExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	// codex advertises apply_patch as a FREEFORM (custom) grammar tool, not a
	// function (create_apply_patch_freeform_tool). The handler already accepts the
	// custom raw-text payload via extractPatch.
	return tools.CreateApplyPatchFreeformTool(), true
}

func (applyPatchExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction || p.Kind == tools.ToolPayloadKindCustom
}

// applyPatchArgs is the apply_patch argument shape: the patch text under "input".
type applyPatchArgs struct {
	Input string `json:"input"`
}

func (a applyPatchExecutor) Handle(_ context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
	patch, err := a.extractPatch(h.Payload)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(patch) == "" {
		return nil, tools.RespondToModelError("apply_patch requires a non-empty patch")
	}

	cwd, cerr := abspath.FromAbsolutePath(h.Turn.Cwd)
	if cerr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("invalid cwd for apply_patch: %v", cerr))
	}
	fs := a.fs
	if fs == nil {
		fs = applypatch.OSFileSystem{}
	}

	var stdout, stderr bytes.Buffer
	_, applyErr := applypatch.ApplyPatch(patch, cwd, &stdout, &stderr, fs)
	if applyErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = applyErr.Error()
		}
		// STUB: approval escalation on sandbox-denied writes is deferred; a
		// failed apply is surfaced to the model verbatim.
		return nil, tools.RespondToModelError(msg)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = "Patch applied."
	}
	return applyPatchToolOutput{text: out}, nil
}

// extractPatch resolves the patch text from a Function (JSON {"input": ...}) or
// Custom (raw input) payload.
func (applyPatchExecutor) extractPatch(p tools.ToolPayload) (string, error) {
	switch p.Kind {
	case tools.ToolPayloadKindCustom:
		return p.Input, nil
	case tools.ToolPayloadKindFunction:
		var parsed applyPatchArgs
		if err := json.Unmarshal([]byte(p.Arguments), &parsed); err != nil {
			// Tolerate a bare-string argument payload.
			var bare string
			if jerr := json.Unmarshal([]byte(p.Arguments), &bare); jerr == nil {
				return bare, nil
			}
			return "", tools.RespondToModelError(fmt.Sprintf("failed to parse apply_patch arguments: %v", err))
		}
		return parsed.Input, nil
	default:
		return "", tools.FatalError("apply_patch invoked with incompatible payload")
	}
}

// applyPatchToolOutput is the Go analogue of the Rust `ApplyPatchToolOutput`.
type applyPatchToolOutput struct {
	tools.DefaultToolOutput
	text string
}

func (o applyPatchToolOutput) LogPreview() string      { return telemetryPreview(o.text) }
func (o applyPatchToolOutput) SuccessForLogging() bool { return true }

func (o applyPatchToolOutput) ToResponseItem(callID string, payload tools.ToolPayload) tools.ResponseInputItem {
	out := protocol.FunctionCallOutputPayload{Text: &o.text, Success: boolPtr(true)}
	if payload.Kind == tools.ToolPayloadKindCustom {
		return tools.CustomToolCallOutputInput(callID, nil, out)
	}
	return tools.FunctionCallOutputInput(callID, out)
}

func (o applyPatchToolOutput) PostToolUseResponse(string, tools.ToolPayload) (json.RawMessage, bool) {
	return mustJSON(o.text), true
}

func (o applyPatchToolOutput) CodeModeResult(tools.ToolPayload) json.RawMessage {
	return mustJSON(map[string]any{})
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

func (requestUserInputExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	return functionSpecStub("request_user_input", "Ask the user one or more questions."), true
}

func (requestUserInputExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (e requestUserInputExecutor) Handle(ctx context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
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
	return newTextToolOutput(string(content), boolPtr(true)), nil
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

func (e requestPermissionsExecutor) Handle(ctx context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
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
	return newTextToolOutput(string(content), boolPtr(true)), nil
}

// ----------------------------------------------------------------------------
// web_search
// ----------------------------------------------------------------------------

type webSearchExecutor struct {
	runner WebSearchRunner
}

func (webSearchExecutor) Name() protocol.ToolName { return protocol.PlainToolName("web_search") }

func (webSearchExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	return functionSpecStub("web_search", "Search the web for relevant information."), true
}

func (webSearchExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (e webSearchExecutor) Handle(ctx context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
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
	name   protocol.ToolName
}

func (e mcpExecutor) Name() protocol.ToolName { return e.name }

func (e mcpExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) { return e.spec, true }

func (mcpExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

func (e mcpExecutor) Handle(ctx context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
	args, err := functionPayloadArguments(h.ToolName, h.Payload)
	if err != nil {
		return nil, err
	}
	arguments := json.RawMessage(args)
	if len(strings.TrimSpace(args)) == 0 {
		arguments = json.RawMessage("{}")
	}
	server, tool := splitQualifiedToolName(e.name.String())

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

	result, callErr := e.caller.CallQualifiedTool(ctx, e.name.String(), arguments, nil)
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

// splitQualifiedToolName splits a "server__tool" qualified name into its server
// and tool components. When no separator is present the whole name is returned as
// the tool with an empty server.
func splitQualifiedToolName(qualified string) (server, tool string) {
	const sep = "__"
	if idx := strings.Index(qualified, sep); idx >= 0 {
		return qualified[:idx], qualified[idx+len(sep):]
	}
	return "", qualified
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

// telemetryPreview returns a short preview of content for log output. It mirrors
// the byte/line-bounded preview the tools package applies internally (which is
// unexported), keeping core's outputs log-friendly.
func telemetryPreview(content string) string {
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

// payloadArguments returns the raw argument string for a Function or Custom
// payload, erroring for other shapes.
func payloadArguments(p tools.ToolPayload) (string, error) {
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
