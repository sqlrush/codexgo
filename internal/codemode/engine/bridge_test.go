package engine

import (
	"fmt"
	"testing"

	"github.com/dop251/goja"
	"github.com/sqlrush/codexgo/internal/codemode"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// fakeToolInvoker is a minimal stand-in for the host-side tool bridge. It records
// each nested tool call a cell makes and returns a canned JSON response, exercising
// the jsonToJSValue / jsValueToJSON conversion path the production runtime uses to
// marshal nested tool inputs and responses across the JS boundary.
type fakeToolInvoker struct {
	calls     []CodeModeNestedToolCall
	responses map[string]any
	errors    map[string]string
}

func newFakeToolInvoker() *fakeToolInvoker {
	return &fakeToolInvoker{responses: map[string]any{}, errors: map[string]string{}}
}

// invoke decodes the JS-supplied argument into JSON, records the call, and returns
// either a canned JSON response (to be injected back into JS) or an error string.
func (f *fakeToolInvoker) invoke(rt *goja.Runtime, toolName string, arg goja.Value) (goja.Value, error) {
	decoded, _, err := jsValueToJSON(rt, arg)
	if err != nil {
		return nil, err
	}
	f.calls = append(f.calls, CodeModeNestedToolCall{
		RuntimeToolCallID: fmt.Sprintf("call-%d", len(f.calls)),
		ToolName:          protocol.PlainToolName(toolName),
		ToolKind:          codemode.CodeModeToolKindFunction,
		Input:             decoded,
	})
	if msg, ok := f.errors[toolName]; ok {
		return nil, fmt.Errorf("%s", msg)
	}
	response, ok := f.responses[toolName]
	if !ok {
		return goja.Undefined(), nil
	}
	return jsonToJSValue(rt, response)
}

// installTools wires the fake invoker into a goja runtime as a synchronous global
// `tools` object so deterministic JS can call nested tools and use their results.
func installTools(t *testing.T, rt *goja.Runtime, invoker *fakeToolInvoker, toolNames ...string) {
	t.Helper()
	tools := rt.NewObject()
	for _, name := range toolNames {
		name := name
		fn := func(call goja.FunctionCall) goja.Value {
			var arg goja.Value = goja.Undefined()
			if len(call.Arguments) > 0 {
				arg = call.Arguments[0]
			}
			result, err := invoker.invoke(rt, name, arg)
			if err != nil {
				panic(rt.NewTypeError(err.Error()))
			}
			return result
		}
		if err := tools.Set(codemode.NormalizeCodeModeIdentifier(name), fn); err != nil {
			t.Fatalf("set tool %q: %v", name, err)
		}
	}
	if err := rt.Set("tools", tools); err != nil {
		t.Fatalf("set tools global: %v", err)
	}
}

// TestNestedToolCallBridge verifies a cell can call a nested tool, the invoker
// receives the decoded JSON input, and the tool's JSON response is observable
// back inside the JS cell. This is the exec -> nested tool -> response round trip.
func TestNestedToolCallBridge(t *testing.T) {
	rt := goja.New()
	invoker := newFakeToolInvoker()
	invoker.responses["get_profile"] = map[string]any{"name": "Ada", "id": 7}
	installTools(t, rt, invoker, "get_profile")

	// Deterministic JS: call the tool, then read a field off its response.
	value, err := rt.RunString(`
		const profile = tools.get_profile({ userId: 42 });
		profile.name + ":" + profile.id;
	`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	if value.String() != "Ada:7" {
		t.Fatalf("script result = %q, want Ada:7", value.String())
	}

	if len(invoker.calls) != 1 {
		t.Fatalf("invoker recorded %d calls, want 1", len(invoker.calls))
	}
	call := invoker.calls[0]
	if call.ToolName.Name != "get_profile" {
		t.Errorf("tool name = %q, want get_profile", call.ToolName.Name)
	}
	input, ok := call.Input.(map[string]any)
	if !ok {
		t.Fatalf("input not an object: %#v", call.Input)
	}
	if input["userId"] != float64(42) {
		t.Errorf("input userId = %v, want 42", input["userId"])
	}
}

// TestNestedToolCallErrorBridge verifies a tool error surfaces as a JS exception
// that the cell can observe (the error-handling path of the bridge).
func TestNestedToolCallErrorBridge(t *testing.T) {
	rt := goja.New()
	invoker := newFakeToolInvoker()
	invoker.errors["explode"] = "tool failed: nope"
	installTools(t, rt, invoker, "explode")

	value, err := rt.RunString(`
		let caught = "none";
		try { tools.explode({}); } catch (e) { caught = e.message; }
		caught;
	`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	if value.String() != "tool failed: nope" {
		t.Fatalf("caught error = %q, want \"tool failed: nope\"", value.String())
	}
	if len(invoker.calls) != 1 {
		t.Fatalf("invoker recorded %d calls, want 1", len(invoker.calls))
	}
}

// TestCellStatePersistenceAcrossExecCalls simulates the store()/load() contract:
// a session-scoped store survives across separate exec evaluations (fresh goja
// runtimes), with values round-tripped through the JSON bridge exactly as the
// production store does. This models cell/session state persistence.
func TestCellStatePersistenceAcrossExecCalls(t *testing.T) {
	// Session-scoped store, keyed by string, holding JSON-decoded values.
	store := map[string]any{}

	installStore := func(rt *goja.Runtime) {
		storeFn := func(call goja.FunctionCall) goja.Value {
			key := call.Argument(0).String()
			decoded, _, err := jsValueToJSON(rt, call.Argument(1))
			if err != nil {
				panic(rt.NewTypeError(err.Error()))
			}
			store[key] = decoded
			return goja.Undefined()
		}
		loadFn := func(call goja.FunctionCall) goja.Value {
			key := call.Argument(0).String()
			value, ok := store[key]
			if !ok {
				return goja.Undefined()
			}
			js, err := jsonToJSValue(rt, value)
			if err != nil {
				panic(rt.NewTypeError(err.Error()))
			}
			return js
		}
		if err := rt.Set("store", storeFn); err != nil {
			t.Fatalf("set store: %v", err)
		}
		if err := rt.Set("load", loadFn); err != nil {
			t.Fatalf("set load: %v", err)
		}
	}

	// First exec call: write state into the session store.
	rt1 := goja.New()
	installStore(rt1)
	if _, err := rt1.RunString(`store("counter", { value: 1, history: [1] });`); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	// Second exec call in a fresh runtime: load the prior state and extend it.
	rt2 := goja.New()
	installStore(rt2)
	value, err := rt2.RunString(`
		const prev = load("counter");
		const next = { value: prev.value + 1, history: prev.history.concat([prev.value + 1]) };
		store("counter", next);
		load("counter").history.join(",");
	`)
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if value.String() != "1,2" {
		t.Fatalf("persisted history = %q, want \"1,2\"", value.String())
	}

	// Loading a missing key yields undefined.
	rt3 := goja.New()
	installStore(rt3)
	missing, err := rt3.RunString(`typeof load("absent")`)
	if err != nil {
		t.Fatalf("missing load: %v", err)
	}
	if missing.String() != "undefined" {
		t.Fatalf("missing key = %q, want undefined", missing.String())
	}
}
