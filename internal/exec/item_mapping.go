package exec

import (
	"encoding/json"
	"sort"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// mapItem converts an engine v1 [protocol.TurnItem] into an exec [ThreadItem]
// with the supplied exec item id. The boolean is false when the item has no
// exec representation (e.g. an empty reasoning summary or an unsupported
// variant), in which case no event should be emitted. This is the Go analogue of
// EventProcessorWithJsonOutput::map_item_with_id.
func (p *JSONLProcessor) mapItem(it protocol.TurnItem, id string) (ThreadItem, bool) {
	switch it.Type {
	case protocol.TurnItemKindAgentMessage:
		return ThreadItem{
			ID: id,
			Details: ThreadItemDetails{
				Kind:         ThreadItemDetailKindAgentMessage,
				AgentMessage: &AgentMessageItem{Text: agentMessageText(it.AgentMessage)},
			},
		}, true
	case protocol.TurnItemKindReasoning:
		text := reasoningText(reasoningSummary(it.Reasoning))
		if text == "" {
			return ThreadItem{}, false
		}
		return ThreadItem{
			ID: id,
			Details: ThreadItemDetails{
				Kind:      ThreadItemDetailKindReasoning,
				Reasoning: &ReasoningItem{Text: text},
			},
		}, true
	case protocol.TurnItemKindWebSearch:
		if it.WebSearch == nil {
			return ThreadItem{}, false
		}
		return ThreadItem{
			ID: id,
			Details: ThreadItemDetails{
				Kind: ThreadItemDetailKindWebSearch,
				WebSearch: &WebSearchItem{
					ID:     it.WebSearch.ID,
					Query:  it.WebSearch.Query,
					Action: it.WebSearch.Action,
				},
			},
		}, true
	case protocol.TurnItemKindFileChange:
		return mapFileChangeItem(id, it.FileChange)
	case protocol.TurnItemKindMcpToolCall:
		return mapMcpToolCallItem(id, it.McpToolCall)
	default:
		return ThreadItem{}, false
	}
}

// agentMessageText flattens an engine agent message item's content fragments
// into the single text the exec stream exposes.
func agentMessageText(item *protocol.AgentMessageItem) string {
	if item == nil {
		return ""
	}
	var text string
	for _, c := range item.Content {
		if c.Type == protocol.AgentMessageContentKindText {
			text += c.Text
		}
	}
	return text
}

// reasoningSummary returns the engine reasoning item's summary fragments.
func reasoningSummary(item *protocol.ReasoningTurnItem) []string {
	if item == nil {
		return nil
	}
	return item.SummaryText
}

// mapFileChangeItem maps an engine file-change item to its exec representation,
// translating each path change and the patch-apply status.
func mapFileChangeItem(id string, item *protocol.FileChangeItem) (ThreadItem, bool) {
	if item == nil {
		return ThreadItem{}, false
	}
	changes := make([]FileUpdateChange, 0, len(item.Changes))
	for _, path := range sortedKeys(item.Changes) {
		changes = append(changes, FileUpdateChange{
			Path: path,
			Kind: patchChangeKindFromRaw(item.Changes[path]),
		})
	}
	return ThreadItem{
		ID: id,
		Details: ThreadItemDetails{
			Kind: ThreadItemDetailKindFileChange,
			FileChange: &FileChangeItem{
				Changes: changes,
				Status:  patchApplyStatusFromRaw(item.Status),
			},
		},
	}, true
}

// sortedKeys returns the map keys in deterministic (lexicographic) order so the
// emitted file-change list is stable across runs.
func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// patchChangeKindFromRaw extracts the change kind from a raw engine FileChange
// (an internally-tagged object on "type"). Unknown kinds degrade to update.
func patchChangeKindFromRaw(raw json.RawMessage) PatchChangeKind {
	var tag struct {
		Type protocol.FileChangeKind `json:"type"`
	}
	if err := json.Unmarshal(raw, &tag); err != nil {
		return PatchChangeKindUpdate
	}
	switch tag.Type {
	case protocol.FileChangeKindAdd:
		return PatchChangeKindAdd
	case protocol.FileChangeKindDelete:
		return PatchChangeKindDelete
	default:
		return PatchChangeKindUpdate
	}
}

// patchApplyStatusFromRaw maps a raw engine patch-apply status to the exec
// status, collapsing failed and declined into failed (matching the Rust
// mapping). An absent status defaults to completed.
func patchApplyStatusFromRaw(raw json.RawMessage) PatchApplyStatus {
	if len(raw) == 0 {
		return PatchApplyStatusCompleted
	}
	var status protocol.PatchApplyStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return PatchApplyStatusCompleted
	}
	switch status {
	case protocol.PatchApplyStatusFailed, protocol.PatchApplyStatusDeclined:
		return PatchApplyStatusFailed
	default:
		return PatchApplyStatusCompleted
	}
}

// mapMcpToolCallItem maps an engine MCP tool-call item to its exec form.
func mapMcpToolCallItem(id string, item *protocol.McpToolCallItem) (ThreadItem, bool) {
	if item == nil {
		return ThreadItem{}, false
	}
	out := &McpToolCallItem{
		Server:    item.Server,
		Tool:      item.Tool,
		Arguments: item.Arguments,
		Status:    mcpStatus(item.Status),
	}
	if len(item.Result) > 0 {
		out.Result = mcpResultFromRaw(item.Result)
	}
	if item.Error != nil {
		out.Error = &McpToolCallItemError{Message: item.Error.Message}
	}
	return ThreadItem{
		ID: id,
		Details: ThreadItemDetails{
			Kind:        ThreadItemDetailKindMcpToolCall,
			McpToolCall: out,
		},
	}, true
}

// mcpStatus maps the engine MCP status to the exec status.
func mcpStatus(s protocol.McpToolCallStatus) McpToolCallStatus {
	switch s {
	case protocol.McpToolCallStatusCompleted:
		return McpToolCallStatusCompleted
	case protocol.McpToolCallStatusFailed:
		return McpToolCallStatusFailed
	default:
		return McpToolCallStatusInProgress
	}
}

// mcpResultFromRaw decodes the engine CallToolResult payload into the exec MCP
// result shape, preserving content, _meta, and structured_content. Unknown
// shapes degrade to an empty result rather than failing the stream.
func mcpResultFromRaw(raw json.RawMessage) *McpToolCallItemResult {
	var probe struct {
		Content           []json.RawMessage `json:"content"`
		Meta              json.RawMessage   `json:"_meta"`
		StructuredContent json.RawMessage   `json:"structured_content"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return &McpToolCallItemResult{}
	}
	return &McpToolCallItemResult{
		Content:           probe.Content,
		Meta:              probe.Meta,
		StructuredContent: probe.StructuredContent,
	}
}
