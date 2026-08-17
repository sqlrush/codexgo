package tools

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ParseMcpTool lowers an MCP tool into a ToolDefinition. Mirrors Rust
// `parse_mcp_tool`: OpenAI models mandate a `properties` field, so an empty
// object is inserted when absent or null, matching the Agents SDK. The output
// schema is wrapped in the MCP CallToolResult envelope.
func ParseMcpTool(tool protocol.Tool) (ToolDefinition, error) {
	var inputSchemaValue any
	if len(tool.InputSchema) > 0 {
		if err := json.Unmarshal(tool.InputSchema, &inputSchemaValue); err != nil {
			return ToolDefinition{}, fmt.Errorf("decode mcp tool input schema: %w", err)
		}
	}

	if obj, ok := inputSchemaValue.(map[string]any); ok {
		if props, present := obj["properties"]; !present || props == nil {
			obj["properties"] = map[string]any{}
		}
	}

	normalizedInput, err := json.Marshal(inputSchemaValue)
	if err != nil {
		return ToolDefinition{}, fmt.Errorf("re-encode mcp tool input schema: %w", err)
	}

	inputSchema, err := ParseToolInputSchema(normalizedInput)
	if err != nil {
		return ToolDefinition{}, fmt.Errorf("parse mcp tool input schema: %w", err)
	}

	structuredContentSchema := json.RawMessage("{}")
	if tool.OutputSchema != nil && len(*tool.OutputSchema) > 0 {
		structuredContentSchema = *tool.OutputSchema
	}

	outputSchema, err := McpCallToolResultOutputSchema(structuredContentSchema)
	if err != nil {
		return ToolDefinition{}, fmt.Errorf("build mcp output schema: %w", err)
	}

	description := ""
	if tool.Description != nil {
		description = *tool.Description
	}

	return ToolDefinition{
		Name:         tool.Name,
		Description:  description,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		DeferLoading: false,
	}, nil
}

// McpCallToolResultOutputSchema wraps a tool's structured-content schema in the
// MCP CallToolResult output-schema envelope. Mirrors Rust
// `mcp_call_tool_result_output_schema`.
func McpCallToolResultOutputSchema(structuredContentSchema json.RawMessage) (json.RawMessage, error) {
	var structured any
	if len(structuredContentSchema) == 0 {
		structured = map[string]any{}
	} else if err := json.Unmarshal(structuredContentSchema, &structured); err != nil {
		return nil, fmt.Errorf("decode structured content schema: %w", err)
	}

	envelope := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
				},
			},
			"structuredContent": structured,
			"isError": map[string]any{
				"type": "boolean",
			},
			"_meta": map[string]any{
				"type": "object",
			},
		},
		"required":             []any{"content"},
		"additionalProperties": false,
	}

	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode mcp output schema: %w", err)
	}
	return raw, nil
}
