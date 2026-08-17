package protocol

import (
	"encoding/json"
	"testing"
)

func TestResponseItemMessageOmitsIDOnSerialize(t *testing.T) {
	id := "msg-1"
	r := ResponseItem{
		Type:      ResponseItemKindMessage,
		MessageID: &id,
		Role:      "user",
		Content:   []ContentItem{{Type: ContentItemKindInputText, Text: "hi"}},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"content":[{"text":"hi","type":"input_text"}],"role":"user","type":"message"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch (id must be skip-serialized):\n got: %s\nwant: %s", b, want)
	}
	if !r.IsUserMessage() {
		t.Fatalf("expected IsUserMessage true")
	}
}

func TestResponseItemFunctionCallRoundTrip(t *testing.T) {
	in := []byte(`{"type":"function_call","name":"shell","arguments":"{\"cmd\":\"ls\"}","call_id":"c1"}`)
	var r ResponseItem
	if err := json.Unmarshal(in, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Type != ResponseItemKindFunctionCall || r.Name != "shell" || r.CallID != "c1" {
		t.Fatalf("unexpected decode: %+v", r)
	}
	if r.Arguments != `{"cmd":"ls"}` {
		t.Fatalf("arguments mismatch: %q", r.Arguments)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"arguments":"{\"cmd\":\"ls\"}","call_id":"c1","name":"shell","type":"function_call"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
}

func TestResponseItemFunctionCallOutputTextBody(t *testing.T) {
	r := ResponseItem{
		Type:   ResponseItemKindFunctionCallOutput,
		CallID: "c1",
		Output: ptr(FunctionCallOutputFromText("ok")),
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"call_id":"c1","output":"ok","type":"function_call_output"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}

	var got ResponseItem
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Output == nil || got.Output.Text == nil || *got.Output.Text != "ok" {
		t.Fatalf("text body lost: %+v", got.Output)
	}
}

func TestResponseItemFunctionCallOutputContentItems(t *testing.T) {
	r := ResponseItem{
		Type:   ResponseItemKindFunctionCallOutput,
		CallID: "c1",
		Output: ptr(FunctionCallOutputFromContentItems([]FunctionCallOutputContentItem{
			{Type: FunctionCallOutputContentItemKindInputText, Text: "a"},
		})),
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"call_id":"c1","output":[{"text":"a","type":"input_text"}],"type":"function_call_output"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
}

func TestResponseItemUnknownTypeBecomesOther(t *testing.T) {
	in := []byte(`{"type":"future_thing","payload":42}`)
	var r ResponseItem
	if err := json.Unmarshal(in, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Type != ResponseItemKindOther {
		t.Fatalf("expected Other, got %q", r.Type)
	}
	// Extra is captured for inspection.
	if string(r.Extra["payload"]) != "42" {
		t.Fatalf("expected Extra payload captured, got %v", r.Extra)
	}
	// Matching Rust's lossy #[serde(other)], serialization drops the payload.
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"type":"other"}` {
		t.Fatalf("Other must serialize lossily as {\"type\":\"other\"}, got: %s", b)
	}
}

func TestResponseItemReasoningContentConditional(t *testing.T) {
	// content is emitted only when it contains at least one reasoning_text item.
	rcSkip := []ReasoningItemContent{{Type: ReasoningItemContentKindText, Text: "x"}}
	r := ResponseItem{
		Type:             ResponseItemKindReasoning,
		Summary:          []ReasoningItemReasoningSummary{NewSummaryText("s")},
		ReasoningContent: &rcSkip,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// content should be absent; encrypted_content always present (null).
	want := `{"encrypted_content":null,"summary":[{"type":"summary_text","text":"s"}],"type":"reasoning"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch (content should be skipped):\n got: %s\nwant: %s", b, want)
	}

	rcEmit := []ReasoningItemContent{{Type: ReasoningItemContentKindReasoningText, Text: "y"}}
	r.ReasoningContent = &rcEmit
	b2, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want2 := `{"content":[{"type":"reasoning_text","text":"y"}],"encrypted_content":null,"summary":[{"type":"summary_text","text":"s"}],"type":"reasoning"}`
	if string(b2) != want2 {
		t.Fatalf("JSON mismatch (content should be present):\n got: %s\nwant: %s", b2, want2)
	}
}

func ptr[T any](v T) *T { return &v }
