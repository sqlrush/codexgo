package tools

import (
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ParseDynamicTool lowers a dynamic tool spec into a ToolDefinition, sanitizing
// its input schema. Mirrors Rust `parse_dynamic_tool`.
func ParseDynamicTool(tool protocol.DynamicToolSpec) (ToolDefinition, error) {
	inputSchema, err := ParseToolInputSchema(tool.InputSchema)
	if err != nil {
		return ToolDefinition{}, fmt.Errorf("parse dynamic tool input schema: %w", err)
	}
	return ToolDefinition{
		Name:         tool.Name,
		Description:  tool.Description,
		InputSchema:  inputSchema,
		OutputSchema: nil,
		DeferLoading: tool.DeferLoading,
	}, nil
}
