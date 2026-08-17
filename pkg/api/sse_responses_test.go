package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/client"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

const cyberRestrictedModelForTests = "gpt-5.3-codex"

// streamFromChunks builds a client.ByteStream that emits the given byte slices
// then closes.
func streamFromChunks(chunks ...[]byte) client.ByteStream {
	ch := make(chan client.ByteChunk, len(chunks))
	for _, c := range chunks {
		ch <- client.ByteChunk{Data: c}
	}
	close(ch)
	return ch
}

// collectEvents runs processSSE over the given SSE chunks and collects results.
func collectEvents(t *testing.T, chunks ...[]byte) []ResponseResult {
	t.Helper()
	stream := streamFromChunks(chunks...)
	out := make(chan ResponseResult, 64)
	go func() {
		defer close(out)
		send := func(res ResponseResult) bool {
			out <- res
			return true
		}
		processSSE(context.Background(), stream, send, time.Second, nil)
	}()
	var results []ResponseResult
	for r := range out {
		results = append(results, r)
	}
	return results
}

func sseFrame(kind, data string) []byte {
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", kind, data))
}

func TestParsesItemsAndCompleted(t *testing.T) {
	item1 := `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}],"phase":"commentary"}}`
	item2 := `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"World"}]}}`
	completed := `{"type":"response.completed","response":{"id":"resp1"}}`

	results := collectEvents(t,
		sseFrame("response.output_item.done", item1),
		sseFrame("response.output_item.done", item2),
		sseFrame("response.completed", completed),
	)

	if len(results) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(results), results)
	}
	if results[0].Event == nil || results[0].Event.Kind != ResponseEventOutputItemDone {
		t.Fatalf("event 0 not output_item.done: %+v", results[0])
	}
	if results[0].Event.Item.Phase == nil || *results[0].Event.Item.Phase != protocol.MessagePhaseCommentary {
		t.Fatalf("event 0 phase mismatch: %+v", results[0].Event.Item)
	}
	if results[2].Event == nil || results[2].Event.Kind != ResponseEventCompleted {
		t.Fatalf("event 2 not completed: %+v", results[2])
	}
	if results[2].Event.ResponseID != "resp1" || results[2].Event.TokenUsage != nil || results[2].Event.EndTurn != nil {
		t.Fatalf("unexpected completed fields: %+v", results[2].Event)
	}
}

func TestErrorWhenMissingCompleted(t *testing.T) {
	item1 := `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}}`
	results := collectEvents(t, sseFrame("response.output_item.done", item1))
	if len(results) != 2 {
		t.Fatalf("expected 2 events, got %d", len(results))
	}
	if results[0].Event == nil || results[0].Event.Kind != ResponseEventOutputItemDone {
		t.Fatalf("event 0 mismatch: %+v", results[0])
	}
	if results[1].Err == nil || results[1].Err.Message != streamClosedBeforeCompletedMessage {
		t.Fatalf("expected stream-closed error, got %+v", results[1])
	}
}

func TestParsesToolCallInputDeltas(t *testing.T) {
	d1 := `{"type":"response.custom_tool_call_input.delta","item_id":"ctc_1","call_id":"call_1","delta":"*** Begin"}`
	completed := `{"type":"response.completed","response":{"id":"resp1"}}`
	results := collectEvents(t, sseFrame("response.custom_tool_call_input.delta", d1), sseFrame("response.completed", completed))
	if results[0].Event == nil || results[0].Event.Kind != ResponseEventToolCallInputDelta {
		t.Fatalf("event 0 mismatch: %+v", results[0])
	}
	ev := results[0].Event
	if ev.ItemID != "ctc_1" || ev.CallID == nil || *ev.CallID != "call_1" || ev.Delta != "*** Begin" {
		t.Fatalf("unexpected delta event: %+v", ev)
	}
}

func TestErrorWhenErrorEventRetryable(t *testing.T) {
	rawError := `{"type":"response.failed","response":{"id":"r","error":{"code":"rate_limit_exceeded","message":"Rate limit reached. Please try again in 11.054s. Visit docs."}}}`
	results := collectEvents(t, sseFrame("response.failed", rawError))
	if len(results) != 1 {
		t.Fatalf("expected 1 event, got %d", len(results))
	}
	if results[0].Err == nil || results[0].Err.Kind != APIErrorRetryable {
		t.Fatalf("expected retryable error, got %+v", results[0])
	}
	if results[0].Err.Delay == nil || *results[0].Err.Delay != time.Duration(11.054*float64(time.Second)) {
		t.Fatalf("unexpected delay: %v", results[0].Err.Delay)
	}
}

func TestContextWindowErrorIsFatal(t *testing.T) {
	rawError := `{"type":"response.failed","response":{"id":"r","error":{"code":"context_length_exceeded","message":"too big"}}}`
	results := collectEvents(t, sseFrame("response.failed", rawError))
	if len(results) != 1 || results[0].Err == nil || results[0].Err.Kind != APIErrorContextWindowExceeded {
		t.Fatalf("expected context window error, got %+v", results)
	}
}

func TestQuotaExceededErrorIsFatal(t *testing.T) {
	rawError := `{"type":"response.failed","response":{"id":"r","error":{"code":"insufficient_quota","message":"no quota"}}}`
	results := collectEvents(t, sseFrame("response.failed", rawError))
	if len(results) != 1 || results[0].Err == nil || results[0].Err.Kind != APIErrorQuotaExceeded {
		t.Fatalf("expected quota error, got %+v", results)
	}
}

func TestCyberPolicyErrorUsesFallbackForEmptyMessage(t *testing.T) {
	rawError := `{"type":"response.failed","response":{"id":"r","error":{"code":"cyber_policy","message":"   "}}}`
	results := collectEvents(t, sseFrame("response.failed", rawError))
	if results[0].Err == nil || results[0].Err.Kind != APIErrorCyberPolicy {
		t.Fatalf("expected cyber policy error, got %+v", results[0])
	}
	if results[0].Err.Message != cyberPolicyFallbackMessageForStream {
		t.Fatalf("unexpected message: %q", results[0].Err.Message)
	}
}

func TestInvalidPromptIsInvalidRequest(t *testing.T) {
	rawError := `{"type":"response.failed","response":{"id":"r","error":{"code":"invalid_prompt","message":"Invalid prompt: limited."}}}`
	results := collectEvents(t, sseFrame("response.failed", rawError))
	if results[0].Err == nil || results[0].Err.Kind != APIErrorInvalidRequest {
		t.Fatalf("expected invalid request, got %+v", results[0])
	}
	if results[0].Err.Message != "Invalid prompt: limited." {
		t.Fatalf("unexpected message: %q", results[0].Err.Message)
	}
}

func TestServerOverloadedIsFatal(t *testing.T) {
	rawError := `{"type":"response.failed","response":{"id":"r","error":{"code":"slow_down","message":"slow"}}}`
	results := collectEvents(t, sseFrame("response.failed", rawError))
	if results[0].Err == nil || results[0].Err.Kind != APIErrorServerOverloaded {
		t.Fatalf("expected server overloaded, got %+v", results[0])
	}
}

func TestProcessSSEEmitsServerModelFromResponseHeaders(t *testing.T) {
	created := fmt.Sprintf(`{"type":"response.created","response":{"id":"resp-1","headers":{"OpenAI-Model":%q}}}`, cyberRestrictedModelForTests)
	completed := `{"type":"response.completed","response":{"id":"resp-1"}}`
	results := collectEvents(t, sseFrame("response.created", created), sseFrame("response.completed", completed))
	if len(results) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(results), results)
	}
	if results[0].Event == nil || results[0].Event.Kind != ResponseEventServerModel || results[0].Event.Model != cyberRestrictedModelForTests {
		t.Fatalf("event 0 server model mismatch: %+v", results[0])
	}
	if results[1].Event == nil || results[1].Event.Kind != ResponseEventCreated {
		t.Fatalf("event 1 created mismatch: %+v", results[1])
	}
}

func TestProcessSSEIgnoresResponseModelFieldInPayload(t *testing.T) {
	created := fmt.Sprintf(`{"type":"response.created","response":{"id":"resp-1","model":%q}}`, cyberRestrictedModelForTests)
	completed := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp-1","model":%q}}`, cyberRestrictedModelForTests)
	results := collectEvents(t, sseFrame("response.created", created), sseFrame("response.completed", completed))
	if len(results) != 2 {
		t.Fatalf("expected 2 events (no server model), got %d: %+v", len(results), results)
	}
	if results[0].Event.Kind != ResponseEventCreated || results[1].Event.Kind != ResponseEventCompleted {
		t.Fatalf("unexpected events: %+v", results)
	}
}

func TestProcessSSEEmitsModelVerification(t *testing.T) {
	meta := fmt.Sprintf(`{"type":"response.metadata","metadata":{"openai_verification_recommendation":[%q]}}`, trustedAccessForCyberVerification)
	completed := `{"type":"response.completed","response":{"id":"resp-1"}}`
	results := collectEvents(t, sseFrame("response.metadata", meta), sseFrame("response.completed", completed))
	if results[0].Event == nil || results[0].Event.Kind != ResponseEventModelVerifications {
		t.Fatalf("expected model verifications, got %+v", results[0])
	}
	if len(results[0].Event.Verifications) != 1 || results[0].Event.Verifications[0] != protocol.ModelVerificationTrustedAccessForCyber {
		t.Fatalf("unexpected verifications: %+v", results[0].Event.Verifications)
	}
}

func TestTableDrivenEventKinds(t *testing.T) {
	completed := `{"type":"response.completed","response":{"id":"c","usage":{"input_tokens":0,"input_tokens_details":null,"output_tokens":0,"output_tokens_details":null,"total_tokens":0},"output":[]}}`
	tests := []struct {
		name        string
		event       string
		expectFirst ResponseEventKind
		expectedLen int
	}{
		{"created", `{"type":"response.created","response":{}}`, ResponseEventCreated, 2},
		{"output_item.done", `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`, ResponseEventOutputItemDone, 2},
		{"unknown", `{"type":"response.new_tool_event"}`, ResponseEventCompleted, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := collectEvents(t, sseFrame("e", tt.event), sseFrame("response.completed", completed))
			if len(results) != tt.expectedLen {
				t.Fatalf("case %s: expected %d events, got %d: %+v", tt.name, tt.expectedLen, len(results), results)
			}
			if results[0].Event == nil || results[0].Event.Kind != tt.expectFirst {
				t.Fatalf("case %s: first event mismatch: %+v", tt.name, results[0])
			}
		})
	}
}

func TestResponsesStreamEventResponseModelPrefersResponseHeaders(t *testing.T) {
	ev := ResponsesStreamEvent{
		Kind:     "response.created",
		Headers:  []byte(`{"openai-model":"top-level-model"}`),
		Response: []byte(fmt.Sprintf(`{"id":"resp-1","headers":{"openai-model":%q}}`, cyberRestrictedModelForTests)),
	}
	if got := ev.ResponseModel(); got != cyberRestrictedModelForTests {
		t.Fatalf("expected response headers model, got %q", got)
	}
}

func TestSpawnResponseStreamEmitsHeaderEvents(t *testing.T) {
	headers := http.Header{}
	headers.Set(requestIDHeader, "req-1")
	headers.Set(openAIModelHeader, cyberRestrictedModelForTests)
	empty := make(chan client.ByteChunk)
	close(empty)
	streamResponse := client.StreamResponse{Status: 200, Headers: headers, Bytes: empty}

	stream := SpawnResponseStream(context.Background(), streamResponse, time.Second, nil, nil)
	if stream.UpstreamRequestID == nil || *stream.UpstreamRequestID != "req-1" {
		t.Fatalf("unexpected upstream request id: %v", stream.UpstreamRequestID)
	}
	first := <-stream.Events
	if first.Event == nil || first.Event.Kind != ResponseEventServerModel || first.Event.Model != cyberRestrictedModelForTests {
		t.Fatalf("expected server model event, got %+v", first)
	}
	// Drain remaining events.
	for range stream.Events {
	}
}

func TestTryParseRetryAfter(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    time.Duration
	}{
		{"ms", "Please try again in 28ms. Visit docs.", 28 * time.Millisecond},
		{"seconds float", "Please try again in 1.898s. Visit docs.", time.Duration(1.898 * float64(time.Second))},
		{"azure seconds word", "Rate limit exceeded. Try again in 35 seconds.", 35 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := "rate_limit_exceeded"
			msg := tt.message
			delay := tryParseRetryAfter(streamError{Code: &code, Message: &msg})
			if delay == nil || *delay != tt.want {
				t.Fatalf("delay = %v, want %v", delay, tt.want)
			}
		})
	}
}
