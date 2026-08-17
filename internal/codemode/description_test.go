package codemode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// uintVal returns a pointer to v for pragma expectations.
func uintVal(v uint64) *uint64 { return &v }

// decodeSchema parses a JSON schema literal into the any shape used by the
// renderer.
func decodeSchema(t *testing.T, raw string) any {
	t.Helper()
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode schema %q: %v", raw, err)
	}
	return out
}

// TestParseExecSource ports parse_exec_source_without_pragma /
// parse_exec_source_with_pragma and exercises the pragma error strings.
func TestParseExecSource(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantCode    string
		wantYield   *uint64
		wantTokens  *uint64
		wantErrPart string
	}{
		{
			name:     "without-pragma",
			input:    "text('hi')",
			wantCode: "text('hi')",
		},
		{
			name:      "with-pragma-yield",
			input:     "// @exec: {\"yield_time_ms\": 10}\ntext('hi')",
			wantCode:  "text('hi')",
			wantYield: uintVal(10),
		},
		{
			name:       "with-pragma-both-fields",
			input:      "// @exec: {\"yield_time_ms\": 5, \"max_output_tokens\": 7}\ndo()",
			wantCode:   "do()",
			wantYield:  uintVal(5),
			wantTokens: uintVal(7),
		},
		{
			name:        "empty-input",
			input:       "   ",
			wantErrPart: "exec expects raw JavaScript source text",
		},
		{
			name:        "pragma-without-body",
			input:       "// @exec: {\"yield_time_ms\": 10}\n   ",
			wantErrPart: "exec pragma must be followed by JavaScript source",
		},
		{
			name:        "pragma-not-object",
			input:       "// @exec: [1,2]\ncode()",
			wantErrPart: "exec pragma must be a JSON object",
		},
		{
			name:        "pragma-unknown-field",
			input:       "// @exec: {\"bogus\": 1}\ncode()",
			wantErrPart: "exec pragma only supports",
		},
		{
			name:        "pragma-invalid-json",
			input:       "// @exec: {not json}\ncode()",
			wantErrPart: "exec pragma must be valid JSON",
		},
		{
			name:        "pragma-negative-value",
			input:       "// @exec: {\"yield_time_ms\": -1}\ncode()",
			wantErrPart: "must be non-negative safe integers",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := ParseExecSource(tc.input)
			if tc.wantErrPart != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrPart)
				}
				if !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseExecSource: %v", err)
			}
			if parsed.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", parsed.Code, tc.wantCode)
			}
			assertUintPtr(t, "yield", parsed.YieldTimeMS, tc.wantYield)
			assertUintPtr(t, "tokens", parsed.MaxOutputTokens, tc.wantTokens)
		})
	}
}

func assertUintPtr(t *testing.T, label string, got, want *uint64) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Fatalf("%s = %d, want nil", label, *got)
	case want != nil && got == nil:
		t.Fatalf("%s = nil, want %d", label, *want)
	case want != nil && got != nil && *got != *want:
		t.Fatalf("%s = %d, want %d", label, *got, *want)
	}
}

// TestNormalizeCodeModeIdentifier ports normalize_identifier_rewrites_invalid_characters.
func TestNormalizeCodeModeIdentifier(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"mcp__ologs__get_profile", "mcp__ologs__get_profile"},
		{"hidden-dynamic-tool", "hidden_dynamic_tool"},
		{"123abc", "_23abc"}, // first char digit becomes underscore
		{"", "_"},            // empty becomes a single underscore
		{"a.b c", "a_b_c"},   // dots and spaces become underscores
		{"$keep", "$keep"},   // dollar is valid
	}
	for _, tc := range cases {
		if got := NormalizeCodeModeIdentifier(tc.in); got != tc.want {
			t.Errorf("NormalizeCodeModeIdentifier(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestAugmentToolDefinitionAppendsTypedDeclaration ports
// augment_tool_definition_appends_typed_declaration.
func TestAugmentToolDefinitionAppendsTypedDeclaration(t *testing.T) {
	def := ToolDefinition{
		Name:        "hidden_dynamic_tool",
		ToolName:    protocol.PlainToolName("hidden_dynamic_tool"),
		Description: "Test tool",
		Kind:        CodeModeToolKindFunction,
		InputSchema: decodeSchema(t, `{
			"type":"object",
			"properties":{"city":{"type":"string"}},
			"required":["city"],
			"additionalProperties":false
		}`),
		OutputSchema: decodeSchema(t, `{
			"type":"object",
			"properties":{"ok":{"type":"boolean"}},
			"required":["ok"]
		}`),
	}

	desc := AugmentToolDefinition(def).Description
	if !strings.Contains(desc, "declare const tools") {
		t.Errorf("missing declare const tools in:\n%s", desc)
	}
	want := "hidden_dynamic_tool(args: { city: string; }): Promise<{ ok: boolean; }>;"
	if !strings.Contains(desc, want) {
		t.Errorf("missing declaration %q in:\n%s", want, desc)
	}
}

// TestAugmentToolDefinitionIncludesPropertyDescriptions ports
// augment_tool_definition_includes_property_descriptions_as_comments.
func TestAugmentToolDefinitionIncludesPropertyDescriptions(t *testing.T) {
	def := ToolDefinition{
		Name:        "weather_tool",
		ToolName:    protocol.PlainToolName("weather_tool"),
		Description: "Weather tool",
		Kind:        CodeModeToolKindFunction,
		InputSchema: decodeSchema(t, `{
			"type":"object",
			"properties":{
				"weather":{
					"type":"array",
					"description":"look up weather for a given list of locations",
					"items":{
						"type":"object",
						"properties":{"location":{"type":"string"}},
						"required":["location"]
					}
				}
			},
			"required":["weather"]
		}`),
		OutputSchema: decodeSchema(t, `{
			"type":"object",
			"properties":{
				"forecast":{"type":"string","description":"human readable weather forecast"}
			},
			"required":["forecast"]
		}`),
	}

	desc := AugmentToolDefinition(def).Description
	want := `weather_tool(args: {
  // look up weather for a given list of locations
  weather: Array<{ location: string; }>;
}): Promise<{
  // human readable weather forecast
  forecast: string;
}>;`
	if !strings.Contains(desc, want) {
		t.Errorf("missing typed declaration block.\ngot:\n%s\nwant substring:\n%s", desc, want)
	}
}

// TestAugmentToolDefinitionLeavesExecUntouched verifies the exec tool itself is
// returned unchanged (no typed declaration appended).
func TestAugmentToolDefinitionLeavesExecUntouched(t *testing.T) {
	def := ToolDefinition{Name: PublicToolName, Description: "run js"}
	got := AugmentToolDefinition(def)
	if got.Description != "run js" {
		t.Fatalf("exec description was altered: %q", got.Description)
	}
}

// TestBuildExecToolDescriptionTimeoutHelpers ports
// exec_description_mentions_timeout_helpers.
func TestBuildExecToolDescriptionTimeoutHelpers(t *testing.T) {
	desc := BuildExecToolDescription(nil, nil, false, false)
	for _, want := range []string{
		"`setTimeout(callback: () => void, delayMs?: number)`",
		"`clearTimeout(timeoutId?: number)`",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("exec description missing %q", want)
		}
	}
}

// TestBuildExecToolDescriptionDeferredTools ports
// exec_description_mentions_deferred_nested_tools_when_available.
func TestBuildExecToolDescriptionDeferredTools(t *testing.T) {
	with := BuildExecToolDescription(nil, nil, false, true)
	if !strings.Contains(with, "Some nested MCP/app tools may be omitted") {
		t.Error("deferred description missing guidance sentence")
	}
	if !strings.Contains(with, "filter `ALL_TOOLS` by `name` and `description`") {
		t.Error("deferred description missing filter guidance")
	}

	without := BuildExecToolDescription(nil, nil, false, false)
	if strings.Contains(without, "Some nested MCP/app tools may be omitted") {
		t.Error("non-deferred description should not include deferred guidance")
	}
}

// TestBuildExecToolDescriptionIncludesNestedTools ports
// code_mode_only_description_includes_nested_tools.
func TestBuildExecToolDescriptionIncludesNestedTools(t *testing.T) {
	desc := BuildExecToolDescription(
		[]ToolDefinition{{
			Name:        "foo",
			ToolName:    protocol.PlainToolName("foo"),
			Description: "bar",
			Kind:        CodeModeToolKindFunction,
		}},
		nil,
		true,
		false,
	)
	if !strings.Contains(desc, "### `foo`\nbar") {
		t.Errorf("missing nested tool heading/body in:\n%s", desc)
	}
}

// TestBuildExecToolDescriptionGroupsNamespaceOnce ports
// code_mode_only_description_groups_namespace_instructions_once.
func TestBuildExecToolDescriptionGroupsNamespaceOnce(t *testing.T) {
	namespaces := map[string]ToolNamespaceDescription{
		"mcp__sample__": {Name: "mcp__sample", Description: "Shared namespace guidance."},
	}
	mcpOut := mcpCallToolResultSchema(t, `{"type":"object","properties":{},"additionalProperties":false}`)
	tools := []ToolDefinition{
		{
			Name:         "mcp__sample__alpha",
			ToolName:     protocol.NamespacedToolName("mcp__sample__", "alpha"),
			Description:  "First tool",
			Kind:         CodeModeToolKindFunction,
			InputSchema:  decodeSchema(t, `{"type":"object","properties":{},"additionalProperties":false}`),
			OutputSchema: mcpOut,
		},
		{
			Name:         "mcp__sample__beta",
			ToolName:     protocol.NamespacedToolName("mcp__sample__", "beta"),
			Description:  "Second tool",
			Kind:         CodeModeToolKindFunction,
			InputSchema:  decodeSchema(t, `{"type":"object","properties":{},"additionalProperties":false}`),
			OutputSchema: mcpCallToolResultSchema(t, `{"type":"object","properties":{},"additionalProperties":false}`),
		},
	}

	desc := BuildExecToolDescription(tools, namespaces, true, false)

	if n := strings.Count(desc, "## mcp__sample"); n != 1 {
		t.Errorf("namespace heading appears %d times, want 1", n)
	}
	if !strings.Contains(desc, "## mcp__sample\nShared namespace guidance.") {
		t.Error("missing namespace guidance block")
	}
	for _, want := range []string{
		"declare const tools: { mcp__sample__alpha(args: {}): Promise<CallToolResult<{}>>; };",
		"declare const tools: { mcp__sample__beta(args: {}): Promise<CallToolResult<{}>>; };",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("missing declaration %q", want)
		}
	}
}

// TestBuildExecToolDescriptionOmitsEmptyNamespace ports
// code_mode_only_description_omits_empty_namespace_sections.
func TestBuildExecToolDescriptionOmitsEmptyNamespace(t *testing.T) {
	namespaces := map[string]ToolNamespaceDescription{
		"mcp__sample__": {Name: "mcp__sample", Description: ""},
	}
	tools := []ToolDefinition{{
		Name:         "mcp__sample__alpha",
		ToolName:     protocol.NamespacedToolName("mcp__sample__", "alpha"),
		Description:  "First tool",
		Kind:         CodeModeToolKindFunction,
		InputSchema:  decodeSchema(t, `{"type":"object","properties":{},"additionalProperties":false}`),
		OutputSchema: mcpCallToolResultSchema(t, `{"type":"object","properties":{},"additionalProperties":false}`),
	}}

	desc := BuildExecToolDescription(tools, namespaces, true, false)
	if strings.Contains(desc, "## mcp__sample") {
		t.Error("empty namespace section should be omitted")
	}
	if !strings.Contains(desc, "### `mcp__sample__alpha`") {
		t.Error("tool heading missing")
	}
}

// TestBuildExecToolDescriptionSharedMCPTypesOnce ports
// code_mode_only_description_renders_shared_mcp_types_once.
func TestBuildExecToolDescriptionSharedMCPTypesOnce(t *testing.T) {
	mkOut := func(structured string) any {
		return decodeSchema(t, `{
			"type":"object",
			"properties":{
				"content":{"type":"array","items":{"type":"object"}},
				"structuredContent":`+structured+`,
				"isError":{"type":"boolean"},
				"_meta":{"type":"object"}
			},
			"required":["content"],
			"additionalProperties":false
		}`)
	}
	tools := []ToolDefinition{
		{
			Name:         "mcp__sample__alpha",
			ToolName:     protocol.NamespacedToolName("mcp__sample__", "alpha"),
			Description:  "First tool",
			Kind:         CodeModeToolKindFunction,
			InputSchema:  decodeSchema(t, `{"type":"object","properties":{},"additionalProperties":false}`),
			OutputSchema: mkOut(`{"type":"object","properties":{"echo":{"type":"string"}},"required":["echo"],"additionalProperties":false}`),
		},
		{
			Name:         "mcp__sample__beta",
			ToolName:     protocol.NamespacedToolName("mcp__sample__", "beta"),
			Description:  "Second tool",
			Kind:         CodeModeToolKindFunction,
			InputSchema:  decodeSchema(t, `{"type":"object","properties":{},"additionalProperties":false}`),
			OutputSchema: mkOut(`{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"],"additionalProperties":false}`),
		},
	}

	desc := BuildExecToolDescription(tools, nil, true, false)
	if n := strings.Count(desc, "type CallToolResult<TStructured = { [key: string]: unknown }>"); n != 1 {
		t.Errorf("shared MCP preamble rendered %d times, want 1", n)
	}
	if n := strings.Count(desc, "Shared MCP Types:"); n != 1 {
		t.Errorf("\"Shared MCP Types:\" appears %d times, want 1", n)
	}
}

// mcpCallToolResultSchema builds an MCP CallToolResult output schema wrapping the
// given structuredContent schema, mirroring the Rust test helper.
func mcpCallToolResultSchema(t *testing.T, structured string) any {
	t.Helper()
	return decodeSchema(t, `{
		"type":"object",
		"properties":{
			"content":{"type":"array","items":{"type":"object"}},
			"structuredContent":`+structured+`,
			"isError":{"type":"boolean"},
			"_meta":{"type":"object"}
		},
		"required":["content"],
		"additionalProperties":false
	}`)
}

// TestIsCodeModeNestedTool verifies exec/wait are excluded and others included.
func TestIsCodeModeNestedTool(t *testing.T) {
	cases := map[string]bool{
		PublicToolName: false,
		WaitToolName:   false,
		"exec_command": true,
		"mcp__x__y":    true,
	}
	for name, want := range cases {
		if got := IsCodeModeNestedTool(name); got != want {
			t.Errorf("IsCodeModeNestedTool(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestEnabledToolMetadataOf verifies the projection normalizes the global name.
func TestEnabledToolMetadataOf(t *testing.T) {
	def := ToolDefinition{
		Name:        "hidden-tool",
		ToolName:    protocol.PlainToolName("hidden-tool"),
		Description: "desc",
		Kind:        CodeModeToolKindFreeform,
	}
	md := EnabledToolMetadataOf(def)
	if md.GlobalName != "hidden_tool" {
		t.Errorf("GlobalName = %q, want hidden_tool", md.GlobalName)
	}
	if md.Kind != CodeModeToolKindFreeform || md.Description != "desc" {
		t.Errorf("metadata = %+v", md)
	}
}

// TestCodeModeToolKindJSON verifies the snake_case round-trip.
func TestCodeModeToolKindJSON(t *testing.T) {
	cases := []struct {
		kind CodeModeToolKind
		want string
	}{
		{CodeModeToolKindFunction, `"function"`},
		{CodeModeToolKindFreeform, `"freeform"`},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.kind)
		if err != nil {
			t.Fatalf("marshal %v: %v", tc.kind, err)
		}
		if string(b) != tc.want {
			t.Fatalf("marshal %v = %s, want %s", tc.kind, b, tc.want)
		}
		var got CodeModeToolKind
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", b, err)
		}
		if got != tc.kind {
			t.Fatalf("round-trip %s = %v, want %v", b, got, tc.kind)
		}
	}
	var bad CodeModeToolKind
	if err := json.Unmarshal([]byte(`"bogus"`), &bad); err == nil {
		t.Fatal("expected error decoding unknown tool kind")
	}
}
