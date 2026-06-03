package codemode

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// fakeDelegate is a configurable CodeModeSessionDelegate for exercising the
// nested-tool bridge, notifications, and cell-close lifecycle without a real host.
type fakeDelegate struct {
	mu        sync.Mutex
	responses map[string]any    // tool name -> JSON response to resolve
	errors    map[string]string // tool name -> error text to reject
	calls     []CodeModeNestedToolCall
	notifies  []notifyRecord
	closed    []CellID
}

type notifyRecord struct {
	callID string
	cellID CellID
	text   string
}

func newFakeDelegate() *fakeDelegate {
	return &fakeDelegate{responses: map[string]any{}, errors: map[string]string{}}
}

func (d *fakeDelegate) InvokeTool(_ context.Context, call CodeModeNestedToolCall) (any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, call)
	if msg, ok := d.errors[call.ToolName.Name]; ok {
		return nil, fmt.Errorf("%s", msg)
	}
	if resp, ok := d.responses[call.ToolName.Name]; ok {
		return resp, nil
	}
	return nil, nil
}

func (d *fakeDelegate) Notify(_ context.Context, callID string, cellID CellID, text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.notifies = append(d.notifies, notifyRecord{callID: callID, cellID: cellID, text: text})
	return nil
}

func (d *fakeDelegate) CellClosed(cellID CellID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = append(d.closed, cellID)
}

func (d *fakeDelegate) recordedCalls() []CodeModeNestedToolCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]CodeModeNestedToolCall(nil), d.calls...)
}

// testYield is a short yield window so tests that exercise the yield timer do not
// block for the production default.
func u64(v uint64) *uint64 { return &v }

// execAndWait runs a request and drives wait() until the cell reaches a terminal
// Result, returning that response. It mirrors the reference `execute` helper but
// also follows yields so async cells complete deterministically.
func execAndWait(t *testing.T, service *CodeModeService, request ExecuteRequest) RuntimeResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started, err := service.Execute(ctx, request)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	response, err := started.InitialResponse(ctx)
	if err != nil {
		t.Fatalf("InitialResponse: %v", err)
	}
	for response.Kind == RuntimeResponseYielded {
		outcome, err := service.Wait(ctx, WaitRequest{CellID: started.CellID, YieldTimeMS: 50})
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		response = outcome.Response
	}
	return response
}

func newExecRequest(source string) ExecuteRequest {
	return ExecuteRequest{
		ToolCallID:   "call_1",
		EnabledTools: nil,
		Source:       source,
		YieldTimeMS:  u64(50),
	}
}

// TestExecSynchronousExit mirrors codex's synchronous_exit_returns_successfully:
// exit() ends the script after the first text() and discards later output.
func TestExecSynchronousExit(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	req := newExecRequest(`text("before"); exit(); text("after");`)
	req.YieldTimeMS = nil
	got := execAndWait(t, service, req)

	want := newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{NewTextItem("before")}, nil)
	assertResponseEqual(t, got, want)
}

// TestStoredValuesSharedBetweenCellsNotSessions mirrors codex's
// stored_values_are_shared_between_cells_but_not_sessions: this is the cell-state
// persistence-across-exec-calls contract plus session isolation.
func TestStoredValuesSharedBetweenCellsNotSessions(t *testing.T) {
	first := NewCodeModeService()
	second := NewCodeModeService()
	defer mustShutdown(t, first)
	defer mustShutdown(t, second)

	writeReq := newExecRequest(`store("key", "visible");`)
	writeReq.YieldTimeMS = nil
	write := execAndWait(t, first, writeReq)
	assertResponseEqual(t, write, newResultResponse(NewCellID("1"), nil, nil))

	readReq := newExecRequest(`text(String(load("key")));`)
	readReq.YieldTimeMS = nil
	sameSession := execAndWait(t, first, readReq)
	assertResponseEqual(t, sameSession,
		newResultResponse(NewCellID("2"), []FunctionCallOutputContentItem{NewTextItem("visible")}, nil))

	otherReq := newExecRequest(`text(String(load("key")));`)
	otherReq.YieldTimeMS = nil
	otherSession := execAndWait(t, second, otherReq)
	assertResponseEqual(t, otherSession,
		newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{NewTextItem("undefined")}, nil))
}

// TestStoredValueRoundTripsObjects verifies object state persists across exec
// calls in a session and is extended (not replaced) per codex's store semantics.
func TestStoredValueRoundTripsObjects(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	first := newExecRequest(`store("counter", { value: 1, history: [1] });`)
	first.YieldTimeMS = nil
	execAndWait(t, service, first)

	second := newExecRequest(`
const prev = load("counter");
const next = { value: prev.value + 1, history: prev.history.concat([prev.value + 1]) };
store("counter", next);
text(load("counter").history.join(","));
`)
	second.YieldTimeMS = nil
	got := execAndWait(t, service, second)
	assertResponseEqual(t, got,
		newResultResponse(NewCellID("2"), []FunctionCallOutputContentItem{NewTextItem("1,2")}, nil))
}

// TestOutputHelpersReturnUndefined mirrors codex's output_helpers_return_undefined.
func TestOutputHelpersReturnUndefined(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	req := newExecRequest(`
const returnsUndefined = [
  text("first"),
  image("https://example.com/image.jpg"),
  notify("ping"),
].map((value) => value === undefined);
text(JSON.stringify(returnsUndefined));
`)
	req.YieldTimeMS = nil
	got := execAndWait(t, service, req)

	high := ImageDetailHigh
	want := newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{
		NewTextItem("first"),
		NewImageItem("https://example.com/image.jpg", &high),
		NewTextItem("[true,true,true]"),
	}, nil)
	assertResponseEqual(t, got, want)
}

// TestImageHelperVariants mirrors several codex image_helper_* tests in a table.
func TestImageHelperVariants(t *testing.T) {
	original := ImageDetailOriginal
	high := ImageDetailHigh
	low := ImageDetailLow

	cases := []struct {
		name   string
		source string
		want   RuntimeResponse
	}{
		{
			name: "raw-mcp-original",
			source: `
image({
  type: "image",
  data: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg==",
  mimeType: "image/png",
  _meta: { "codex/imageDetail": "original" },
});
`,
			want: newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{
				NewImageItem("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg==", &original),
			}, nil),
		},
		{
			name:   "second-arg-overrides-object-detail",
			source: `image({ image_url: "https://example.com/image.jpg", detail: "high" }, "original");`,
			want: newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{
				NewImageItem("https://example.com/image.jpg", &original),
			}, nil),
		},
		{
			name:   "accepts-low-detail",
			source: `image({ image_url: "https://example.com/image.jpg", detail: "low" });`,
			want: newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{
				NewImageItem("https://example.com/image.jpg", &low),
			}, nil),
		},
		{
			name:   "rejects-unsupported-detail",
			source: `image({ image_url: "https://example.com/image.jpg", detail: "medium" });`,
			want:   newResultResponse(NewCellID("1"), nil, strPtr("image detail must be one of: auto, low, high, original")),
		},
		{
			name: "rejects-mcp-result-container",
			source: `
image({
  content: [
    { type: "image", data: "abc", mimeType: "image/png" },
  ],
  isError: false,
});
`,
			want: newResultResponse(NewCellID("1"), nil, strPtr(imageHelperExpectsMessage)),
		},
		{
			name:   "default-high-detail",
			source: `image("https://example.com/image.jpg");`,
			want: newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{
				NewImageItem("https://example.com/image.jpg", &high),
			}, nil),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := NewCodeModeService()
			defer mustShutdown(t, service)
			req := newExecRequest(tc.source)
			req.YieldTimeMS = nil
			got := execAndWait(t, service, req)
			assertResponseEqual(t, got, tc.want)
		})
	}
}

// TestConsoleNotExposed mirrors codex's v8_console_is_not_exposed_on_global_this.
func TestConsoleNotExposed(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	req := newExecRequest(`text(String(Object.hasOwn(globalThis, "console")));`)
	req.YieldTimeMS = nil
	got := execAndWait(t, service, req)
	assertResponseEqual(t, got,
		newResultResponse(NewCellID("1"), []FunctionCallOutputContentItem{NewTextItem("false")}, nil))
}

// TestWaitReportsMissingCell mirrors codex's
// wait_reports_missing_cell_separately_from_runtime_results.
func TestWaitReportsMissingCell(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	outcome, err := service.Wait(context.Background(), WaitRequest{CellID: NewCellID("missing"), YieldTimeMS: 1})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if outcome.Kind != WaitOutcomeMissingCell {
		t.Fatalf("kind = %v, want MissingCell", outcome.Kind)
	}
	want := newResultResponse(NewCellID("missing"), nil, strPtr("exec cell missing not found"))
	assertResponseEqual(t, outcome.Response, want)
}

// TestExecRuntimeError verifies a thrown error surfaces as a Result with
// error_text populated.
func TestExecRuntimeError(t *testing.T) {
	service := NewCodeModeService()
	defer mustShutdown(t, service)

	req := newExecRequest(`throw new Error("boom");`)
	req.YieldTimeMS = nil
	got := execAndWait(t, service, req)
	if got.Kind != RuntimeResponseResult || got.ErrorText == nil {
		t.Fatalf("expected Result with error_text, got %+v", got)
	}
}

// mustShutdown shuts a service down within a bounded window.
func mustShutdown(t *testing.T, service *CodeModeService) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// assertResponseEqual compares two RuntimeResponses field-by-field with a helpful
// diff, since RuntimeResponse contains a *string that reflect.DeepEqual handles
// but renders opaquely on failure.
func assertResponseEqual(t *testing.T, got, want RuntimeResponse) {
	t.Helper()
	if got.Kind != want.Kind {
		t.Fatalf("kind = %v, want %v (got=%+v)", got.Kind, want.Kind, got)
	}
	if got.CellID != want.CellID {
		t.Fatalf("cell id = %s, want %s", got.CellID, want.CellID)
	}
	if !reflect.DeepEqual(normalizeItems(got.ContentItems), normalizeItems(want.ContentItems)) {
		t.Fatalf("content items = %+v, want %+v", got.ContentItems, want.ContentItems)
	}
	if !equalStrPtr(got.ErrorText, want.ErrorText) {
		t.Fatalf("error text = %v, want %v", derefStr(got.ErrorText), derefStr(want.ErrorText))
	}
}

func normalizeItems(items []FunctionCallOutputContentItem) []FunctionCallOutputContentItem {
	if len(items) == 0 {
		return nil
	}
	return items
}

func equalStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func derefStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// plainTool is a small constructor for a function-kind nested tool definition.
func plainTool(name string) ToolDefinition {
	return ToolDefinition{
		Name:     name,
		ToolName: protocol.PlainToolName(name),
		Kind:     CodeModeToolKindFunction,
	}
}
