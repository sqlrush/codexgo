package truncation

import (
	"fmt"
	"strings"
)

// FormattedTruncateText truncates content according to policy and, when
// truncation occurs, prefixes the result with a header reporting the original
// total line count. If content already fits within the policy's byte budget it
// is returned unchanged.
//
// This mirrors codex-utils-output-truncation::formatted_truncate_text.
func FormattedTruncateText(content string, policy TruncationPolicy) string {
	if len(content) <= policy.ByteBudget() {
		return content
	}

	totalLines := countLines(content)
	result := TruncateText(content, policy)
	return fmt.Sprintf("Total output lines: %d\n\n%s", totalLines, result)
}

// TruncateText truncates content according to policy without any header.
//
// This mirrors codex-utils-output-truncation::truncate_text.
func TruncateText(content string, policy TruncationPolicy) string {
	switch policy.Mode() {
	case ModeBytes:
		return TruncateMiddleChars(content, policy.Limit())
	default: // ModeTokens
		result, _, _ := TruncateMiddleWithTokenBudget(content, policy.Limit())
		return result
	}
}

// FormattedTruncateTextContentItemsWithPolicy combines all input_text segments
// from items, and if their combined length exceeds the policy's byte budget,
// replaces them with a single formatted, truncated text item followed by any
// image and encrypted-content items (in their original relative order). It
// returns the resulting items and, when truncation occurred, the approximate
// original token count of the combined text via ok == true.
//
// When there is no text to truncate, or the combined text fits within budget, a
// deep copy of the original items is returned with ok == false.
//
// This mirrors
// codex-utils-output-truncation::formatted_truncate_text_content_items_with_policy,
// whose Rust signature returns (Vec<...>, Option<usize>).
func FormattedTruncateTextContentItemsWithPolicy(
	items []FunctionCallOutputContentItem,
	policy TruncationPolicy,
) (out []FunctionCallOutputContentItem, originalTokens int, ok bool) {
	hasText := false
	for _, item := range items {
		if item.Kind == KindInputText {
			hasText = true
			break
		}
	}

	if !hasText {
		return cloneItems(items), 0, false
	}

	// Mirror the Rust accumulation exactly: a '\n' separator is only inserted
	// once the accumulator is non-empty. Because leading empty segments leave
	// the accumulator empty, they contribute neither content nor a separator.
	var builder strings.Builder
	for _, item := range items {
		if item.Kind != KindInputText {
			continue
		}
		if builder.Len() != 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(item.Text)
	}
	combined := builder.String()

	if len(combined) <= policy.ByteBudget() {
		return cloneItems(items), 0, false
	}

	result := make([]FunctionCallOutputContentItem, 0, len(items))
	result = append(result, NewInputText(FormattedTruncateText(combined, policy)))
	for _, item := range items {
		switch item.Kind {
		case KindInputImage:
			result = append(result, NewInputImage(item.ImageURL, item.Detail))
		case KindEncryptedContent:
			result = append(result, NewEncryptedContent(item.EncryptedContent))
		}
	}

	return result, ApproxTokenCount(combined), true
}

// TruncateFunctionOutputItemsWithPolicy walks items in order, copying image and
// encrypted-content items verbatim and spending a budget (bytes or tokens
// depending on policy) across input_text items. Text items that fit are kept
// whole; the first item that overflows is truncated to fit the remaining
// budget; any further text items are omitted. When any text item is omitted, a
// trailing summary text item is appended reporting the count.
//
// This mirrors
// codex-utils-output-truncation::truncate_function_output_items_with_policy.
func TruncateFunctionOutputItemsWithPolicy(
	items []FunctionCallOutputContentItem,
	policy TruncationPolicy,
) []FunctionCallOutputContentItem {
	out := make([]FunctionCallOutputContentItem, 0, len(items))

	var remainingBudget int
	switch policy.Mode() {
	case ModeBytes:
		remainingBudget = policy.ByteBudget()
	default: // ModeTokens
		remainingBudget = policy.TokenBudget()
	}

	omittedTextItems := 0

	for _, item := range items {
		switch item.Kind {
		case KindInputText:
			text := item.Text
			if remainingBudget == 0 {
				omittedTextItems++
				continue
			}

			var cost int
			switch policy.Mode() {
			case ModeBytes:
				cost = len(text)
			default: // ModeTokens
				cost = ApproxTokenCount(text)
			}

			if cost <= remainingBudget {
				out = append(out, NewInputText(text))
				remainingBudget = saturatingSubInt(remainingBudget, cost)
			} else {
				var snippetPolicy TruncationPolicy
				switch policy.Mode() {
				case ModeBytes:
					snippetPolicy = BytesPolicy(remainingBudget)
				default: // ModeTokens
					snippetPolicy = TokensPolicy(remainingBudget)
				}
				snippet := TruncateText(text, snippetPolicy)
				if snippet == "" {
					omittedTextItems++
				} else {
					out = append(out, NewInputText(snippet))
				}
				remainingBudget = 0
			}
		case KindInputImage:
			out = append(out, NewInputImage(item.ImageURL, item.Detail))
		case KindEncryptedContent:
			out = append(out, NewEncryptedContent(item.EncryptedContent))
		}
	}

	if omittedTextItems > 0 {
		out = append(out, NewInputText(fmt.Sprintf("[omitted %d text items ...]", omittedTextItems)))
	}

	return out
}

// countLines counts lines the way Rust's str::lines does: it splits on '\n'
// (stripping a trailing '\r'), and a trailing newline does not produce an empty
// final line. An empty string has zero lines.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	count := 0
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			count++
			start = i + 1
		}
	}
	// Count a final line only if there is content after the last newline.
	if start < len(s) {
		count++
	}
	return count
}
