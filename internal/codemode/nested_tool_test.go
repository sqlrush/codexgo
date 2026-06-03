package codemode

import (
	"context"
	"testing"
	"time"
)

// TestNestedToolCallEndToEnd drives the full exec -> nested tool -> response ->
// result round trip through the real CodeModeService and a fake delegate. The
// cell awaits a tool call; the delegate's JSON response is injected back into the
// runtime; the cell observes a field off the response and emits it as text.
func TestNestedToolCallEndToEnd(t *testing.T) {
	delegate := newFakeDelegate()
	delegate.responses["get_profile"] = map[string]any{"name": "Ada", "id": float64(7)}
	service := NewCodeModeServiceWithDelegate(delegate)
	defer mustShutdown(t, service)

	req := ExecuteRequest{
		ToolCallID:   "call_1",
		EnabledTools: []ToolDefinition{plainTool("get_profile")},
		Source: `
const profile = await tools.get_profile({ userId: 42 });
text(profile.name + ":" + profile.id);
`,
	}
	got := execAndWait(t, service, req)

	assertResponseEqual(t, got,
		newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{NewTextItem("Ada:7")}, nil))

	calls := delegate.recordedCalls()
	if len(calls) != 1 {
		t.Fatalf("delegate recorded %d calls, want 1", len(calls))
	}
	call := calls[0]
	if call.ToolName.Name != "get_profile" {
		t.Errorf("tool name = %q, want get_profile", call.ToolName.Name)
	}
	if call.RuntimeToolCallID != "tool-1" {
		t.Errorf("runtime tool call id = %q, want tool-1", call.RuntimeToolCallID)
	}
	if call.CellID.AsStr() != "1" {
		t.Errorf("cell id = %q, want 1", call.CellID.AsStr())
	}
	input, ok := call.Input.(map[string]any)
	if !ok {
		t.Fatalf("input not an object: %#v", call.Input)
	}
	if input["userId"] != float64(42) {
		t.Errorf("input userId = %v, want 42", input["userId"])
	}
}

// TestNestedToolCallError verifies a delegate error rejects the cell's promise,
// which the cell can catch and surface.
func TestNestedToolCallError(t *testing.T) {
	delegate := newFakeDelegate()
	delegate.errors["explode"] = "tool failed: nope"
	service := NewCodeModeServiceWithDelegate(delegate)
	defer mustShutdown(t, service)

	req := ExecuteRequest{
		ToolCallID:   "call_1",
		EnabledTools: []ToolDefinition{plainTool("explode")},
		Source: `
let caught = "none";
try { await tools.explode({}); } catch (e) { caught = String(e); }
text(caught);
`,
	}
	got := execAndWait(t, service, req)
	assertResponseEqual(t, got,
		newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{NewTextItem("tool failed: nope")}, nil))
}

// TestNestedToolCallsChained verifies multiple sequential awaited tool calls each
// round-trip through the delegate, with later calls observing earlier results.
func TestNestedToolCallsChained(t *testing.T) {
	delegate := newFakeDelegate()
	delegate.responses["step"] = map[string]any{"ok": true}
	service := NewCodeModeServiceWithDelegate(delegate)
	defer mustShutdown(t, service)

	req := ExecuteRequest{
		ToolCallID:   "call_1",
		EnabledTools: []ToolDefinition{plainTool("step")},
		Source: `
let total = 0;
for (let i = 0; i < 3; i++) {
  const r = await tools.step({ i });
  if (r.ok) total++;
}
text(String(total));
`,
	}
	got := execAndWait(t, service, req)
	assertResponseEqual(t, got,
		newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{NewTextItem("3")}, nil))

	calls := delegate.recordedCalls()
	if len(calls) != 3 {
		t.Fatalf("delegate recorded %d calls, want 3", len(calls))
	}
	for i, call := range calls {
		input, ok := call.Input.(map[string]any)
		if !ok {
			t.Fatalf("call %d input not an object: %#v", i, call.Input)
		}
		if input["i"] != float64(i) {
			t.Errorf("call %d input i = %v, want %d", i, input["i"], i)
		}
	}
}

// TestNestedToolCallsParallel verifies Promise.all over several tool calls works:
// all calls dispatch, all responses resolve, and the cell sees the aggregate.
func TestNestedToolCallsParallel(t *testing.T) {
	delegate := newFakeDelegate()
	delegate.responses["echo"] = map[string]any{"value": "x"}
	service := NewCodeModeServiceWithDelegate(delegate)
	defer mustShutdown(t, service)

	req := ExecuteRequest{
		ToolCallID:   "call_1",
		EnabledTools: []ToolDefinition{plainTool("echo")},
		Source: `
const results = await Promise.all([
  tools.echo({ n: 1 }),
  tools.echo({ n: 2 }),
  tools.echo({ n: 3 }),
]);
text(String(results.length));
`,
	}
	got := execAndWait(t, service, req)
	assertResponseEqual(t, got,
		newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{NewTextItem("3")}, nil))

	if calls := delegate.recordedCalls(); len(calls) != 3 {
		t.Fatalf("delegate recorded %d calls, want 3", len(calls))
	}
}

// TestNoopDelegateErrorOnCancellation verifies the noop delegate's InvokeTool
// returns codex's exact "nested tools are unavailable" message once its context
// is cancelled (it awaits cancellation before erroring, mirroring codex).
func TestNoopDelegateErrorOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		v   any
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		v, err := NoopCodeModeSessionDelegate{}.InvokeTool(ctx, CodeModeNestedToolCall{})
		resultCh <- result{v: v, err: err}
	}()

	select {
	case <-resultCh:
		t.Fatal("InvokeTool returned before cancellation")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case got := <-resultCh:
		if got.err == nil || got.err.Error() != "code mode nested tools are unavailable" {
			t.Fatalf("err = %v, want \"code mode nested tools are unavailable\"", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("InvokeTool did not return after cancellation")
	}
}

// TestNoopDelegateNestedToolCellTerminates verifies a cell whose nested tool is
// served by the noop delegate (which never resolves) can be terminated, and the
// noop delegate's pending invocation is cancelled as part of teardown.
func TestNoopDelegateNestedToolCellTerminates(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := ExecuteRequest{
		ToolCallID:   "call_1",
		EnabledTools: []ToolDefinition{plainTool("anything")},
		Source:       `await tools.anything({});`,
		YieldTimeMS:  u64(20),
	}
	started, err := service.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	first, err := started.InitialResponse(ctx)
	if err != nil {
		t.Fatalf("InitialResponse: %v", err)
	}
	if first.Kind != RuntimeResponseYielded {
		t.Fatalf("first kind = %v, want Yielded (noop tool never resolves)", first.Kind)
	}

	outcome, err := service.Terminate(ctx, started.CellID)
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if outcome.Response.Kind != RuntimeResponseTerminated {
		t.Fatalf("terminate response kind = %v, want Terminated", outcome.Response.Kind)
	}
}

// TestNotifyDeliveredToDelegate verifies notify() reaches the delegate with the
// originating tool-call id and cell id, and the message also appears in output.
func TestNotifyDeliveredToDelegate(t *testing.T) {
	delegate := newFakeDelegate()
	service := NewCodeModeServiceWithDelegate(delegate)
	defer mustShutdown(t, service)

	req := newExecRequest(`notify("hello world");`)
	req.YieldTimeMS = nil
	got := execAndWait(t, service, req)
	if got.Kind != RuntimeResponseResult || got.ErrorText != nil {
		t.Fatalf("expected clean Result, got %+v", got)
	}

	// The notification task completes before the terminal Result is delivered
	// (handleResult waits on notifications), so it is observable now.
	delegate.mu.Lock()
	defer delegate.mu.Unlock()
	if len(delegate.notifies) != 1 {
		t.Fatalf("delegate recorded %d notifies, want 1", len(delegate.notifies))
	}
	n := delegate.notifies[0]
	if n.text != "hello world" {
		t.Errorf("notify text = %q, want hello world", n.text)
	}
	if n.callID != "call_1" {
		t.Errorf("notify call id = %q, want call_1", n.callID)
	}
	if n.cellID.AsStr() != "1" {
		t.Errorf("notify cell id = %q, want 1", n.cellID.AsStr())
	}
}

// TestCellClosedCalledOnTerminal verifies the delegate's CellClosed lifecycle
// callback fires once a cell reaches a terminal state.
func TestCellClosedCalledOnTerminal(t *testing.T) {
	delegate := newFakeDelegate()
	service := NewCodeModeServiceWithDelegate(delegate)

	req := newExecRequest(`text("done");`)
	req.YieldTimeMS = nil
	execAndWait(t, service, req)
	mustShutdown(t, service)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		delegate.mu.Lock()
		n := len(delegate.closed)
		delegate.mu.Unlock()
		if n >= 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("CellClosed was never called")
}

// TestTerminateRunningCell verifies a never-completing cell can be terminated and
// returns a Terminated response.
func TestTerminateRunningCell(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A cell that yields then awaits forever.
	req := ExecuteRequest{
		ToolCallID:  "call_1",
		Source:      `text("before"); await new Promise(() => {});`,
		YieldTimeMS: u64(20),
	}
	started, err := service.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	first, err := started.InitialResponse(ctx)
	if err != nil {
		t.Fatalf("InitialResponse: %v", err)
	}
	if first.Kind != RuntimeResponseYielded {
		t.Fatalf("first response kind = %v, want Yielded (%+v)", first.Kind, first)
	}

	outcome, err := service.Terminate(ctx, started.CellID)
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if outcome.Kind != WaitOutcomeLiveCell {
		t.Fatalf("terminate outcome kind = %v, want LiveCell", outcome.Kind)
	}
	if outcome.Response.Kind != RuntimeResponseTerminated {
		t.Fatalf("terminate response kind = %v, want Terminated (%+v)", outcome.Response.Kind, outcome.Response)
	}
}

// TestShutdownInterruptsCPUBoundCell mirrors codex's
// shutdown_interrupts_cpu_bound_cells: a tight infinite loop yields at the window
// boundary (via interrupt-driven yield is not possible for CPU-bound code, so the
// cell yields only after the runtime parks; here we verify Shutdown unwinds it).
func TestShutdownInterruptsCPUBoundCell(t *testing.T) {
	service := NewCodeModeService()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := ExecuteRequest{
		ToolCallID:  "call_1",
		Source:      `while (true) {}`,
		YieldTimeMS: u64(10),
	}
	started, err := service.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The CPU-bound cell never yields control on its own; Shutdown must interrupt
	// the runtime so the control loop can tear down. InitialResponse may never
	// arrive, so we do not block on it here.
	_ = started

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown did not interrupt CPU-bound cell: %v", err)
	}
}

// TestExecYieldsThenCompletes verifies a CPU/async cell that runs past the yield
// window yields first, then completes on a subsequent wait.
func TestExecYieldsThenCompletes(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Resolve after a timer longer than the initial yield window so the first
	// response is a yield, then a wait drives it to completion.
	req := ExecuteRequest{
		ToolCallID:  "call_1",
		Source:      `await new Promise((resolve) => setTimeout(resolve, 40)); text("done");`,
		YieldTimeMS: u64(5),
	}
	started, err := service.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	first, err := started.InitialResponse(ctx)
	if err != nil {
		t.Fatalf("InitialResponse: %v", err)
	}
	if first.Kind != RuntimeResponseYielded {
		t.Fatalf("first kind = %v, want Yielded", first.Kind)
	}

	var final RuntimeResponse = first
	for final.Kind == RuntimeResponseYielded {
		outcome, err := service.Wait(ctx, WaitRequest{CellID: started.CellID, YieldTimeMS: 100})
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		final = outcome.Response
	}
	assertResponseEqual(t, final,
		newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{NewTextItem("done")}, nil))
}
