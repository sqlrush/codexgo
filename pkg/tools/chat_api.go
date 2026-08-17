package tools

// Chat-completions tool serialization.
//
// codexgo extension (upstream 0.136 removed the chat wire protocol): function
// tool specs are re-shaped into the chat-completions `tools` form, which nests
// the declaration under a "function" key:
//
//	{"type":"function","function":{"name","description","parameters"}}
//
// Only plain function specs translate; hosted/namespace/freeform specs are
// Responses-API concepts with no chat equivalent and are skipped.

import (
	"encoding/json"
	"fmt"
)

// chatFunctionDecl is the nested function declaration in a chat tool.
type chatFunctionDecl struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  JsonSchema `json:"parameters"`
}

// chatTool is one entry of the chat-completions `tools` array.
type chatTool struct {
	Type     string           `json:"type"`
	Function chatFunctionDecl `json:"function"`
}

// CreateToolsJSONForChatAPI serializes the function-kind tool specs into the
// chat-completions tools form. Non-function specs (namespace, tool_search,
// image_generation, web_search, freeform) have no chat representation and are
// skipped silently — chat-wire providers simply do not see them.
func CreateToolsJSONForChatAPI(tools []ToolSpec) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(tools))
	for i, tool := range tools {
		if tool.Kind != ToolSpecKindFunction || tool.Function == nil {
			continue
		}
		raw, err := json.Marshal(chatTool{
			Type: "function",
			Function: chatFunctionDecl{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("serialize chat tool %d (%s): %w", i, tool.Name(), err)
		}
		out = append(out, raw)
	}
	return out, nil
}
