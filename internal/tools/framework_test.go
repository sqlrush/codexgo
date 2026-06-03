package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestFunctionCallErrorDisplay(t *testing.T) {
	if got := RespondToModelError("oops").Error(); got != "oops" {
		t.Fatalf("RespondToModel error = %q", got)
	}
	if got := FatalError("boom").Error(); got != "Fatal error: boom" {
		t.Fatalf("Fatal error = %q", got)
	}
}

func TestToolPayloadLogPayload(t *testing.T) {
	limit := 5
	tests := []struct {
		name    string
		payload ToolPayload
		want    string
	}{
		{"function", FunctionPayload(`{"a":1}`), `{"a":1}`},
		{"tool_search", ToolSearchPayload(SearchToolCallParams{Query: "find", Limit: &limit}), "find"},
		{"custom", CustomPayload("raw input"), "raw input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.payload.LogPayload(); got != tt.want {
				t.Fatalf("LogPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSearchToolCallParamsSerialization(t *testing.T) {
	raw, err := json.Marshal(SearchToolCallParams{Query: "q"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"query":"q"}` {
		t.Fatalf("expected limit omitted, got %s", raw)
	}
	limit := 3
	raw, _ = json.Marshal(SearchToolCallParams{Query: "q", Limit: &limit})
	jsonEqual(t, raw, `{"query":"q","limit":3}`)
}

func TestToolCallFunctionArguments(t *testing.T) {
	call := ToolCall{
		ToolName: protocol.PlainToolName("demo"),
		Payload:  FunctionPayload(`{"x":1}`),
	}
	args, err := call.FunctionArguments()
	if err != nil {
		t.Fatalf("FunctionArguments: %v", err)
	}
	if args != `{"x":1}` {
		t.Fatalf("args = %q", args)
	}

	call.Payload = CustomPayload("input")
	if _, err := call.FunctionArguments(); err == nil {
		t.Fatalf("expected fatal error for non-function payload")
	} else if !strings.Contains(err.Error(), "incompatible payload") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConversationHistoryIsSnapshot(t *testing.T) {
	items := []protocol.ResponseItem{{Type: protocol.ResponseItemKindMessage, Role: "user"}}
	hist := NewConversationHistory(items)
	items[0].Role = "assistant"
	if hist.Items()[0].Role != "user" {
		t.Fatalf("expected snapshot to be immutable to caller mutation")
	}
}

func TestNoopTurnItemEmitter(t *testing.T) {
	var emitter TurnItemEmitter = NoopTurnItemEmitter{}
	emitter.EmitStarted(context.Background(), WebSearchTurnItem(protocol.WebSearchItem{ID: "1"}))
	emitter.EmitCompleted(context.Background(), WebSearchTurnItem(protocol.WebSearchItem{ID: "1"}))
	if _, ok := emitter.ImageGenerationCompleted(context.Background(), "c", "p", "r"); ok {
		t.Fatalf("expected no artifact from noop emitter")
	}
}

func TestJsonToolOutput(t *testing.T) {
	out := NewJsonToolOutput(json.RawMessage(`{"ok": true}`))
	if !out.SuccessForLogging() {
		t.Fatalf("expected success")
	}
	// Value is compacted.
	if string(out.Value()) != `{"ok":true}` {
		t.Fatalf("expected compacted value, got %s", out.Value())
	}

	item := out.ToResponseItem("call-1", FunctionPayload("{}"))
	if item.Kind != ResponseInputItemKindFunctionCallOutput || item.CallID != "call-1" {
		t.Fatalf("unexpected function response item: %+v", item)
	}
	if item.Output.Text == nil || *item.Output.Text != `{"ok":true}` {
		t.Fatalf("unexpected output text: %+v", item.Output.Text)
	}

	customItem := out.ToResponseItem("call-2", CustomPayload("in"))
	if customItem.Kind != ResponseInputItemKindCustomToolCallOutput {
		t.Fatalf("expected custom tool call output, got %+v", customItem)
	}

	resp, ok := out.PostToolUseResponse("call-1", FunctionPayload("{}"))
	if !ok {
		t.Fatalf("expected post tool use response")
	}
	jsonEqual(t, resp, `{"ok":true}`)

	cm := out.CodeModeResult(FunctionPayload("{}"))
	jsonEqual(t, cm, `{"ok":true}`)
}

func TestJsonToolOutputDefaultHooks(t *testing.T) {
	out := NewJsonToolOutput(json.RawMessage(`1`))
	if out.PostToolUseID("abc") != "abc" {
		t.Fatalf("default PostToolUseID should echo call id")
	}
	if _, ok := out.PostToolUseInput(FunctionPayload("{}")); ok {
		t.Fatalf("default PostToolUseInput should return false")
	}
}

func TestJsonToolOutputWithExplicitFailure(t *testing.T) {
	out := NewJsonToolOutputWithSuccess(json.RawMessage(`"err"`), boolPtr(false))
	if out.SuccessForLogging() {
		t.Fatalf("expected failure")
	}
	item := out.ToResponseItem("c", FunctionPayload("{}"))
	if item.Output.Success == nil || *item.Output.Success {
		t.Fatalf("expected success=false on response item")
	}
}

func TestResponseInputItemSerialization(t *testing.T) {
	out := protocol.FunctionCallOutputFromText("result text")
	item := FunctionCallOutputInput("call-1", out)
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonEqual(t, raw, `{"type":"function_call_output","call_id":"call-1","output":"result text"}`)

	search := ToolSearchOutputInput("c", "completed", "sync", []json.RawMessage{json.RawMessage(`{"name":"t"}`)})
	raw, _ = json.Marshal(search)
	jsonEqual(t, raw, `{"type":"tool_search_output","call_id":"c","status":"completed","execution":"sync","tools":[{"name":"t"}]}`)
}

func TestToolExposureIsDirect(t *testing.T) {
	tests := []struct {
		exposure ToolExposure
		direct   bool
	}{
		{ToolExposureDirect, true},
		{ToolExposureDirectModelOnly, true},
		{ToolExposureDeferred, false},
		{ToolExposureHidden, false},
	}
	for _, tt := range tests {
		if got := tt.exposure.IsDirect(); got != tt.direct {
			t.Fatalf("exposure %d IsDirect=%v, want %v", tt.exposure, got, tt.direct)
		}
	}
}

func TestDiscoverableToolHelpers(t *testing.T) {
	connector := ConnectorDiscoverableTool(appserverproto.AppInfo{
		ID:         "connector_cal",
		Name:       "Calendar",
		InstallURL: strp("https://example.test/cal"),
	})
	if connector.ToolType() != DiscoverableToolTypeConnector {
		t.Fatalf("expected connector type")
	}
	if connector.ID() != "connector_cal" || connector.Name() != "Calendar" {
		t.Fatalf("connector accessors wrong")
	}
	if connector.InstallURL() == nil || *connector.InstallURL() != "https://example.test/cal" {
		t.Fatalf("connector install url wrong")
	}

	plugin := PluginDiscoverableTool(DiscoverablePluginInfo{ID: "slack", Name: "Slack"})
	if plugin.ToolType() != DiscoverableToolTypePlugin {
		t.Fatalf("expected plugin type")
	}
	if plugin.InstallURL() != nil {
		t.Fatalf("plugin install url should be nil")
	}
}

func TestFilterRequestPluginInstallForTUI(t *testing.T) {
	connector := ConnectorDiscoverableTool(appserverproto.AppInfo{ID: "c", Name: "C"})
	plugin := PluginDiscoverableTool(DiscoverablePluginInfo{ID: "p", Name: "P"})
	tools := []DiscoverableTool{connector, plugin}

	tui := tuiClientName
	filtered := FilterRequestPluginInstallDiscoverableToolsForClient(tools, &tui)
	if len(filtered) != 1 || filtered[0].Kind != DiscoverableToolKindConnector {
		t.Fatalf("expected only connector for TUI, got %+v", filtered)
	}

	other := "other-client"
	unfiltered := FilterRequestPluginInstallDiscoverableToolsForClient(tools, &other)
	if len(unfiltered) != 2 {
		t.Fatalf("expected all tools for non-TUI client")
	}
}

func TestCollectRequestPluginInstallEntries(t *testing.T) {
	connector := ConnectorDiscoverableTool(appserverproto.AppInfo{
		ID: "c", Name: "C", Description: strp("connector desc"),
	})
	plugin := PluginDiscoverableTool(DiscoverablePluginInfo{
		ID: "p", Name: "P", Description: strp("plugin desc"),
		HasSkills: true, MCPServerNames: []string{"srv"}, AppConnectorIDs: []string{"a"},
	})

	entries := CollectRequestPluginInstallEntries([]DiscoverableTool{connector, plugin})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries")
	}
	if entries[0].ToolType != DiscoverableToolTypeConnector || entries[0].HasSkills {
		t.Fatalf("connector entry wrong: %+v", entries[0])
	}
	// Connector empty slices must serialize as [] not null.
	raw, _ := json.Marshal(entries[0])
	if !strings.Contains(string(raw), `"mcp_server_names":[]`) {
		t.Fatalf("connector mcp_server_names should be [], got %s", raw)
	}
	if entries[1].ToolType != DiscoverableToolTypePlugin || !entries[1].HasSkills {
		t.Fatalf("plugin entry wrong: %+v", entries[1])
	}
}

func TestDiscoverableToolEnumWireNames(t *testing.T) {
	if string(DiscoverableToolTypeConnector) != "connector" {
		t.Fatalf("connector wire name wrong")
	}
	if string(DiscoverableToolActionInstall) != "install" {
		t.Fatalf("install action wire name wrong")
	}
}

func TestCodeModeNameForToolName(t *testing.T) {
	tests := []struct {
		name     string
		toolName protocol.ToolName
		want     string
	}{
		{"plain", protocol.PlainToolName("read"), "read"},
		{"namespace trailing underscore", protocol.NamespacedToolName("mcp_", "search"), "mcp_search"},
		{"name leading underscore", protocol.NamespacedToolName("ns", "_private"), "ns_private"},
		{"double underscore join", protocol.NamespacedToolName("ns", "search"), "ns__search"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CodeModeNameForToolName(tt.toolName); got != tt.want {
				t.Fatalf("CodeModeNameForToolName() = %q, want %q", got, tt.want)
			}
		})
	}
}
