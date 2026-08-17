package remotecompact

import (
	"math"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// KeepUserMessage decides whether a user-role message from a compacted
// transcript should be preserved. The reference resolves this via
// `parse_turn_item`, keeping items that parse as a real UserMessage or a
// HookPrompt while dropping contextual session-prefix wrappers.
//
// Because that classifier lives behind unexported core internals, this package
// accepts it as an injected predicate. Callers (e.g. core) supply the real
// implementation; tests and standalone callers may use [DefaultKeepUserMessage].
type KeepUserMessage func(item protocol.ResponseItem) bool

// DefaultKeepUserMessage is a conservative fallback that keeps every user-role
// message. It is used when no classifier is supplied. It deliberately errs
// toward retention so no genuine user content is lost when the richer
// turn-item classifier is unavailable.
func DefaultKeepUserMessage(_ protocol.ResponseItem) bool { return true }

// ShouldKeepCompactedHistoryItem reports whether an item emitted by remote
// compaction should be preserved in the installed history. It mirrors the Rust
// `should_keep_compacted_history_item`.
//
// The rules are:
//   - developer messages are dropped (remote output can include stale/duplicated
//     instructions);
//   - user messages are kept only when keepUser reports true (real user content
//     and persisted hook prompts), dropping session-prefix wrappers;
//   - assistant messages are kept (future models may emit them);
//   - all other message roles are dropped;
//   - Compaction and ContextCompaction items are kept;
//   - CompactionTrigger and every non-message item kind are dropped.
//
// keepUser may be nil, in which case [DefaultKeepUserMessage] is used.
func ShouldKeepCompactedHistoryItem(item protocol.ResponseItem, keepUser KeepUserMessage) bool {
	if keepUser == nil {
		keepUser = DefaultKeepUserMessage
	}
	switch item.Type {
	case protocol.ResponseItemKindMessage:
		switch item.Role {
		case "developer":
			return false
		case "user":
			return keepUser(item)
		case "assistant":
			return true
		default:
			return false
		}
	case protocol.ResponseItemKindCompaction, protocol.ResponseItemKindContextCompaction:
		return true
	default:
		// CompactionTrigger plus reasoning, shell/function/tool calls and outputs,
		// web/image calls, and the Other catch-all are all dropped.
		return false
	}
}

// UserMessageText extracts the model-visible text from a user-role message, used
// by injected summary classifiers. It is nil-safe and concatenates all
// input/output text fragments. Non-message items yield the empty string.
//
// This is a convenience for callers wiring up [InsertInitialContextBeforeLastRealUserOrSummary];
// the summary check itself is supplied by the caller.
func UserMessageText(item protocol.ResponseItem) string {
	if item.Type != protocol.ResponseItemKindMessage {
		return ""
	}
	text := ""
	for _, c := range item.Content {
		switch c.Type {
		case protocol.ContentItemKindInputText, protocol.ContentItemKindOutputText:
			text += c.Text
		}
	}
	return text
}

// IsSummaryMessage reports whether a user-message text is a compaction summary.
// It is the pluggable counterpart to the Rust `is_summary_message`; callers
// supply the concrete predicate so this package stays decoupled from the
// summary-marker definition owned by core.
type IsSummaryMessage func(message string) bool

// InsertInitialContextBeforeLastRealUserOrSummary re-injects canonical context
// into a compacted transcript, preferring the position before the last real user
// message. If there is no real user message it falls back to before the last
// user-like/summary item, then to before the last compaction item, and finally
// appends the context. It mirrors the Rust
// `insert_initial_context_before_last_real_user_or_summary`.
//
// The classifiers are injected:
//   - keepUser identifies user-message-like items (the Rust `parse_turn_item ==
//     UserMessage` test); when nil, [DefaultKeepUserMessage] is used.
//   - isSummary identifies compaction-summary user messages; when nil, no
//     message is treated as a summary, so every kept user message is "real".
//
// The input slice is not mutated; a new slice is returned.
func InsertInitialContextBeforeLastRealUserOrSummary(
	compactedHistory []protocol.ResponseItem,
	initialContext []protocol.ResponseItem,
	keepUser KeepUserMessage,
	isSummary IsSummaryMessage,
) []protocol.ResponseItem {
	if keepUser == nil {
		keepUser = DefaultKeepUserMessage
	}

	lastUserOrSummaryIndex := -1
	lastRealUserIndex := -1
	for i := len(compactedHistory) - 1; i >= 0; i-- {
		item := compactedHistory[i]
		if item.Type != protocol.ResponseItemKindMessage || item.Role != "user" {
			continue
		}
		if !keepUser(item) {
			continue
		}
		// Compaction summaries are encoded as user messages, so track both the
		// last real user message (preferred insertion point) and the last
		// user-message-like item (fallback summary insertion point).
		if lastUserOrSummaryIndex == -1 {
			lastUserOrSummaryIndex = i
		}
		if isSummary == nil || !isSummary(UserMessageText(item)) {
			lastRealUserIndex = i
			break
		}
	}

	lastCompactionIndex := -1
	for i := len(compactedHistory) - 1; i >= 0; i-- {
		switch compactedHistory[i].Type {
		case protocol.ResponseItemKindCompaction, protocol.ResponseItemKindContextCompaction:
			lastCompactionIndex = i
		}
		if lastCompactionIndex != -1 {
			break
		}
	}

	insertionIndex := firstNonNegative(lastRealUserIndex, lastUserOrSummaryIndex, lastCompactionIndex)
	return spliceAt(compactedHistory, initialContext, insertionIndex)
}

// ProcessCompactedHistory filters a model-provided compacted transcript with
// [ShouldKeepCompactedHistoryItem] and then re-injects the supplied initial
// context. It mirrors the Rust `process_compacted_history`, except that the
// session-built initial context is supplied by the caller (the reference builds
// it from a live Session/TurnContext, which is out of this package's scope).
//
// When initialContext is empty, no injection occurs. The input slice is not
// mutated; a new slice is returned.
func ProcessCompactedHistory(
	compactedHistory []protocol.ResponseItem,
	initialContext []protocol.ResponseItem,
	keepUser KeepUserMessage,
	isSummary IsSummaryMessage,
) []protocol.ResponseItem {
	retained := make([]protocol.ResponseItem, 0, len(compactedHistory))
	for _, item := range compactedHistory {
		if ShouldKeepCompactedHistoryItem(item, keepUser) {
			retained = append(retained, item)
		}
	}
	return InsertInitialContextBeforeLastRealUserOrSummary(retained, initialContext, keepUser, isSummary)
}

// CompactRequestLogData holds the diagnostic figure logged when a remote
// compaction request fails. It mirrors the Rust `CompactRequestLogData`.
type CompactRequestLogData struct {
	// FailingCompactionRequestModelVisibleBytes is the approximate model-visible
	// byte size of the failing request (instructions plus every input item).
	FailingCompactionRequestModelVisibleBytes int64
}

// EstimateModelVisibleBytes estimates the model-visible byte size of a single
// response item. It is the pluggable counterpart to the Rust
// `estimate_response_item_model_visible_bytes`; callers supply the concrete
// estimator owned by core's context manager.
type EstimateModelVisibleBytes func(item protocol.ResponseItem) int64

// BuildCompactRequestLogData computes the failing-request size by folding the
// estimated model-visible bytes of every input item onto the instruction
// length, using saturating addition. It mirrors the Rust
// `build_compact_request_log_data`.
//
// estimate may be nil, in which case input items contribute zero bytes and only
// the instruction length is counted.
func BuildCompactRequestLogData(input []protocol.ResponseItem, instructions string, estimate EstimateModelVisibleBytes) CompactRequestLogData {
	// The Rust seeds the fold with i64::try_from(instructions.len()).unwrap_or(i64::MAX);
	// a Go string length always fits in int64, so the conversion is exact.
	total := int64(len(instructions))
	for _, item := range input {
		var bytes int64
		if estimate != nil {
			bytes = estimate(item)
		}
		total = saturatingAddInt64(total, bytes)
	}
	return CompactRequestLogData{FailingCompactionRequestModelVisibleBytes: total}
}

// IsCodexGeneratedItem reports whether the trailing item of a transcript was
// generated by codex (and is therefore safe to trim). It is the pluggable
// counterpart to the Rust `is_codex_generated_item`.
type IsCodexGeneratedItem func(item protocol.ResponseItem) bool

// EstimateTokenCountWithBaseInstructions estimates the token count of a
// transcript including its base instructions, returning (count, true) when an
// estimate is available. It is the pluggable counterpart to the Rust
// `ContextManager::estimate_token_count_with_base_instructions`.
type EstimateTokenCountWithBaseInstructions func(history []protocol.ResponseItem) (int64, bool)

// TrimFunctionCallHistoryToFitContextWindow drops codex-generated trailing items
// from history until the estimated token count fits within contextWindow, and
// returns the trimmed history together with the number of items removed. It
// mirrors the Rust `trim_function_call_history_to_fit_context_window`.
//
// Trimming stops as soon as the trailing item is not codex-generated, the
// estimate is unavailable, or history is empty. When contextWindow is nil (no
// known window), nothing is trimmed. The input slice is not mutated; a new
// slice is returned alongside the deleted count.
func TrimFunctionCallHistoryToFitContextWindow(
	history []protocol.ResponseItem,
	contextWindow *int64,
	estimateTokens EstimateTokenCountWithBaseInstructions,
	isCodexGenerated IsCodexGeneratedItem,
) ([]protocol.ResponseItem, int) {
	out := append([]protocol.ResponseItem(nil), history...)
	deleted := 0
	if contextWindow == nil {
		return out, deleted
	}
	window := *contextWindow

	for {
		if estimateTokens == nil {
			break
		}
		estimated, ok := estimateTokens(out)
		if !ok || estimated <= window {
			break
		}
		if len(out) == 0 {
			break
		}
		last := out[len(out)-1]
		if isCodexGenerated == nil || !isCodexGenerated(last) {
			break
		}
		out = out[:len(out)-1]
		deleted++
	}
	return out, deleted
}

// firstNonNegative returns the first non-negative argument, or -1 if all are
// negative. It models the Rust Option::or chain over indices.
func firstNonNegative(values ...int) int {
	for _, v := range values {
		if v >= 0 {
			return v
		}
	}
	return -1
}

// spliceAt returns a new slice with extra inserted at index. When index is
// negative, extra is appended. The base slice is never mutated.
func spliceAt(base, extra []protocol.ResponseItem, index int) []protocol.ResponseItem {
	if len(extra) == 0 {
		return append([]protocol.ResponseItem(nil), base...)
	}
	if index < 0 || index > len(base) {
		out := make([]protocol.ResponseItem, 0, len(base)+len(extra))
		out = append(out, base...)
		out = append(out, extra...)
		return out
	}
	out := make([]protocol.ResponseItem, 0, len(base)+len(extra))
	out = append(out, base[:index]...)
	out = append(out, extra...)
	out = append(out, base[index:]...)
	return out
}

// saturatingAddInt64 returns a + b clamped to the int64 range, matching the Rust
// i64::saturating_add.
func saturatingAddInt64(a, b int64) int64 {
	sum := a + b
	if b > 0 && sum < a {
		return math.MaxInt64
	}
	if b < 0 && sum > a {
		return math.MinInt64
	}
	return sum
}
