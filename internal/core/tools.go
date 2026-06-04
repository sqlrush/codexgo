package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// This file ports codex-core's `tools/router.rs` + `tools/mod.rs` and the core
// tool-dispatch entry point. It provides a concrete [ToolRouter] (the DI
// interface declared in managers.go) backed by a registry of executors for the
// built-in tools, plus the [BuildToolCall] parser that turns a model-emitted
// [protocol.ResponseItem] into a routable [ParsedToolCall].
//
// The Rust router carries a richer surface (parallel-call gating,
// runtime-cancellation waits, argument-diff consumers, code-mode results, hook
// orchestration). Those are noted as STUB where omitted; the turn-running subset
// implemented here parses the call, routes to an executor, and folds the result
// back into history as a function-call output item.

// ----------------------------------------------------------------------------
// ParsedToolCall + payload (mirrors Rust ToolCall / ToolPayload)
// ----------------------------------------------------------------------------

// ParsedToolCall is a model-requested tool call resolved from a response item.
// It is the Go analogue of the Rust `tools::router::ToolCall`: a tool name, the
// correlating call id, and the canonical payload variant.
//
// It is constructed once by [BuildToolCall] and then treated as read-only.
type ParsedToolCall struct {
	// ToolName is the (possibly namespaced) tool name.
	ToolName protocol.ToolName
	// CallID correlates the call with its output item.
	CallID string
	// Payload is the canonical payload for the call.
	Payload tools.ToolPayload
}

// ----------------------------------------------------------------------------
// AnyToolResult (mirrors Rust registry::AnyToolResult)
// ----------------------------------------------------------------------------

// AnyToolResult is the dispatched outcome of a tool call: the originating call
// id and payload plus the executor's [tools.ToolOutput]. It is the Go analogue
// of the Rust `registry::AnyToolResult` and knows how to fold itself back into
// conversation history.
//
// STUB: the Rust value also carries a post-tool-use hook payload; hook wiring is
// owned by the hooks area and is omitted here.
type AnyToolResult struct {
	// CallID is the call this result answers.
	CallID string
	// Payload is the payload the call was dispatched with.
	Payload tools.ToolPayload
	// Output is the executor's model-facing output.
	Output tools.ToolOutput
}

// IntoResponse folds the result into an input-side conversation item, mirroring
// the Rust `AnyToolResult::into_response`.
func (r AnyToolResult) IntoResponse() tools.ResponseInputItem {
	return r.Output.ToResponseItem(r.CallID, r.Payload)
}

// ToToolResult projects the dispatched outcome onto the foundation's [ToolResult]
// view (textual output + success flag) used by the turn runner.
func (r AnyToolResult) ToToolResult() ToolResult {
	return ToolResult{
		Output:  toolOutputText(r.Output, r.CallID, r.Payload),
		Success: r.Output.SuccessForLogging(),
	}
}

// toolOutputText extracts the model-facing text from a tool output's response
// item, falling back to the empty string for non-text bodies.
func toolOutputText(out tools.ToolOutput, callID string, payload tools.ToolPayload) string {
	item := out.ToResponseItem(callID, payload)
	switch item.Kind {
	case tools.ResponseInputItemKindFunctionCallOutput,
		tools.ResponseInputItemKindCustomToolCallOutput:
		return functionOutputText(item.Output)
	default:
		return ""
	}
}

// functionOutputText renders a [protocol.FunctionCallOutputPayload] as plain
// text, concatenating input-text content items.
func functionOutputText(p protocol.FunctionCallOutputPayload) string {
	if p.Text != nil {
		return *p.Text
	}
	var s string
	for _, item := range p.ContentItems {
		if item.Type == protocol.FunctionCallOutputContentItemKindInputText {
			s += item.Text
		}
	}
	return s
}

// ----------------------------------------------------------------------------
// Executor contract (reduced core analogue of CoreToolRuntime)
// ----------------------------------------------------------------------------

// toolExecutor is the narrow, core-internal contract a built-in tool runtime
// implements. It is a reduced port of the Rust `CoreToolRuntime`/`ToolExecutor`
// traits: it advertises its model-visible name and spec and handles one
// invocation.
//
// STUB: parallel-call support, runtime-cancellation waits, pre/post hook
// payloads, telemetry tags, and argument-diff consumers are omitted; those are
// owned by the parallel/hooks/telemetry area agents.
type toolExecutor interface {
	// Name returns the registry key (and model-visible name) for the tool.
	Name() protocol.ToolName
	// Spec returns the model-visible specification for the tool, or false when
	// the tool is hidden (dispatch-only) for the current turn.
	Spec(tc *TurnContext) (tools.ToolSpec, bool)
	// MatchesPayload reports whether the executor accepts the given payload
	// shape, mirroring the Rust `matches_kind` guard.
	MatchesPayload(p tools.ToolPayload) bool
	// Handle executes one invocation, returning its output or a typed
	// [tools.FunctionCallError].
	Handle(ctx context.Context, h *toolHandlerContext) (tools.ToolOutput, error)
}

// toolHandlerContext bundles the per-invocation inputs handed to an executor. It
// is the reduced Go analogue of the Rust `ToolInvocation`.
type toolHandlerContext struct {
	// Session is the owning session (for events, history, approvals).
	Session *Session
	// Turn is the read-only turn snapshot.
	Turn *TurnContext
	// CallID correlates the call with its output.
	CallID string
	// ToolName is the resolved tool name.
	ToolName protocol.ToolName
	// Payload is the canonical payload for this call.
	Payload tools.ToolPayload
}

// ----------------------------------------------------------------------------
// DefaultToolRouter (mirrors Rust tools::router::ToolRouter)
// ----------------------------------------------------------------------------

// DefaultToolRouter is the concrete [ToolRouter] used by the turn runner. It
// owns a registry of [toolExecutor]s keyed by tool name and resolves the
// model-visible spec set per turn. It is the Go analogue of the Rust
// `ToolRouter` (turn-running subset).
//
// A router is immutable after construction; [SpecsForTurn] and [Dispatch] read
// the registry concurrently.
type DefaultToolRouter struct {
	executors map[string]toolExecutor
	// order preserves a stable insertion order so the model-visible spec list is
	// deterministic across turns (the Rust registry uses a HashMap but the
	// spec_plan emits a stable order; we keep insertion order here).
	order []string
}

var _ ToolRouter = (*DefaultToolRouter)(nil)

// NewDefaultToolRouter builds a router from a set of executors. Duplicate tool
// names are rejected (the last registration wins after a logged conflict is not
// possible without a logger, so the constructor errors instead, matching the
// Rust `error_or_panic` intent at the system boundary).
func NewDefaultToolRouter(executors ...toolExecutor) (*DefaultToolRouter, error) {
	r := &DefaultToolRouter{executors: make(map[string]toolExecutor, len(executors))}
	for _, ex := range executors {
		if ex == nil {
			continue
		}
		key := ex.Name().String()
		if _, dup := r.executors[key]; dup {
			return nil, fmt.Errorf("core: tool %q already registered", key)
		}
		r.executors[key] = ex
		r.order = append(r.order, key)
	}
	return r, nil
}

// BuiltinToolRouter constructs a router wired with the built-in tool executors
// for the turn-running subset, using the supplied injected dependencies. Callers
// pass nil for a dependency to omit the tools that depend on it (e.g. a nil
// McpToolCaller omits MCP routing).
func BuiltinToolRouter(deps BuiltinToolDeps) (*DefaultToolRouter, error) {
	executors := builtinExecutors(deps)
	return NewDefaultToolRouter(executors...)
}

// SpecsForTurn returns the model-visible tool specs for the turn, in stable
// registration order. Executors that report a hidden spec for the turn are
// skipped. Mirrors the Rust `ToolRouter::model_visible_specs` (resolved against
// the turn context).
func (r *DefaultToolRouter) SpecsForTurn(_ context.Context, tc *TurnContext) ([]tools.ToolSpec, error) {
	if tc == nil {
		return nil, fmt.Errorf("core: SpecsForTurn requires a turn context")
	}
	specs := make([]tools.ToolSpec, 0, len(r.order))
	for _, key := range r.order {
		ex := r.executors[key]
		if spec, ok := ex.Spec(tc); ok {
			specs = append(specs, spec)
		}
	}
	return specs, nil
}

// Dispatch routes a single invocation to its executor and adapts the dispatched
// outcome onto the foundation's [ToolResult]. It is the [ToolRouter] entry point
// used by the turn runner. Use [DefaultToolRouter.DispatchAny] when the caller
// needs the richer [AnyToolResult] (e.g. to fold a structured response item back
// into history).
func (r *DefaultToolRouter) Dispatch(ctx context.Context, tc *TurnContext, inv ToolInvocation) (ToolResult, error) {
	res, err := r.DispatchAny(ctx, tc, inv)
	if err != nil {
		return ToolResult{}, err
	}
	return res.ToToolResult(), nil
}

// DispatchAny routes a single invocation to its executor and returns the full
// [AnyToolResult]. It mirrors the Rust `ToolRegistry::dispatch_any`: it looks up
// the tool, validates the payload shape, increments the turn tool-call counter,
// runs the executor, and wraps the output.
//
// The owning [Session] is taken from tc when available; callers that need to
// supply it explicitly (e.g. for approvals/events from inside an executor)
// should use [DefaultToolRouter.DispatchParsed].
//
// Errors are typed [*tools.FunctionCallError]: RespondToModel for recoverable
// failures (surfaced verbatim to the model) and Fatal for unrecoverable ones.
func (r *DefaultToolRouter) DispatchAny(ctx context.Context, tc *TurnContext, inv ToolInvocation) (AnyToolResult, error) {
	payload := tools.FunctionPayload(string(inv.Arguments))
	return r.dispatch(ctx, nil, tc, inv.CallID, inv.Name, payload)
}

// dispatch is the shared dispatch core used by both [DefaultToolRouter.Dispatch]
// and [DefaultToolRouter.DispatchParsed]. It looks up the tool, validates the
// payload shape, accounts the call against the active turn, and runs the
// executor.
func (r *DefaultToolRouter) dispatch(ctx context.Context, sess *Session, tc *TurnContext, callID string, name protocol.ToolName, payload tools.ToolPayload) (AnyToolResult, error) {
	if tc == nil {
		return AnyToolResult{}, tools.FatalError("dispatch requires a turn context")
	}

	// Account the tool call against the active turn, mirroring the Rust
	// dispatch's tool_calls increment.
	if sess != nil {
		if at := sess.ActiveTurn(); at != nil && at.State != nil {
			at.State.IncToolCalls()
		}
	}

	key := name.String()
	ex, ok := r.executors[key]
	if !ok {
		return AnyToolResult{}, tools.RespondToModelError(unsupportedToolCallMessage(payload, name))
	}
	if !ex.MatchesPayload(payload) {
		return AnyToolResult{}, tools.FatalError(fmt.Sprintf("tool %s invoked with incompatible payload", name))
	}

	hctx := &toolHandlerContext{
		Session:  sess,
		Turn:     tc,
		CallID:   callID,
		ToolName: name,
		Payload:  payload,
	}
	out, err := ex.Handle(ctx, hctx)
	if err != nil {
		return AnyToolResult{}, err
	}
	return AnyToolResult{CallID: callID, Payload: payload, Output: out}, nil
}

// registeredToolNames returns the registry keys in registration order. Exposed
// for tests, mirroring the Rust `registered_tool_names_for_test`.
func (r *DefaultToolRouter) registeredToolNames() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	sort.Strings(out)
	return out
}

// ----------------------------------------------------------------------------
// BuildToolCall (mirrors Rust ToolRouter::build_tool_call)
// ----------------------------------------------------------------------------

// BuildToolCall parses a model-emitted [protocol.ResponseItem] into a routable
// [ParsedToolCall], returning (nil, nil) for items that are not tool calls (so
// the caller can skip them). It is a faithful port of the Rust
// `ToolRouter::build_tool_call`.
//
// Supported variants:
//   - FunctionCall      -> Function payload (namespaced tool name preserved)
//   - ToolSearchCall     -> ToolSearch payload (only when execution=="client"
//     and a call id is present; other tool-search calls return nil)
//   - CustomToolCall      -> Custom payload (plain tool name)
//
// A malformed tool_search argument payload is a recoverable, model-facing error.
func BuildToolCall(item protocol.ResponseItem) (*ParsedToolCall, error) {
	switch item.Type {
	case protocol.ResponseItemKindFunctionCall:
		name := protocol.ToolName{Name: item.Name, Namespace: item.Namespace}
		return &ParsedToolCall{
			ToolName: name,
			CallID:   item.CallID,
			Payload:  tools.FunctionPayload(item.Arguments),
		}, nil

	case protocol.ResponseItemKindToolSearchCall:
		// Only client-side searches with a concrete call id are routed; others
		// (e.g. server-executed searches) are folded back by the model and
		// skipped here.
		if item.Execution != "client" || item.CallIDOpt == nil {
			return nil, nil
		}
		var params tools.SearchToolCallParams
		if len(item.ArgumentsValue) > 0 && string(item.ArgumentsValue) != "null" {
			if err := json.Unmarshal(item.ArgumentsValue, &params); err != nil {
				return nil, tools.RespondToModelError(fmt.Sprintf("failed to parse tool_search arguments: %v", err))
			}
		}
		return &ParsedToolCall{
			ToolName: protocol.PlainToolName("tool_search"),
			CallID:   *item.CallIDOpt,
			Payload:  tools.ToolSearchPayload(params),
		}, nil

	case protocol.ResponseItemKindCustomToolCall:
		return &ParsedToolCall{
			ToolName: protocol.PlainToolName(item.Name),
			CallID:   item.CallID,
			Payload:  tools.CustomPayload(item.Input),
		}, nil

	default:
		return nil, nil
	}
}

// ToInvocation projects a parsed call onto the foundation's [ToolInvocation]
// (the DI surface). The Arguments slice carries the raw payload bytes so the
// foundation surface stays payload-shape agnostic. Note this lossily flattens
// the payload to its raw bytes; prefer [DefaultToolRouter.DispatchParsed] when
// the exact payload variant (e.g. ToolSearch vs Custom) must be preserved.
func (c ParsedToolCall) ToInvocation() ToolInvocation {
	return ToolInvocation{
		CallID:    c.CallID,
		Name:      c.ToolName,
		Arguments: rawArgumentsForPayload(c.Payload),
	}
}

// DispatchParsed dispatches a [ParsedToolCall] directly, preserving its exact
// payload variant (rather than re-deriving it from raw bytes) and threading the
// owning session. This is the preferred entry point for the turn runner.
func (r *DefaultToolRouter) DispatchParsed(ctx context.Context, sess *Session, tc *TurnContext, call ParsedToolCall) (AnyToolResult, error) {
	return r.dispatch(ctx, sess, tc, call.CallID, call.ToolName, call.Payload)
}

// DispatchWithSession dispatches an invocation while threading the owning
// session so executors can emit visible events (e.g. exec_command_begin/end,
// file_change item lifecycle). It is the session-aware counterpart of the
// [ToolRouter.Dispatch] interface method, satisfying [sessionAwareRouter].
func (r *DefaultToolRouter) DispatchWithSession(ctx context.Context, sess *Session, tc *TurnContext, inv ToolInvocation, payload tools.ToolPayload) (ToolResult, error) {
	res, err := r.dispatch(ctx, sess, tc, inv.CallID, inv.Name, payload)
	if err != nil {
		return ToolResult{}, err
	}
	return res.ToToolResult(), nil
}

// sessionAwareRouter is implemented by routers that can dispatch with the owning
// session threaded through. The turn runner prefers this path so built-in
// executors (shell_command / apply_patch) can emit their lifecycle events; a
// router that does not implement it falls back to the session-less
// [ToolRouter.Dispatch].
type sessionAwareRouter interface {
	DispatchWithSession(ctx context.Context, sess *Session, tc *TurnContext, inv ToolInvocation, payload tools.ToolPayload) (ToolResult, error)
}

// rawArgumentsForPayload renders a payload's raw argument bytes for the
// foundation [ToolInvocation.Arguments] field.
func rawArgumentsForPayload(p tools.ToolPayload) []byte {
	switch p.Kind {
	case tools.ToolPayloadKindToolSearch:
		raw, err := json.Marshal(p.SearchArguments)
		if err != nil {
			return nil
		}
		return raw
	case tools.ToolPayloadKindCustom:
		return []byte(p.Input)
	default:
		return []byte(p.Arguments)
	}
}

// unsupportedToolCallMessage builds the model-facing message for an unknown tool,
// mirroring the Rust `unsupported_tool_call_message`.
func unsupportedToolCallMessage(payload tools.ToolPayload, name protocol.ToolName) string {
	if payload.Kind == tools.ToolPayloadKindCustom {
		return fmt.Sprintf("unsupported custom tool call: %s", name)
	}
	return fmt.Sprintf("unsupported call: %s", name)
}
