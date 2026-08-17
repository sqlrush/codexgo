package websearch

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func u64ptr(v uint64) *uint64 { return &v }

func TestSearchRequestMarshalOmitsNoneAndKeepsOrder(t *testing.T) {
	input := TextSearchInput("hello")
	commands := SearchCommands{SearchQuery: &[]SearchQuery{{Q: "hi"}}}
	settings := BuildSearchSettings(nil, protocol.WebSearchModeLive)
	req := SearchRequest{
		ID:              "session-1",
		Model:           "gpt-5",
		Input:           &input,
		Commands:        &commands,
		Settings:        &settings,
		MaxOutputTokens: u64ptr(2048),
	}
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"id":"session-1","model":"gpt-5","input":"hello",` +
		`"commands":{"search_query":[{"q":"hi"}]},` +
		`"settings":{"allowed_callers":["direct"],"external_web_access":true},` +
		`"max_output_tokens":2048}`
	if string(got) != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func TestSearchRequestMarshalMinimal(t *testing.T) {
	req := SearchRequest{ID: "s", Model: "m"}
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"id":"s","model":"m"}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestSearchInputItemsMarshalsAsArray(t *testing.T) {
	input := ItemsSearchInput([]protocol.ResponseItem{
		message(userRole, "hi"),
	})
	got, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// ResponseItem.MarshalJSON in internal/protocol emits keys in the JSON
	// object's natural (sorted) order via map encoding; this asserts the exact
	// bytes that protocol produces so the SearchInput wrapper stays faithful.
	want := `[{"content":[{"text":"hi","type":"input_text"}],"role":"user","type":"message"}]`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCommandsSchemaParses(t *testing.T) {
	// The embedded schema must parse without compaction, mirroring the spec
	// builder, and must not error.
	spec, err := ToolSpec()
	if err != nil {
		t.Fatalf("ToolSpec: %v", err)
	}
	// Re-marshal the parsed parameters to confirm the schema round-trips.
	if _, err := json.Marshal(spec.Namespace.Tools[0].Function.Parameters); err != nil {
		t.Fatalf("marshal parameters: %v", err)
	}
}
