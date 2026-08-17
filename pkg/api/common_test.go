package api

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestResponsesApiRequestMinimalShape(t *testing.T) {
	req := ResponsesApiRequest{
		Model:      "gpt-5.1-codex",
		ToolChoice: "auto",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// instructions omitted (empty); reasoning present as null; slices are [].
	want := `{"model":"gpt-5.1-codex","input":[],"tools":[],"tool_choice":"auto","parallel_tool_calls":false,"reasoning":null,"store":false,"stream":false,"include":[]}`
	if string(data) != want {
		t.Fatalf("unexpected json:\n got: %s\nwant: %s", data, want)
	}
}

func TestResponsesApiRequestFullShapeOrder(t *testing.T) {
	effort := protocol.ReasoningEffortHigh
	summary := protocol.ReasoningSummaryAuto
	tier := "priority"
	cacheKey := "ck"
	req := ResponsesApiRequest{
		Model:             "m",
		Instructions:      "be helpful",
		Input:             []protocol.ResponseItem{},
		Tools:             []json.RawMessage{json.RawMessage(`{"type":"function"}`)},
		ToolChoice:        "auto",
		ParallelToolCalls: true,
		Reasoning:         &Reasoning{Effort: &effort, Summary: &summary},
		Store:             true,
		Stream:            true,
		Include:           []string{"reasoning.encrypted_content"},
		ServiceTier:       &tier,
		PromptCacheKey:    &cacheKey,
		ClientMetadata:    map[string]string{"k": "v"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"model":"m","instructions":"be helpful","input":[],"tools":[{"type":"function"}],"tool_choice":"auto","parallel_tool_calls":true,"reasoning":{"effort":"high","summary":"auto"},"store":true,"stream":true,"include":["reasoning.encrypted_content"],"service_tier":"priority","prompt_cache_key":"ck","client_metadata":{"k":"v"}}`
	if string(data) != want {
		t.Fatalf("unexpected json:\n got: %s\nwant: %s", data, want)
	}
}

func TestReasoningOmitsEmptyFields(t *testing.T) {
	effort := protocol.ReasoningEffortLow
	r := Reasoning{Effort: &effort}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"effort":"low"}` {
		t.Fatalf("unexpected reasoning json: %s", data)
	}
}

func TestResponseCreateWsRequestShape(t *testing.T) {
	req := ResponseCreateWsRequest{
		Model:      "m",
		Input:      []protocol.ResponseItem{},
		ToolChoice: "auto",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"model":"m","input":[],"tools":[],"tool_choice":"auto","parallel_tool_calls":false,"reasoning":null,"store":false,"stream":false,"include":[]}`
	if string(data) != want {
		t.Fatalf("unexpected json:\n got: %s\nwant: %s", data, want)
	}
}

func TestResponsesWsRequestTagged(t *testing.T) {
	req := ResponsesWsRequest{
		Kind:      ResponsesWsRequestProcessed,
		Processed: &ResponseProcessedWsRequest{ResponseID: "resp1"},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"response.processed","response_id":"resp1"}`
	if string(data) != want {
		t.Fatalf("unexpected json: %s", data)
	}
}

func TestResponsesWsRequestCreateTagFirst(t *testing.T) {
	req := ResponsesWsRequest{
		Kind: ResponsesWsRequestCreate,
		Create: &ResponseCreateWsRequest{
			Model:      "m",
			Input:      []protocol.ResponseItem{},
			ToolChoice: "auto",
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var tag string
	if err := json.Unmarshal(top["type"], &tag); err != nil || tag != "response.create" {
		t.Fatalf("unexpected tag %q", tag)
	}
	if _, ok := top["model"]; !ok {
		t.Fatalf("expected model field flattened into tagged object")
	}
}

func TestResponseCreateWsRequestFromAPI(t *testing.T) {
	tier := "t"
	api := ResponsesApiRequest{Model: "m", ServiceTier: &tier, Stream: true}
	ws := ResponseCreateWsRequestFromAPI(api)
	if ws.Model != "m" || ws.ServiceTier == nil || *ws.ServiceTier != "t" || !ws.Stream {
		t.Fatalf("unexpected conversion: %+v", ws)
	}
	if ws.PreviousResponseID != nil || ws.Generate != nil {
		t.Fatalf("expected nil previous_response_id and generate")
	}
}

func TestCreateTextParamForRequest(t *testing.T) {
	if CreateTextParamForRequest(nil, nil, false) != nil {
		t.Fatalf("expected nil when no verbosity or schema")
	}
	v := protocol.VerbosityHigh
	schema := json.RawMessage(`{"type":"object"}`)
	tc := CreateTextParamForRequest(&v, &schema, true)
	if tc == nil || tc.Verbosity == nil || *tc.Verbosity != OpenAiVerbosityHigh {
		t.Fatalf("unexpected verbosity: %+v", tc)
	}
	if tc.Format == nil || tc.Format.Name != "codex_output_schema" || !tc.Format.Strict {
		t.Fatalf("unexpected format: %+v", tc.Format)
	}
}

func TestResponseCreateClientMetadataMergesTrace(t *testing.T) {
	tp := "00-trace-span-01"
	ts := "vendor=value"
	trace := &protocol.W3cTraceContext{Traceparent: &tp, Tracestate: &ts}
	out := ResponseCreateClientMetadata(map[string]string{"k": "v"}, trace)
	if out[WSRequestHeaderTraceparentClientMetadataKey] != tp {
		t.Fatalf("missing traceparent")
	}
	if out[WSRequestHeaderTracestateClientMetadataKey] != ts {
		t.Fatalf("missing tracestate")
	}
	if out["k"] != "v" {
		t.Fatalf("missing original metadata")
	}
}

func TestResponseCreateClientMetadataEmptyReturnsNil(t *testing.T) {
	if out := ResponseCreateClientMetadata(nil, nil); out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
}
