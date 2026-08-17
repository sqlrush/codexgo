package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestUserInputTextRoundTrip(t *testing.T) {
	ph := "image"
	u := UserInput{
		Type: UserInputKindText,
		Text: "hello",
		TextElements: []TextElement{
			NewTextElement(ByteRange{Start: 0, End: 5}, &ph),
		},
	}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"text":"hello","text_elements":[{"byte_range":{"start":0,"end":5},"placeholder":"image"}],"type":"text"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got UserInput
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, u) {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, u)
	}
}

func TestUserInputImageOmitsDetail(t *testing.T) {
	u := UserInput{Type: UserInputKindImage, ImageURL: "data:image/png;base64,xxx"}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"image_url":"data:image/png;base64,xxx","type":"image"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
}

func TestUserInputUnknownVariantTolerated(t *testing.T) {
	// #[non_exhaustive]: an unknown variant must round-trip without error.
	in := []byte(`{"type":"voice","duration_ms":1200,"url":"x"}`)
	var u UserInput
	if err := json.Unmarshal(in, &u); err != nil {
		t.Fatalf("unmarshal unknown variant: %v", err)
	}
	if u.Type != "voice" {
		t.Fatalf("expected type voice, got %q", u.Type)
	}
	b, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Re-decode to compare structurally (key order may differ).
	var orig, got map[string]any
	_ = json.Unmarshal(in, &orig)
	_ = json.Unmarshal(b, &got)
	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("unknown variant lost data:\n got: %v\nwant: %v", got, orig)
	}
}

func TestTurnItemAgentMessageRoundTrip(t *testing.T) {
	phase := MessagePhaseFinalAnswer
	item := TurnItem{
		Type: TurnItemKindAgentMessage,
		AgentMessage: &AgentMessageItem{
			ID:      "abc",
			Content: []AgentMessageContent{NewAgentMessageText("done")},
			Phase:   &phase,
		},
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Ensure the discriminator is present and PascalCase (no rename_all).
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if string(probe["type"]) != `"AgentMessage"` {
		t.Fatalf("type tag mismatch: %s", probe["type"])
	}
	var got TurnItem
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, item) {
		t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", got.AgentMessage, item.AgentMessage)
	}
}

func TestTurnItemMcpToolCallRoundTrip(t *testing.T) {
	item := TurnItem{
		Type: TurnItemKindMcpToolCall,
		McpToolCall: &McpToolCallItem{
			ID:        "call-1",
			Server:    "srv",
			Tool:      "do",
			Arguments: json.RawMessage(`{"a":1}`),
			Status:    McpToolCallStatusCompleted,
		},
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TurnItem
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != TurnItemKindMcpToolCall || got.McpToolCall.ID != "call-1" {
		t.Fatalf("unexpected decode: %+v", got.McpToolCall)
	}
	if string(got.McpToolCall.Arguments) != `{"a":1}` {
		t.Fatalf("arguments mismatch: %s", got.McpToolCall.Arguments)
	}
}

func TestDynamicToolSpecLegacyExposeToContext(t *testing.T) {
	in := []byte(`{"name":"t","description":"d","inputSchema":{"type":"object"},"exposeToContext":false}`)
	var d DynamicToolSpec
	if err := json.Unmarshal(in, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !d.DeferLoading {
		t.Fatalf("expected deferLoading=true from exposeToContext=false")
	}
}

func TestDynamicToolCallOutputContentItemRoundTrip(t *testing.T) {
	item := DynamicToolCallOutputContentItem{
		Type:     DynamicToolCallOutputContentItemKindInputImage,
		ImageURL: "data:...",
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"inputImage","imageUrl":"data:..."}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got DynamicToolCallOutputContentItem
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != item {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, item)
	}
}
