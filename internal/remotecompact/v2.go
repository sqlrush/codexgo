package remotecompact

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/utils/truncation"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// RetainedMessageTokenBudget mirrors the current "/responses/compact"
// retained-message default while the server-side path remains the reference
// implementation. It bounds the total tokens of retained prefix messages in v2
// compaction. It mirrors the Rust `RETAINED_MESSAGE_TOKEN_BUDGET`.
const RetainedMessageTokenBudget = 64_000

// MaxRemoteCompactionV2StreamRetries caps the per-transport retry budget for v2
// compaction streams. Compaction attempts can run much longer than normal turns,
// so this is kept smaller than the general Responses stream retry budget. It
// mirrors the Rust `MAX_REMOTE_COMPACTION_V2_STREAM_RETRIES`.
const MaxRemoteCompactionV2StreamRetries uint64 = 2

// CompactionOutput is the result of collecting a v2 compaction stream: the
// single compaction output item and the completing response id.
type CompactionOutput struct {
	// Item is the compaction ResponseItem the model emitted.
	Item protocol.ResponseItem
	// ResponseID is the id from the terminal response.completed event.
	ResponseID string
}

// CollectCompactionOutput drains a Responses stream and extracts the single
// compaction output item plus the completing response id. It mirrors the Rust
// `collect_compaction_output`.
//
// It tolerates additional output items (e.g. an assistant reply) but requires
// exactly one Compaction output item and a terminal response.completed event.
// The first error event encountered terminates collection. The context cancels
// the drain.
func CollectCompactionOutput(ctx context.Context, stream api.ResponseStream) (CompactionOutput, error) {
	outputItemCount := 0
	compactionCount := 0
	var compactionOutput *protocol.ResponseItem
	completedResponseID := ""
	completed := false

drain:
	for {
		select {
		case <-ctx.Done():
			return CompactionOutput{}, ctx.Err()
		case result, ok := <-stream.Events:
			if !ok {
				// Channel closed without a response.completed event.
				return CompactionOutput{}, streamClosedError()
			}
			if result.Err != nil {
				return CompactionOutput{}, result.Err
			}
			if result.Event == nil {
				continue
			}
			switch result.Event.Kind {
			case api.ResponseEventOutputItemDone:
				if result.Event.Item == nil {
					continue
				}
				outputItemCount++
				if result.Event.Item.Type == protocol.ResponseItemKindCompaction {
					compactionCount++
					if compactionOutput == nil {
						item := *result.Event.Item
						compactionOutput = &item
					}
				}
			case api.ResponseEventCompleted:
				completedResponseID = result.Event.ResponseID
				completed = true
				break drain
			}
		}
	}

	if !completed {
		return CompactionOutput{}, streamClosedError()
	}
	if compactionCount != 1 {
		return CompactionOutput{}, fmt.Errorf(
			"remote compaction v2 expected exactly one compaction output item, got %d from %d output items",
			compactionCount, outputItemCount,
		)
	}
	// compactionOutput is guaranteed non-nil when compactionCount == 1.
	return CompactionOutput{Item: *compactionOutput, ResponseID: completedResponseID}, nil
}

// streamClosedError reports a stream that ended before response.completed,
// matching the Rust CodexErr::Stream message used by collect_compaction_output.
func streamClosedError() error {
	return fmt.Errorf("remote compaction v2 stream closed before response.completed")
}

// BuildV2CompactedHistory rebuilds the installed history for v2 compaction from
// the original prompt input and the model's compaction output item. It mirrors
// the Rust `build_v2_compacted_history`.
//
// It retains the prefix user/developer/system messages that also pass
// [ShouldKeepCompactedHistoryItem], truncates them to the retained-message token
// budget (newest first), and appends the compaction output. keepUser is the same
// pluggable classifier used by the shared history helpers and may be nil.
//
// The input slice is not mutated; a new slice is returned.
func BuildV2CompactedHistory(promptInput []protocol.ResponseItem, compactionOutput protocol.ResponseItem, keepUser KeepUserMessage) []protocol.ResponseItem {
	retained := make([]protocol.ResponseItem, 0, len(promptInput))
	for _, item := range promptInput {
		if !isRetainedForRemoteCompactionV2(item) {
			continue
		}
		if !ShouldKeepCompactedHistoryItem(item, keepUser) {
			continue
		}
		retained = append(retained, item)
	}
	retained = TruncateRetainedMessagesForRemoteCompaction(retained, RetainedMessageTokenBudget)
	return append(retained, compactionOutput)
}

// isRetainedForRemoteCompactionV2 reports whether a prompt item is eligible to
// be retained as a prefix message in v2 compaction: only user/developer/system
// role messages qualify. It mirrors the Rust `is_retained_for_remote_compaction_v2`.
func isRetainedForRemoteCompactionV2(item protocol.ResponseItem) bool {
	if item.Type != protocol.ResponseItemKindMessage {
		return false
	}
	switch item.Role {
	case "user", "developer", "system":
		return true
	default:
		return false
	}
}

// TruncateRetainedMessagesForRemoteCompaction keeps the newest retained messages
// within maxTokens, truncating the boundary message's text if needed and
// dropping older messages once the budget is spent. It mirrors the Rust
// `truncate_retained_messages_for_remote_compaction`.
//
// Each kept message is charged at least one token (so image-only messages still
// consume budget). The relative order of the surviving messages is preserved.
// The input slice is not mutated; a new slice is returned.
func TruncateRetainedMessagesForRemoteCompaction(items []protocol.ResponseItem, maxTokens int) []protocol.ResponseItem {
	remaining := maxTokens
	truncatedReversed := make([]protocol.ResponseItem, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		if remaining == 0 {
			continue
		}
		item := items[i]
		tokenCount := messageTextTokenCount(item)
		if tokenCount < 1 {
			tokenCount = 1
		}
		if tokenCount <= remaining {
			truncatedReversed = append(truncatedReversed, item)
			remaining = saturatingSubInt(remaining, tokenCount)
		} else if truncated, ok := truncateMessageTextToTokenBudget(item, remaining); ok {
			truncatedReversed = append(truncatedReversed, truncated)
			remaining = 0
		}
	}
	// Reverse back into original order.
	out := make([]protocol.ResponseItem, len(truncatedReversed))
	for i, item := range truncatedReversed {
		out[len(truncatedReversed)-1-i] = item
	}
	return out
}

// messageTextTokenCount returns the approximate token count of a message's text
// content (input/output text); images contribute zero. Non-message items return
// zero. It mirrors the Rust `message_text_token_count`.
func messageTextTokenCount(item protocol.ResponseItem) int {
	if item.Type != protocol.ResponseItemKindMessage {
		return 0
	}
	total := 0
	for _, c := range item.Content {
		switch c.Type {
		case protocol.ContentItemKindInputText, protocol.ContentItemKindOutputText:
			total += truncation.ApproxTokenCount(c.Text)
		case protocol.ContentItemKindInputImage:
			// Images contribute zero text tokens.
		}
	}
	return total
}

// truncateMessageTextToTokenBudget truncates a message's text content to fit
// maxTokens, preserving images and dropping text parts once the budget is spent.
// It returns (item, false) when nothing survives. Non-message items pass through
// unchanged. It mirrors the Rust `truncate_message_text_to_token_budget`.
//
// The input item is not mutated; a new item with new content is returned.
func truncateMessageTextToTokenBudget(item protocol.ResponseItem, maxTokens int) (protocol.ResponseItem, bool) {
	if item.Type != protocol.ResponseItemKindMessage {
		return item, true
	}

	remaining := maxTokens
	truncatedContent := make([]protocol.ContentItem, 0, len(item.Content))
	for _, content := range item.Content {
		switch content.Type {
		case protocol.ContentItemKindInputText, protocol.ContentItemKindOutputText:
			if remaining == 0 {
				continue
			}
			text := content.Text
			tokenCount := truncation.ApproxTokenCount(text)
			if tokenCount <= remaining {
				remaining = saturatingSubInt(remaining, tokenCount)
			} else {
				text = truncation.TruncateText(text, truncation.TokensPolicy(remaining))
				remaining = 0
			}
			if text != "" {
				truncatedContent = append(truncatedContent, protocol.ContentItem{
					Type: content.Type,
					Text: text,
				})
			}
		case protocol.ContentItemKindInputImage:
			truncatedContent = append(truncatedContent, content)
		}
	}

	if len(truncatedContent) == 0 {
		return protocol.ResponseItem{}, false
	}

	out := protocol.ResponseItem{
		Type:      protocol.ResponseItemKindMessage,
		MessageID: item.MessageID,
		Role:      item.Role,
		Content:   truncatedContent,
		Phase:     item.Phase,
	}
	return out, true
}

// saturatingSubInt returns max(a-b, 0), matching the saturating_sub usage in the
// Rust truncation loop.
func saturatingSubInt(a, b int) int {
	if a < b {
		return 0
	}
	return a - b
}
