package truncation

import (
	"fmt"
	"unicode/utf8"
)

// TruncateMiddleChars truncates s to at most maxBytes bytes, preserving the
// beginning and the end and inserting a character-count marker in the middle.
//
// This mirrors codex_utils_string::truncate_middle_chars.
func TruncateMiddleChars(s string, maxBytes int) string {
	return truncateWithByteEstimate(s, maxBytes, false)
}

// TruncateMiddleWithTokenBudget truncates the middle of a UTF-8 string to at
// most maxTokens approximate tokens, preserving the beginning and the end. It
// returns the possibly truncated string and, if truncation occurred, the
// original token count via the second return value (ok == true). When no
// truncation occurred, it returns the original string with ok == false.
//
// This mirrors codex_utils_string::truncate_middle_with_token_budget, whose
// Rust signature returns (String, Option<u64>).
func TruncateMiddleWithTokenBudget(s string, maxTokens int) (result string, originalTokens uint64, ok bool) {
	if len(s) == 0 {
		return "", 0, false
	}

	if maxTokens > 0 && len(s) <= ApproxBytesForTokens(maxTokens) {
		return s, 0, false
	}

	truncated := truncateWithByteEstimate(s, ApproxBytesForTokens(maxTokens), true)
	totalTokens := uint64(ApproxTokenCount(s))

	if truncated == s {
		return truncated, 0, false
	}
	return truncated, totalTokens, true
}

// truncateWithByteEstimate is the shared core for byte- and token-based
// middle truncation. When useTokens is true the marker is rendered in tokens
// and the removed count is approximated from removed bytes; otherwise the
// marker is rendered in chars using the removed character count.
//
// This mirrors the private truncate_with_byte_estimate in truncate.rs.
func truncateWithByteEstimate(s string, maxBytes int, useTokens bool) string {
	if len(s) == 0 {
		return ""
	}

	totalChars := utf8.RuneCountInString(s)

	if maxBytes == 0 {
		return formatTruncationMarker(useTokens, removedUnits(useTokens, len(s), totalChars))
	}

	if len(s) <= maxBytes {
		return s
	}

	totalBytes := len(s)
	leftBudget, rightBudget := splitBudget(maxBytes)
	removedChars, left, right := splitString(s, leftBudget, rightBudget)
	marker := formatTruncationMarker(
		useTokens,
		removedUnits(useTokens, saturatingSubInt(totalBytes, maxBytes), removedChars),
	)

	return assembleTruncatedOutput(left, right, marker)
}

// splitString splits s into a prefix bounded by beginningBytes and a suffix
// bounded by endBytes, both respecting UTF-8 character boundaries. It returns
// the number of characters removed from the middle along with the prefix and
// suffix substrings.
//
// This mirrors the private split_string in truncate.rs, including its exact
// boundary handling so that output matches byte-for-byte.
func splitString(s string, beginningBytes, endBytes int) (removedChars int, before, after string) {
	if len(s) == 0 {
		return 0, "", ""
	}

	length := len(s)
	tailStartTarget := saturatingSubInt(length, endBytes)
	prefixEnd := 0
	suffixStart := length
	removed := 0
	suffixStarted := false

	for idx := 0; idx < length; {
		_, size := utf8.DecodeRuneInString(s[idx:])
		charEnd := idx + size

		if charEnd <= beginningBytes {
			prefixEnd = charEnd
			idx = charEnd
			continue
		}

		if idx >= tailStartTarget {
			if !suffixStarted {
				suffixStart = idx
				suffixStarted = true
			}
			idx = charEnd
			continue
		}

		removed++
		idx = charEnd
	}

	if suffixStart < prefixEnd {
		suffixStart = prefixEnd
	}

	return removed, s[:prefixEnd], s[suffixStart:]
}

// splitBudget divides a byte budget into a left and right half, giving any odd
// extra byte to the right side. This mirrors split_budget in truncate.rs.
func splitBudget(budget int) (left, right int) {
	left = budget / 2
	return left, budget - left
}

// formatTruncationMarker renders the inline truncation marker for the given
// removed count, in tokens or chars. This mirrors format_truncation_marker.
func formatTruncationMarker(useTokens bool, removedCount uint64) string {
	if useTokens {
		return fmt.Sprintf("…%d tokens truncated…", removedCount)
	}
	return fmt.Sprintf("…%d chars truncated…", removedCount)
}

// removedUnits returns the count rendered in the marker: an approximate token
// count derived from removedBytes when useTokens is true, otherwise the raw
// removed character count. This mirrors removed_units in truncate.rs.
func removedUnits(useTokens bool, removedBytes, removedChars int) uint64 {
	if useTokens {
		if removedBytes < 0 {
			removedBytes = 0
		}
		return ApproxTokensFromByteCount(uint64(removedBytes))
	}
	if removedChars < 0 {
		return 0
	}
	return uint64(removedChars)
}

// assembleTruncatedOutput concatenates prefix, marker, and suffix. This mirrors
// assemble_truncated_output in truncate.rs.
func assembleTruncatedOutput(prefix, suffix, marker string) string {
	return prefix + marker + suffix
}

// saturatingSubInt returns max(a-b, 0) for the difference, matching the
// saturating_sub usage in the Rust implementation.
func saturatingSubInt(a, b int) int {
	if a < b {
		return 0
	}
	return a - b
}
