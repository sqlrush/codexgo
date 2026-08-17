package websearch

import (
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/truncation"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// assistantContextTokenLimit caps assistant output text retained in the search
// tail. Rust: ASSISTANT_CONTEXT_TOKEN_LIMIT.
const assistantContextTokenLimit = 1000

const (
	assistantRole = "assistant"
	userRole      = "user"
)

// contextualUserOpenTags are the leading tags that mark a user message as
// session-context rather than a real user turn. The codex core machinery
// (parse_turn_item -> parse_user_message -> is_contextual_user_fragment) treats
// such messages as non-UserMessage; standalone web search excludes them from the
// search tail. These are the standard contextual user fragment open tags.
var contextualUserOpenTags = []string{
	"<environment_context>",
	"<user_instructions>",
	"<additional_context>",
	"<skill_instructions>",
	"<user_shell_command>",
	"<turn_aborted>",
	"<subagent_notification>",
	"<internal_model_context>",
	"<hook_prompt>",
}

// RecentInput builds the conversation tail for standalone web search. It keeps
// the previous real user text message, up to 1k tokens of assistant text that
// followed it, and the current real user text message. Returns nil when the tail
// is empty. Rust: recent_input.
func RecentInput(items []protocol.ResponseItem) *SearchInput {
	return RecentInputWith(items, IsRealUserMessage)
}

// RealUserMessagePredicate selects which user messages count as real user turns
// (not contextual fragments). Rust: parse_turn_item(...) is
// Some(TurnItem::UserMessage(_)).
type RealUserMessagePredicate func(item protocol.ResponseItem) bool

// RecentInputWith is RecentInput with an injectable real-user-message predicate,
// allowing callers wired into the full codex_core turn-item parser to supply the
// exact contextual-fragment classification.
func RecentInputWith(items []protocol.ResponseItem, isRealUser RealUserMessagePredicate) *SearchInput {
	var messages []protocol.ResponseItem
	for _, item := range items {
		pushVisibleMessage(&messages, item, isRealUser)
	}

	retainTailFromLastNUserMessages(&messages, 2)
	truncateAssistantOutputTextToTokenBudget(&messages, assistantContextTokenLimit)
	if len(messages) == 0 {
		return nil
	}
	input := ItemsSearchInput(messages)
	return &input
}

// pushVisibleMessage appends an assistant message verbatim, or a text-only copy
// of a real user message, mirroring Rust push_visible_message.
func pushVisibleMessage(messages *[]protocol.ResponseItem, item protocol.ResponseItem, isRealUser RealUserMessagePredicate) {
	if item.Type != protocol.ResponseItemKindMessage {
		return
	}
	switch item.Role {
	case assistantRole:
		*messages = append(*messages, item)
	case userRole:
		if !isRealUser(item) {
			return
		}
		var content []protocol.ContentItem
		for _, c := range item.Content {
			if c.Type == protocol.ContentItemKindInputText {
				content = append(content, c)
			}
		}
		if len(content) == 0 {
			return
		}
		*messages = append(*messages, protocol.ResponseItem{
			Type:      protocol.ResponseItemKindMessage,
			MessageID: item.MessageID,
			Role:      item.Role,
			Content:   content,
			Phase:     item.Phase,
		})
	}
}

// IsRealUserMessage reports whether a user message is a real user turn rather
// than a contextual fragment. It recognizes the standard contextual user
// fragment open tags. Rust equivalent: parse_user_message returns Some only when
// is_contextual_user_message_content is false.
func IsRealUserMessage(item protocol.ResponseItem) bool {
	if item.Type != protocol.ResponseItemKindMessage || item.Role != userRole {
		return false
	}
	for _, c := range item.Content {
		if c.Type == protocol.ContentItemKindInputText && isContextualUserText(c.Text) {
			return false
		}
	}
	return true
}

// isContextualUserText reports whether the text begins (ASCII-case-insensitively,
// after leading whitespace) with a contextual user fragment open tag. Mirrors
// the prefix matching done by the codex contextual fragment registrations.
func isContextualUserText(text string) bool {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	for _, tag := range contextualUserOpenTags {
		if len(trimmed) >= len(tag) && strings.EqualFold(trimmed[:len(tag)], tag) {
			return true
		}
	}
	return false
}

// retainTailFromLastNUserMessages retains items from the earliest of the last
// userMessageCount user messages through the latest user message. Rust:
// retain_tail_from_last_n_user_messages.
func retainTailFromLastNUserMessages(items *[]protocol.ResponseItem, userMessageCount int) {
	if userMessageCount == 0 {
		*items = nil
		return
	}

	latestUserIdx := -1
	for i := len(*items) - 1; i >= 0; i-- {
		if (*items)[i].IsUserMessage() {
			latestUserIdx = i
			break
		}
	}
	if latestUserIdx < 0 {
		*items = nil
		return
	}
	*items = (*items)[:latestUserIdx+1]

	// Earliest index among the last userMessageCount user messages.
	seen := 0
	earliestRetainedUserIdx := latestUserIdx
	for i := len(*items) - 1; i >= 0; i-- {
		if (*items)[i].IsUserMessage() {
			seen++
			earliestRetainedUserIdx = i
			if seen == userMessageCount {
				break
			}
		}
	}
	*items = (*items)[earliestRetainedUserIdx:]
}

// truncateAssistantOutputTextToTokenBudget trims assistant output text across
// items to a shared token budget. Rust:
// truncate_assistant_output_text_to_token_budget.
func truncateAssistantOutputTextToTokenBudget(items *[]protocol.ResponseItem, maxTokens int) {
	remainingBudget := maxTokens
	out := (*items)[:0]
	for _, item := range *items {
		if item.Type != protocol.ResponseItemKindMessage || item.Role != assistantRole {
			out = append(out, item)
			continue
		}
		content := item.Content[:0]
		for _, contentItem := range item.Content {
			if contentItem.Type != protocol.ContentItemKindOutputText {
				content = append(content, contentItem)
				continue
			}
			if remainingBudget == 0 {
				continue
			}
			tokenCount := truncation.ApproxTokenCount(contentItem.Text)
			if tokenCount <= remainingBudget {
				remainingBudget = saturatingSubInt(remainingBudget, tokenCount)
				content = append(content, contentItem)
				continue
			}
			contentItem.Text = truncation.TruncateText(contentItem.Text, truncation.TokensPolicy(remainingBudget))
			remainingBudget = 0
			content = append(content, contentItem)
		}
		item.Content = content
		if len(item.Content) == 0 {
			continue
		}
		out = append(out, item)
	}
	*items = out
}

func saturatingSubInt(a, b int) int {
	if a < b {
		return 0
	}
	return a - b
}
