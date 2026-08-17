package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// This file tests the core tool-dispatch layer ported from codex-core's
// tools/router.rs + tools/mod.rs + the built-in handlers: BuildToolCall parsing,
// the DefaultToolRouter (registration, spec resolution, dispatch routing), and
// each built-in executor against mock/real dependencies.

// ----------------------------------------------------------------------------
// Test fixtures
// ----------------------------------------------------------------------------

// newTestTurn builds a minimal TurnContext for dispatch tests.
func newTestTurn(cwd string) *TurnContext {
	return &TurnContext{
		SubID: "turn-1",
		Cwd:   cwd,
		CollaborationMode: protocol.CollaborationMode{
			Mode: protocol.ModeKind("default"),
		},
	}
}

// newTestSession is provided by approvals_test.go in this package; tools tests
// reuse it so the Session fixture stays consistent across the suite.

// drainEventKinds collects the kinds of all currently-buffered events.
func drainEventKinds(events <-chan protocol.Event) []protocol.EventMsgKind {
	var kinds []protocol.EventMsgKind
	for {
		select {
		case ev := <-events:
			kinds = append(kinds, ev.Msg.Type)
		default:
			return kinds
		}
	}
}

func hasEventKind(kinds []protocol.EventMsgKind, want protocol.EventMsgKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

// ---- mock dependencies -----------------------------------------------------

// namedExecutor is a dispatch-only stand-in for an externally supplied
// executor (the shell family / apply_patch now come from core/localexec).
type namedExecutor struct{ name string }

func (e namedExecutor) Name() protocol.ToolName { return protocol.PlainToolName(e.name) }
func (e namedExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	return functionSpecStub(e.name, e.name), true
}
func (namedExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}
func (e namedExecutor) Handle(context.Context, *ToolHandlerContext) (tools.ToolOutput, error) {
	return NewTextToolOutput(e.name, boolPtr(true)), nil
}

type mockWebSearch struct {
	result json.RawMessage
	err    error
	gotQ   string
}

func (m *mockWebSearch) Search(_ context.Context, query string) (json.RawMessage, error) {
	m.gotQ = query
	return m.result, m.err
}

type mockUserInput struct {
	resp protocol.RequestUserInputResponse
	ok   bool
}

func (m *mockUserInput) RequestUserInput(_ context.Context, _ string, _ protocol.RequestUserInputArgs) (protocol.RequestUserInputResponse, bool) {
	return m.resp, m.ok
}

type mockPermissions struct {
	resp protocol.RequestPermissionsResponse
	ok   bool
}

func (m *mockPermissions) RequestPermissions(_ context.Context, _ string, _ protocol.RequestPermissionsArgs) (protocol.RequestPermissionsResponse, bool) {
	return m.resp, m.ok
}

type mockMcpCaller struct {
	result protocol.CallToolResult
	err    error
	gotQN  string
}

func (m *mockMcpCaller) CallQualifiedTool(_ context.Context, qualifiedName string, _, _ json.RawMessage) (protocol.CallToolResult, error) {
	m.gotQN = qualifiedName
	return m.result, m.err
}

// ----------------------------------------------------------------------------
// BuildToolCall
// ----------------------------------------------------------------------------

func TestBuildToolCall(t *testing.T) {
	t.Parallel()
	ns := "mcp__codex_apps__calendar"

	tests := []struct {
		name        string
		item        protocol.ResponseItem
		wantNil     bool
		wantErrKind *tools.FunctionCallErrorKind
		wantName    protocol.ToolName
		wantCallID  string
		wantKind    tools.ToolPayloadKind
		wantArgs    string // for Function/Custom payloads
	}{
		{
			name: "function call preserves namespace",
			item: protocol.ResponseItem{
				Type:      protocol.ResponseItemKindFunctionCall,
				Name:      "create_event",
				Namespace: &ns,
				Arguments: "{}",
				CallID:    "call-namespace",
			},
			wantName:   protocol.NamespacedToolName(ns, "create_event"),
			wantCallID: "call-namespace",
			wantKind:   tools.ToolPayloadKindFunction,
			wantArgs:   "{}",
		},
		{
			name: "plain function call",
			item: protocol.ResponseItem{
				Type:      protocol.ResponseItemKindFunctionCall,
				Name:      "exec_command",
				Arguments: `{"command":["ls"]}`,
				CallID:    "call-fn",
			},
			wantName:   protocol.PlainToolName("exec_command"),
			wantCallID: "call-fn",
			wantKind:   tools.ToolPayloadKindFunction,
			wantArgs:   `{"command":["ls"]}`,
		},
		{
			name: "custom tool call",
			item: protocol.ResponseItem{
				Type:   protocol.ResponseItemKindCustomToolCall,
				Name:   "apply_patch",
				Input:  "*** Begin Patch",
				CallID: "call-custom",
			},
			wantName:   protocol.PlainToolName("apply_patch"),
			wantCallID: "call-custom",
			wantKind:   tools.ToolPayloadKindCustom,
			wantArgs:   "*** Begin Patch",
		},
		{
			name: "client tool_search call",
			item: protocol.ResponseItem{
				Type:           protocol.ResponseItemKindToolSearchCall,
				Execution:      "client",
				CallIDOpt:      strPtr("call-search"),
				ArgumentsValue: json.RawMessage(`{"query":"foo"}`),
			},
			wantName:   protocol.PlainToolName("tool_search"),
			wantCallID: "call-search",
			wantKind:   tools.ToolPayloadKindToolSearch,
		},
		{
			name: "client tool_search with null arguments",
			item: protocol.ResponseItem{
				Type:           protocol.ResponseItemKindToolSearchCall,
				Execution:      "client",
				CallIDOpt:      strPtr("call-search-null"),
				ArgumentsValue: json.RawMessage(`null`),
			},
			wantName:   protocol.PlainToolName("tool_search"),
			wantCallID: "call-search-null",
			wantKind:   tools.ToolPayloadKindToolSearch,
		},
		{
			name: "server tool_search call is skipped",
			item: protocol.ResponseItem{
				Type:      protocol.ResponseItemKindToolSearchCall,
				Execution: "server",
				CallIDOpt: strPtr("call-server"),
			},
			wantNil: true,
		},
		{
			name: "client tool_search without call id is skipped",
			item: protocol.ResponseItem{
				Type:      protocol.ResponseItemKindToolSearchCall,
				Execution: "client",
			},
			wantNil: true,
		},
		{
			name: "malformed tool_search arguments errors to model",
			item: protocol.ResponseItem{
				Type:           protocol.ResponseItemKindToolSearchCall,
				Execution:      "client",
				CallIDOpt:      strPtr("call-bad"),
				ArgumentsValue: json.RawMessage(`{"query":`),
			},
			wantErrKind: kindPtr(tools.FunctionCallErrorRespondToModel),
		},
		{
			name:    "non-tool item is skipped",
			item:    protocol.ResponseItem{Type: protocol.ResponseItemKindMessage, Role: "assistant"},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := BuildToolCall(tt.item)

			if tt.wantErrKind != nil {
				var fce *tools.FunctionCallError
				if !errors.As(err, &fce) {
					t.Fatalf("want FunctionCallError, got %v", err)
				}
				if fce.Kind != *tt.wantErrKind {
					t.Fatalf("error kind = %v, want %v", fce.Kind, *tt.wantErrKind)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("want nil parsed call, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want parsed call, got nil")
			}
			// ToolName carries a *string Namespace, so struct equality would
			// compare pointer identity (two distinct *string with equal contents
			// are not ==). Compare by canonical String(), the value equality the
			// Rust PartialEq impl provides.
			if got.ToolName.String() != tt.wantName.String() {
				t.Errorf("ToolName = %q, want %q", got.ToolName.String(), tt.wantName.String())
			}
			if got.CallID != tt.wantCallID {
				t.Errorf("CallID = %q, want %q", got.CallID, tt.wantCallID)
			}
			if got.Payload.Kind != tt.wantKind {
				t.Errorf("Payload.Kind = %v, want %v", got.Payload.Kind, tt.wantKind)
			}
			switch tt.wantKind {
			case tools.ToolPayloadKindFunction:
				if got.Payload.Arguments != tt.wantArgs {
					t.Errorf("Arguments = %q, want %q", got.Payload.Arguments, tt.wantArgs)
				}
			case tools.ToolPayloadKindCustom:
				if got.Payload.Input != tt.wantArgs {
					t.Errorf("Input = %q, want %q", got.Payload.Input, tt.wantArgs)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// DefaultToolRouter construction + specs
// ----------------------------------------------------------------------------

func TestNewDefaultToolRouterRejectsDuplicates(t *testing.T) {
	t.Parallel()
	_, err := NewDefaultToolRouter(viewImageExecutor{}, viewImageExecutor{})
	if err == nil {
		t.Fatal("want duplicate-registration error, got nil")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error = %v, want 'already registered'", err)
	}
}

func TestNewDefaultToolRouterSkipsNil(t *testing.T) {
	t.Parallel()
	r, err := NewDefaultToolRouter(viewImageExecutor{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := r.registeredToolNames(); len(got) != 1 || got[0] != "view_image" {
		t.Errorf("registeredToolNames = %v, want [view_image]", got)
	}
}

func TestBuiltinToolRouterRegistration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		deps      BuiltinToolDeps
		wantTools []string
	}{
		{
			name: "no deps registers only the dependency-free tools",
			deps: BuiltinToolDeps{},
			// view_image, update_plan always; the five deferred collab runtimes
			// always (dispatch-only; advertised only via tool_search, gated
			// per-turn); tool_search always (advertisement is gated per-turn in
			// Spec); web_search always (hosted spec, provider-executed). The shell
			// family and apply_patch are supplied by core/localexec, so a host that
			// does not wire them (airush) registers none of them.
			// get_context_remaining / new_context register always and are
			// advertised only under a token budget (0.147).
			wantTools: []string{
				"get_context_remaining",
				"multi_agent_v1close_agent", "multi_agent_v1resume_agent",
				"multi_agent_v1send_input", "multi_agent_v1spawn_agent",
				"multi_agent_v1wait_agent", "new_context",
				"tool_search", "update_plan", "view_image", "web_search",
			},
		},
		{
			name: "shell tools register first, apply_patch after update_plan",
			deps: BuiltinToolDeps{
				ShellTools: []ToolExecutor{namedExecutor{"exec_command"}, namedExecutor{"write_stdin"}, namedExecutor{"shell_command"}},
				ApplyPatch: namedExecutor{"apply_patch"},
			},
			wantTools: []string{
				"apply_patch", "exec_command", "get_context_remaining",
				"multi_agent_v1close_agent", "multi_agent_v1resume_agent",
				"multi_agent_v1send_input", "multi_agent_v1spawn_agent",
				"multi_agent_v1wait_agent", "new_context",
				"shell_command", "tool_search", "update_plan", "view_image", "web_search", "write_stdin",
			},
		},
		{
			name: "all deps register everything",
			deps: BuiltinToolDeps{
				ShellTools:  []ToolExecutor{namedExecutor{"exec_command"}, namedExecutor{"write_stdin"}, namedExecutor{"shell_command"}},
				ApplyPatch:  namedExecutor{"apply_patch"},
				WebSearch:   &mockWebSearch{},
				UserInput:   &mockUserInput{},
				Permissions: &mockPermissions{},
				Mcp:         &mockMcpCaller{},
				McpTools: []tools.McpToolInfo{{
					ServerName:        "srv",
					CallableName:      "srv__tool",
					CallableNamespace: "mcp__srv__",
					Tool:              protocol.Tool{Name: "tool", InputSchema: json.RawMessage(`{"type":"object"}`)},
				}},
			},
			wantTools: []string{
				"apply_patch", "exec_command", "get_context_remaining",
				"multi_agent_v1close_agent", "multi_agent_v1resume_agent",
				"multi_agent_v1send_input", "multi_agent_v1spawn_agent",
				"multi_agent_v1wait_agent", "new_context",
				"request_permissions",
				"request_user_input", "shell_command", "srv__tool", "tool_search",
				"update_plan", "view_image", "web_search", "write_stdin",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := BuiltinToolRouter(tt.deps)
			if err != nil {
				t.Fatalf("BuiltinToolRouter: %v", err)
			}
			got := r.registeredToolNames()
			if strings.Join(got, ",") != strings.Join(tt.wantTools, ",") {
				t.Errorf("registered = %v, want %v", got, tt.wantTools)
			}
		})
	}
}

func TestSpecsForTurnNilContext(t *testing.T) {
	t.Parallel()
	r, _ := NewDefaultToolRouter(viewImageExecutor{})
	if _, err := r.SpecsForTurn(context.Background(), nil); err == nil {
		t.Fatal("want error for nil turn context")
	}
}

// ----------------------------------------------------------------------------
// Dispatch routing + accounting
// ----------------------------------------------------------------------------

func TestDispatchUnknownTool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload tools.ToolPayload
		wantMsg string
	}{
		{
			name:    "function payload",
			payload: tools.FunctionPayload("{}"),
			wantMsg: "unsupported call",
		},
		{
			name:    "custom payload",
			payload: tools.CustomPayload("x"),
			wantMsg: "unsupported custom tool call",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, _ := NewDefaultToolRouter(viewImageExecutor{})
			_, err := r.dispatch(context.Background(), nil, newTestTurn("/tmp"), "c1", protocol.PlainToolName("nope"), tt.payload)
			var fce *tools.FunctionCallError
			if !errors.As(err, &fce) {
				t.Fatalf("want FunctionCallError, got %v", err)
			}
			if fce.Kind != tools.FunctionCallErrorRespondToModel {
				t.Errorf("kind = %v, want RespondToModel", fce.Kind)
			}
			if !strings.Contains(fce.Message, tt.wantMsg) {
				t.Errorf("message = %q, want substring %q", fce.Message, tt.wantMsg)
			}
		})
	}
}

func TestDispatchPayloadMismatchIsFatal(t *testing.T) {
	t.Parallel()
	r, _ := NewDefaultToolRouter(requestUserInputExecutor{req: &mockUserInput{}})
	// request_user_input only accepts Function payloads; hand it a Custom one.
	_, err := r.dispatch(context.Background(), nil, newTestTurn("/tmp"), "c1",
		protocol.PlainToolName("request_user_input"), tools.CustomPayload("x"))
	var fce *tools.FunctionCallError
	if !errors.As(err, &fce) {
		t.Fatalf("want FunctionCallError, got %v", err)
	}
	if fce.Kind != tools.FunctionCallErrorFatal {
		t.Errorf("kind = %v, want Fatal", fce.Kind)
	}
}

func TestDispatchNilTurnIsFatal(t *testing.T) {
	t.Parallel()
	r, _ := NewDefaultToolRouter(viewImageExecutor{})
	_, err := r.DispatchAny(context.Background(), nil, ToolInvocation{
		CallID: "c1", Name: protocol.PlainToolName("view_image"), Arguments: []byte("{}"),
	})
	var fce *tools.FunctionCallError
	if !errors.As(err, &fce) {
		t.Fatalf("want FunctionCallError, got %v", err)
	}
	if fce.Kind != tools.FunctionCallErrorFatal {
		t.Errorf("kind = %v, want Fatal", fce.Kind)
	}
}

func TestDispatchAccountsToolCallOnActiveTurn(t *testing.T) {
	t.Parallel()
	sess, events := newTestSession(t)
	r, _ := NewDefaultToolRouter(planExecutor{})
	turn := newTestTurn("/tmp")

	call := ParsedToolCall{
		ToolName: protocol.PlainToolName("update_plan"),
		CallID:   "c1",
		Payload:  tools.FunctionPayload(`{"plan":[]}`),
	}
	if _, err := r.DispatchParsed(context.Background(), sess, turn, call); err != nil {
		t.Fatalf("DispatchParsed: %v", err)
	}
	if got := sess.ActiveTurn().State.ToolCalls(); got != 1 {
		t.Errorf("ToolCalls = %d, want 1", got)
	}
	if kinds := drainEventKinds(events); !hasEventKind(kinds, protocol.EventMsgKindPlanUpdate) {
		t.Errorf("missing plan_update event, got %v", kinds)
	}
}

// ----------------------------------------------------------------------------
// Built-in executors
// ----------------------------------------------------------------------------

func TestViewImageExecutor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    string
		wantErr bool
	}{
		{name: "valid path", args: `{"path":"/img.png"}`},
		{name: "empty path errors", args: `{"path":""}`, wantErr: true},
		{name: "malformed json errors", args: `{`, wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess, events := newTestSession(t)
			ex := viewImageExecutor{}
			out, err := ex.Handle(context.Background(), &ToolHandlerContext{
				Session: sess, Turn: newTestTurn("/tmp"), CallID: "c1",
				ToolName: ex.Name(), Payload: tools.FunctionPayload(tt.args),
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out == nil {
				t.Fatal("want output, got nil")
			}
			if kinds := drainEventKinds(events); !hasEventKind(kinds, protocol.EventMsgKindViewImageToolCall) {
				t.Errorf("missing view_image_tool_call event, got %v", kinds)
			}
		})
	}
}

func TestPlanExecutorPlanModeGuard(t *testing.T) {
	t.Parallel()
	sess, _ := newTestSession(t)
	ex := planExecutor{}
	turn := newTestTurn("/tmp")
	turn.CollaborationMode = protocol.CollaborationMode{Mode: protocol.ModeKindPlan}

	_, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: turn, CallID: "c1",
		ToolName: ex.Name(), Payload: tools.FunctionPayload(`{"plan":[]}`),
	})
	var fce *tools.FunctionCallError
	if !errors.As(err, &fce) {
		t.Fatalf("want FunctionCallError, got %v", err)
	}
	if fce.Kind != tools.FunctionCallErrorRespondToModel {
		t.Errorf("kind = %v, want RespondToModel", fce.Kind)
	}
	if !strings.Contains(fce.Message, "Plan mode") {
		t.Errorf("message = %q, want Plan-mode rejection", fce.Message)
	}
}

func TestPlanExecutorSuccess(t *testing.T) {
	t.Parallel()
	sess, events := newTestSession(t)
	ex := planExecutor{}
	out, err := ex.Handle(context.Background(), &ToolHandlerContext{
		Session: sess, Turn: newTestTurn("/tmp"), CallID: "c1",
		ToolName: ex.Name(), Payload: tools.FunctionPayload(`{"plan":[]}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := toolOutputText(out, "c1", tools.FunctionPayload(`{"plan":[]}`)); got != planUpdatedMessage {
		t.Errorf("output text = %q, want %q", got, planUpdatedMessage)
	}
	if kinds := drainEventKinds(events); !hasEventKind(kinds, protocol.EventMsgKindPlanUpdate) {
		t.Errorf("missing plan_update event, got %v", kinds)
	}
}

func TestRequestUserInputExecutor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		req     *mockUserInput
		args    string
		wantErr bool
	}{
		{
			name: "answered",
			req: &mockUserInput{ok: true, resp: protocol.RequestUserInputResponse{
				Answers: map[string]protocol.RequestUserInputAnswer{"q1": {Answers: []string{"yes"}}},
			}},
			args: `{"questions":[]}`,
		},
		{
			name:    "cancelled errors to model",
			req:     &mockUserInput{ok: false},
			args:    `{"questions":[]}`,
			wantErr: true,
		},
		{
			name:    "malformed args error",
			req:     &mockUserInput{ok: true},
			args:    `{`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess, _ := newTestSession(t)
			ex := requestUserInputExecutor{req: tt.req}
			out, err := ex.Handle(context.Background(), &ToolHandlerContext{
				Session: sess, Turn: newTestTurn("/tmp"), CallID: "c1",
				ToolName: ex.Name(), Payload: tools.FunctionPayload(tt.args),
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			text := toolOutputText(out, "c1", tools.FunctionPayload(tt.args))
			if !strings.Contains(text, "yes") {
				t.Errorf("output = %q, want serialized answer", text)
			}
		})
	}
}

func TestRequestPermissionsExecutor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		req     *mockPermissions
		wantErr bool
	}{
		{name: "granted", req: &mockPermissions{ok: true}},
		{name: "cancelled errors", req: &mockPermissions{ok: false}, wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess, _ := newTestSession(t)
			ex := requestPermissionsExecutor{req: tt.req}
			_, err := ex.Handle(context.Background(), &ToolHandlerContext{
				Session: sess, Turn: newTestTurn("/tmp"), CallID: "c1",
				ToolName: ex.Name(), Payload: tools.FunctionPayload(`{"permissions":{}}`),
			})
			if tt.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWebSearchExecutor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		runner  *mockWebSearch
		args    string
		wantErr bool
		wantQ   string
	}{
		{
			name:   "runs search",
			runner: &mockWebSearch{result: json.RawMessage(`{"results":[]}`)},
			args:   `{"query":"golang"}`,
			wantQ:  "golang",
		},
		{
			name:    "empty query errors",
			runner:  &mockWebSearch{},
			args:    `{"query":"  "}`,
			wantErr: true,
		},
		{
			name:    "runner error surfaces",
			runner:  &mockWebSearch{err: errors.New("network")},
			args:    `{"query":"x"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess, events := newTestSession(t)
			ex := webSearchExecutor{runner: tt.runner}
			_, err := ex.Handle(context.Background(), &ToolHandlerContext{
				Session: sess, Turn: newTestTurn("/tmp"), CallID: "c1",
				ToolName: ex.Name(), Payload: tools.FunctionPayload(tt.args),
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.runner.gotQ != tt.wantQ {
				t.Errorf("query = %q, want %q", tt.runner.gotQ, tt.wantQ)
			}
			kinds := drainEventKinds(events)
			if !hasEventKind(kinds, protocol.EventMsgKindWebSearchBegin) {
				t.Errorf("missing web_search_begin, got %v", kinds)
			}
			if !hasEventKind(kinds, protocol.EventMsgKindWebSearchEnd) {
				t.Errorf("missing web_search_end, got %v", kinds)
			}
		})
	}
}

func TestMcpExecutor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		caller  *mockMcpCaller
		wantErr bool
	}{
		{
			name:   "success folds mcp result",
			caller: &mockMcpCaller{result: protocol.CallToolResult{Content: []json.RawMessage{json.RawMessage(`{"type":"text","text":"ok"}`)}}},
		},
		{
			name:    "caller error surfaces to model",
			caller:  &mockMcpCaller{err: errors.New("server down")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess, events := newTestSession(t)
			ex := mcpExecutor{
				caller: tt.caller,
				spec:   functionSpecStub("srv__tool", "an mcp tool"),
				name:   protocol.PlainToolName("srv__tool"),
			}
			out, err := ex.Handle(context.Background(), &ToolHandlerContext{
				Session: sess, Turn: newTestTurn("/tmp"), CallID: "c1",
				ToolName: ex.Name(), Payload: tools.FunctionPayload(`{"q":1}`),
			})
			kinds := drainEventKinds(events)
			if !hasEventKind(kinds, protocol.EventMsgKindMcpToolCallBegin) {
				t.Errorf("missing mcp_tool_call_begin, got %v", kinds)
			}
			if !hasEventKind(kinds, protocol.EventMsgKindMcpToolCallEnd) {
				t.Errorf("missing mcp_tool_call_end, got %v", kinds)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out == nil {
				t.Fatal("want output, got nil")
			}
			if tt.caller.gotQN != "srv__tool" {
				t.Errorf("qualified name = %q, want srv__tool", tt.caller.gotQN)
			}
		})
	}
}

func TestSplitQualifiedToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in         string
		wantServer string
		wantTool   string
	}{
		{in: "srv__tool", wantServer: "srv", wantTool: "tool"},
		{in: "a__b__c", wantServer: "a", wantTool: "b__c"},
		{in: "plain", wantServer: "", wantTool: "plain"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			s, tool := splitQualifiedToolName(tt.in)
			if s != tt.wantServer || tool != tt.wantTool {
				t.Errorf("split(%q) = (%q,%q), want (%q,%q)", tt.in, s, tool, tt.wantServer, tt.wantTool)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// AnyToolResult folding
// ----------------------------------------------------------------------------

func TestAnyToolResultIntoResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		payload  tools.ToolPayload
		output   tools.ToolOutput
		wantKind tools.ResponseInputItemKind
		wantText string
	}{
		{
			name:     "function text output",
			payload:  tools.FunctionPayload("{}"),
			output:   NewTextToolOutput("done", boolPtr(true)),
			wantKind: tools.ResponseInputItemKindFunctionCallOutput,
			wantText: "done",
		},
		{
			name:     "custom text output",
			payload:  tools.CustomPayload("x"),
			output:   NewTextToolOutput("custom-done", boolPtr(true)),
			wantKind: tools.ResponseInputItemKindCustomToolCallOutput,
			wantText: "custom-done",
		},
		{
			name:     "mcp output",
			payload:  tools.FunctionPayload("{}"),
			output:   mcpToolOutput{result: protocol.CallToolResult{}},
			wantKind: tools.ResponseInputItemKindMcpToolCallOutput,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := AnyToolResult{CallID: "c1", Payload: tt.payload, Output: tt.output}
			item := res.IntoResponse()
			if item.Kind != tt.wantKind {
				t.Errorf("kind = %v, want %v", item.Kind, tt.wantKind)
			}
			if tt.wantText != "" {
				if got := res.ToToolResult(); got.Output != tt.wantText {
					t.Errorf("ToToolResult.Output = %q, want %q", got.Output, tt.wantText)
				}
			}
		})
	}
}

func TestAnyToolResultToToolResultSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		output      tools.ToolOutput
		wantSuccess bool
	}{
		{name: "explicit success", output: NewTextToolOutput("ok", boolPtr(true)), wantSuccess: true},
		{name: "explicit failure", output: NewTextToolOutput("bad", boolPtr(false)), wantSuccess: false},
		{name: "nil defaults success", output: NewTextToolOutput("ok", nil), wantSuccess: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := AnyToolResult{CallID: "c1", Payload: tools.FunctionPayload("{}"), Output: tt.output}
			if got := res.ToToolResult().Success; got != tt.wantSuccess {
				t.Errorf("Success = %v, want %v", got, tt.wantSuccess)
			}
		})
	}
}
func strPtr(s string) *string { return &s }

func kindPtr(k tools.FunctionCallErrorKind) *tools.FunctionCallErrorKind { return &k }

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// specNames returns the model-visible spec names for a turn, in advertised
// order (shared by the router/tool_search/MCP tests).
func specNames(t *testing.T, router *DefaultToolRouter, tc *TurnContext) []string {
	t.Helper()
	specs, err := router.SpecsForTurn(context.Background(), tc)
	if err != nil {
		t.Fatalf("SpecsForTurn: %v", err)
	}
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name())
	}
	return names
}
