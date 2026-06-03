package write

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// SanitizeResponseItemForMemories filters a single response item for stage-1
// memory extraction, mirroring sanitize_response_item_for_memories.
//
// The rules, in order:
//   - Non-message items are kept only when ShouldPersistResponseItemForMemories
//     accepts them.
//   - Developer-role messages are always excluded (returns nil, false).
//   - Non-user, non-developer messages are kept verbatim.
//   - User messages have contextual fragments (AGENTS.md instructions and skill
//     blocks) stripped; if nothing remains, the message is dropped.
//
// The boolean is false when the item should be omitted entirely.
func SanitizeResponseItemForMemories(item protocol.ResponseItem) (protocol.ResponseItem, bool) {
	if item.Type != protocol.ResponseItemKindMessage {
		if rollout.ShouldPersistResponseItemForMemories(item) {
			return item, true
		}
		return protocol.ResponseItem{}, false
	}

	if item.Role == "developer" {
		return protocol.ResponseItem{}, false
	}

	if item.Role != "user" {
		return item, true
	}

	filtered := make([]protocol.ContentItem, 0, len(item.Content))
	for _, contentItem := range item.Content {
		if isMemoryExcludedContextualUserFragment(contentItem) {
			continue
		}
		filtered = append(filtered, contentItem)
	}
	if len(filtered) == 0 {
		return protocol.ResponseItem{}, false
	}

	sanitized := item
	sanitized.Content = filtered
	return sanitized, true
}

// SerializeFilteredRolloutResponseItems sanitizes and JSON-encodes the response
// items embedded in rollout items, mirroring
// serialize_filtered_rollout_response_items. The redact callback applies secret
// redaction to the serialized output (the codexgo redactor lives outside this
// package); pass a no-op (identity) redactor when redaction is unnecessary.
func SerializeFilteredRolloutResponseItems(
	items []rollout.RolloutItem,
	redact func(string) string,
) (string, error) {
	filtered := make([]protocol.ResponseItem, 0, len(items))
	for _, item := range items {
		if item.Kind != rollout.RolloutItemKindResponseItem || item.ResponseItem == nil {
			continue
		}
		if sanitized, ok := SanitizeResponseItemForMemories(*item.ResponseItem); ok {
			filtered = append(filtered, sanitized)
		}
	}
	serialized, err := json.Marshal(filtered)
	if err != nil {
		return "", fmt.Errorf("failed to serialize rollout memory: %w", err)
	}
	out := string(serialized)
	if redact != nil {
		out = redact(out)
	}
	return out, nil
}

// isMemoryExcludedContextualUserFragment reports whether a user content fragment
// is contextual scaffolding (AGENTS.md instructions or a skill block) that must
// not be persisted into memories, mirroring
// is_memory_excluded_contextual_user_fragment.
func isMemoryExcludedContextualUserFragment(contentItem protocol.ContentItem) bool {
	if contentItem.Type != protocol.ContentItemKindInputText {
		return false
	}
	text := contentItem.Text
	return matchesMarkedFragment(text, "# AGENTS.md instructions for ", "</INSTRUCTIONS>") ||
		matchesMarkedFragment(text, "<skill>", "</skill>")
}

// matchesMarkedFragment reports whether the trimmed text both begins with
// startMarker and ends with endMarker, case-insensitively, mirroring
// matches_marked_fragment.
func matchesMarkedFragment(text, startMarker, endMarker string) bool {
	trimmed := strings.TrimLeft(text, " \t\r\n\x0b\x0c")
	startsWith := len(trimmed) >= len(startMarker) &&
		strings.EqualFold(trimmed[:len(startMarker)], startMarker)

	trimmed = strings.TrimRight(trimmed, " \t\r\n\x0b\x0c")
	start := len(trimmed) - len(endMarker)
	if start < 0 {
		return false
	}
	endsWith := strings.EqualFold(trimmed[start:], endMarker)
	return startsWith && endsWith
}
