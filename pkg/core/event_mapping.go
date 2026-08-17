package core

import (
	"strings"

	"github.com/google/uuid"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// newItemID generates a fresh UUIDv4 string for a turn item whose source
// response item carries no id, mirroring the Rust `Uuid::new_v4().to_string()`
// fallback used by `parse_agent_message` and `UserMessageItem::new`.
func newItemID() string { return uuid.NewString() }

// This file ports `event_mapping.rs`: it maps a streamed [protocol.ResponseItem]
// (the Responses-API conversation history shape) onto the client-visible
// [protocol.TurnItem] family. The turn runner uses this projection both for
// item-lifecycle events and for the compaction history-rebuild logic, which scans
// history for user messages.
//
// The mapping is intentionally total over the variants core understands and
// returns (TurnItem{}, false) for items that have no client-visible turn-item
// representation (system messages, tool calls/outputs, compaction markers, and
// forward-compatible unknown items).

// Contextual-developer prefixes mark a developer message as rollback-trimmable
// contextual scaffolding rather than persistent instruction text. Mirrors the
// Rust `CONTEXTUAL_DEVELOPER_PREFIXES`.
var contextualDeveloperPrefixes = []string{
	"<permissions instructions>",
	"<model_switch>",
	collaborationModeOpenTag,
	realtimeConversationOpenTag,
	"<personality_spec>",
}

// collaborationModeOpenTag / realtimeConversationOpenTag mirror the Rust
// `COLLABORATION_MODE_OPEN_TAG` / `REALTIME_CONVERSATION_OPEN_TAG` constants
// (owned by codex_protocol::protocol). They are duplicated here as private
// constants to avoid widening the protocol surface for the turn-running subset.
const (
	collaborationModeOpenTag    = "<collaboration_mode>"
	realtimeConversationOpenTag = "<realtime_conversation>"
)

// Image marker tags. Mirrors the Rust IMAGE_OPEN_TAG / IMAGE_CLOSE_TAG /
// LOCAL_IMAGE_OPEN_TAG_{PREFIX,SUFFIX} (owned by codex_protocol::models).
const (
	imageOpenTag            = "<image>"
	imageCloseTag           = "</image>"
	localImageOpenTagPrefix = "<image name="
	localImageOpenTagSuffix = ">"
	localImageCloseTag      = imageCloseTag
)

// isImageOpenTagText reports whether text is exactly the inline image open tag.
func isImageOpenTagText(text string) bool { return text == imageOpenTag }

// isImageCloseTagText reports whether text is exactly the inline image close tag.
func isImageCloseTagText(text string) bool { return text == imageCloseTag }

// isLocalImageOpenTagText reports whether text is a local-image open tag
// (`<image name=...>`). Mirrors the Rust `is_local_image_open_tag_text`.
func isLocalImageOpenTagText(text string) bool {
	rest, ok := strings.CutPrefix(text, localImageOpenTagPrefix)
	if !ok {
		return false
	}
	return strings.HasSuffix(rest, localImageOpenTagSuffix)
}

// isLocalImageCloseTagText reports whether text closes a local-image tag.
func isLocalImageCloseTagText(text string) bool { return isImageCloseTagText(text) }

// isContextualUserMessageContent reports whether any fragment in a user message
// is rollback-trimmable contextual scaffolding. Mirrors the Rust
// `is_contextual_user_message_content`.
//
// STUB: the full Rust predicate also recognizes hook-prompt fragments and the
// registered standard contextual fragments (user instructions, environment
// context, skill instructions, etc.) via a fragment registry. Those registries
// are owned by the context/hooks area agents; here we recognize the
// hook-prompt-fragment shape (see [parseHookPromptFragment]) plus the
// contextual-developer prefix set, which covers the turn-running subset.
func isContextualUserMessageContent(content []protocol.ContentItem) bool {
	for _, item := range content {
		if isContextualUserFragment(item) {
			return true
		}
	}
	return false
}

// isContextualUserFragment reports whether a single content item is contextual
// user scaffolding. Mirrors the Rust `is_contextual_user_fragment` (reduced: see
// the STUB note on [isContextualUserMessageContent]).
func isContextualUserFragment(item protocol.ContentItem) bool {
	if item.Type != protocol.ContentItemKindInputText {
		return false
	}
	if _, ok := parseHookPromptFragment(item.Text); ok {
		return true
	}
	return isStandardContextualUserText(item.Text)
}

// isStandardContextualUserText reports whether text matches one of the
// recognized contextual prefixes (case-insensitive prefix match). This is the
// turn-running-subset stand-in for the Rust fragment registry's `matches_text`.
func isStandardContextualUserText(text string) bool {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	for _, prefix := range contextualDeveloperPrefixes {
		if hasPrefixFold(trimmed, prefix) {
			return true
		}
	}
	return false
}

// hasPrefixFold reports whether s begins with prefix under ASCII
// case-insensitive comparison, mirroring the Rust `eq_ignore_ascii_case` prefix
// check used by `is_contextual_dev_fragment`.
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

// parseUserMessage converts a user-role message's content into a
// [protocol.UserMessageItem], skipping the message entirely when it is contextual
// scaffolding. Image marker tags that bracket an inline image are dropped (the
// image itself is preserved). Mirrors the Rust `parse_user_message`.
func parseUserMessage(content []protocol.ContentItem) (protocol.UserMessageItem, bool) {
	if isContextualUserMessageContent(content) {
		return protocol.UserMessageItem{}, false
	}

	out := make([]protocol.UserInput, 0, len(content))
	for idx, item := range content {
		switch item.Type {
		case protocol.ContentItemKindInputText:
			if isImageMarkerToSkip(content, idx) {
				continue
			}
			out = append(out, protocol.UserInput{
				Type: protocol.UserInputKindText,
				Text: item.Text,
				// Model input content carries no UI element ranges.
				TextElements: nil,
			})
		case protocol.ContentItemKindInputImage:
			out = append(out, protocol.UserInput{
				Type:     protocol.UserInputKindImage,
				ImageURL: item.ImageURL,
				Detail:   item.Detail,
			})
		case protocol.ContentItemKindOutputText:
			// Output text in a user message is unexpected; the Rust code warns and
			// drops it. We drop silently (logging is owned by the caller).
		}
	}
	return protocol.UserMessageItem{
		ID:      newItemID(),
		Content: out,
	}, true
}

// isImageMarkerToSkip reports whether the InputText item at idx is an image
// marker tag that brackets an adjacent inline image and should therefore be
// dropped from the user message. Mirrors the Rust open/close bracket guard in
// `parse_user_message`.
func isImageMarkerToSkip(content []protocol.ContentItem, idx int) bool {
	text := content[idx].Text
	openMatches := (isLocalImageOpenTagText(text) || isImageOpenTagText(text)) &&
		idx+1 < len(content) && content[idx+1].Type == protocol.ContentItemKindInputImage
	closeMatches := idx > 0 &&
		(isLocalImageCloseTagText(text) || isImageCloseTagText(text)) &&
		content[idx-1].Type == protocol.ContentItemKindInputImage
	return openMatches || closeMatches
}

// parseAgentMessage converts an assistant-role message into a
// [protocol.AgentMessageItem]. A missing id is filled with a generated id.
// Mirrors the Rust `parse_agent_message`.
func parseAgentMessage(id *string, content []protocol.ContentItem, phase *protocol.MessagePhase) protocol.AgentMessageItem {
	out := make([]protocol.AgentMessageContent, 0, len(content))
	for _, item := range content {
		switch item.Type {
		case protocol.ContentItemKindInputText, protocol.ContentItemKindOutputText:
			out = append(out, protocol.NewAgentMessageText(item.Text))
		default:
			// Unexpected content in an assistant message; the Rust code warns and
			// drops it.
		}
	}
	msgID := ""
	if id != nil {
		msgID = *id
	}
	if msgID == "" {
		msgID = newItemID()
	}
	return protocol.AgentMessageItem{
		ID:      msgID,
		Content: out,
		Phase:   phase,
	}
}

// parseTurnItem maps a streamed [protocol.ResponseItem] to its client-visible
// [protocol.TurnItem], returning (item, true) when a mapping exists. It is a
// faithful port of the Rust `parse_turn_item`.
//
// Mappings:
//   - user message  -> HookPrompt (when it parses as hook fragments) or
//     UserMessage (skipped entirely when contextual scaffolding)
//   - assistant message -> AgentMessage
//   - system message -> none
//   - reasoning      -> Reasoning
//   - web search call -> WebSearch
//   - image generation call -> ImageGeneration
//   - everything else -> none
func parseTurnItem(item protocol.ResponseItem) (protocol.TurnItem, bool) {
	switch item.Type {
	case protocol.ResponseItemKindMessage:
		return parseMessageTurnItem(item)
	case protocol.ResponseItemKindReasoning:
		return parseReasoningTurnItem(item), true
	case protocol.ResponseItemKindWebSearchCall:
		return parseWebSearchTurnItem(item), true
	case protocol.ResponseItemKindImageGenerationCall:
		return protocol.TurnItem{
			Type: protocol.TurnItemKindImageGeneration,
			ImageGeneration: &protocol.ImageGenerationItem{
				ID:            item.ImageID,
				Status:        item.ImageStatus,
				RevisedPrompt: item.RevisedPrompt,
				Result:        item.Result,
			},
		}, true
	default:
		return protocol.TurnItem{}, false
	}
}

// parseMessageTurnItem maps a Message ResponseItem by role.
func parseMessageTurnItem(item protocol.ResponseItem) (protocol.TurnItem, bool) {
	switch item.Role {
	case "user":
		if hookItem, ok := parseVisibleHookPromptMessage(item.MessageID, item.Content); ok {
			return protocol.TurnItem{Type: protocol.TurnItemKindHookPrompt, HookPrompt: &hookItem}, true
		}
		if userItem, ok := parseUserMessage(item.Content); ok {
			return protocol.TurnItem{Type: protocol.TurnItemKindUserMessage, UserMessage: &userItem}, true
		}
		return protocol.TurnItem{}, false
	case "assistant":
		agent := parseAgentMessage(item.MessageID, item.Content, item.Phase)
		return protocol.TurnItem{Type: protocol.TurnItemKindAgentMessage, AgentMessage: &agent}, true
	default:
		// "system" and any other role have no turn-item representation.
		return protocol.TurnItem{}, false
	}
}

// parseReasoningTurnItem maps a Reasoning ResponseItem to a ReasoningTurnItem,
// flattening the summary and raw content into string slices. Mirrors the Rust
// reasoning arm of `parse_turn_item`.
func parseReasoningTurnItem(item protocol.ResponseItem) protocol.TurnItem {
	summary := make([]string, 0, len(item.Summary))
	for _, entry := range item.Summary {
		summary = append(summary, entry.Text)
	}
	var raw []string
	if item.ReasoningContent != nil {
		raw = make([]string, 0, len(*item.ReasoningContent))
		for _, entry := range *item.ReasoningContent {
			raw = append(raw, entry.Text)
		}
	}
	return protocol.TurnItem{
		Type: protocol.TurnItemKindReasoning,
		Reasoning: &protocol.ReasoningTurnItem{
			ID:          item.ReasoningID,
			SummaryText: summary,
			RawContent:  raw,
		},
	}
}

// parseWebSearchTurnItem maps a WebSearchCall ResponseItem to a WebSearchItem,
// computing the human-readable query detail. Mirrors the Rust web-search arm of
// `parse_turn_item`.
func parseWebSearchTurnItem(item protocol.ResponseItem) protocol.TurnItem {
	var (
		action protocol.WebSearchAction
		query  string
	)
	if item.WebAction != nil {
		action = *item.WebAction
		query = webSearchActionDetail(*item.WebAction)
	} else {
		action = protocol.WebSearchAction{Type: protocol.WebSearchActionKindOther}
	}
	id := ""
	if item.WebSearchID != nil {
		id = *item.WebSearchID
	}
	return protocol.TurnItem{
		Type: protocol.TurnItemKindWebSearch,
		WebSearch: &protocol.WebSearchItem{
			ID:     id,
			Query:  query,
			Action: action,
		},
	}
}
