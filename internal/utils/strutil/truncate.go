// Truncation helpers for shrinking large chunks of output while preserving a
// prefix and a suffix on UTF-8 boundaries. These mirror the behavior of the
// reference crate's truncate module, including the exact truncation markers.

package strutil

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// approxBytesPerToken is the rough bytes-per-token ratio used by the token
// estimators below.
const approxBytesPerToken = 4

// TruncateMiddleChars truncates s to at most maxBytes bytes, preserving the
// beginning and the end and inserting a "…N chars truncated…" marker in the
// middle. If s already fits within maxBytes, it is returned unchanged.
func TruncateMiddleChars(s string, maxBytes int) string {
	return truncateWithByteEstimate(s, maxBytes, false /* useTokens */)
}

// TruncateMiddleWithTokenBudget truncates the middle of the UTF-8 string s to
// at most maxTokens approximate tokens, preserving the beginning and the end.
//
// It returns the (possibly truncated) string and the original approximate token
// count. The boolean reports whether truncation occurred: when false, the
// returned string equals s and the token count should be ignored (it is 0),
// mirroring the Rust Option<u64> return.
func TruncateMiddleWithTokenBudget(s string, maxTokens int) (string, uint64, bool) {
	if s == "" {
		return "", 0, false
	}

	if maxTokens > 0 && len(s) <= ApproxBytesForTokens(maxTokens) {
		return s, 0, false
	}

	truncated := truncateWithByteEstimate(s, ApproxBytesForTokens(maxTokens), true /* useTokens */)
	totalTokens := uint64(ApproxTokenCount(s))

	if truncated == s {
		return truncated, 0, false
	}
	return truncated, totalTokens, true
}

// truncateWithByteEstimate is the shared engine behind the public truncate
// functions. When useTokens is true the marker counts tokens; otherwise it
// counts characters.
func truncateWithByteEstimate(s string, maxBytes int, useTokens bool) string {
	if s == "" {
		return ""
	}

	totalChars := utf8.RuneCountInString(s)

	if maxBytes <= 0 {
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
		removedUnits(useTokens, saturatingSub(totalBytes, maxBytes), removedChars),
	)

	return assembleTruncatedOutput(left, right, marker)
}

// ApproxTokenCount returns the approximate number of tokens in text using a
// fixed bytes-per-token ratio (rounding up).
func ApproxTokenCount(text string) int {
	length := len(text)
	return (length + (approxBytesPerToken - 1)) / approxBytesPerToken
}

// ApproxBytesForTokens returns the approximate number of bytes occupied by the
// given number of tokens. The result saturates at math.MaxInt on overflow.
func ApproxBytesForTokens(tokens int) int {
	return saturatingMul(tokens, approxBytesPerToken)
}

// ApproxTokensFromByteCount returns the approximate token count for a number of
// bytes (rounding up).
func ApproxTokensFromByteCount(bytes int) uint64 {
	b := uint64(bytes)
	return (b + (approxBytesPerToken - 1)) / approxBytesPerToken
}

// splitString splits s into a prefix that fits within beginningBytes and a
// suffix that fits within endBytes, both on UTF-8 boundaries. It returns the
// number of whole characters dropped from the middle along with the prefix and
// suffix substrings. Inputs are never mutated.
func splitString(s string, beginningBytes, endBytes int) (int, string, string) {
	if s == "" {
		return 0, "", ""
	}

	length := len(s)
	tailStartTarget := saturatingSub(length, endBytes)
	prefixEnd := 0
	suffixStart := length
	removedChars := 0
	suffixStarted := false

	for idx, ch := range s {
		charEnd := idx + utf8.RuneLen(ch)
		if charEnd <= beginningBytes {
			prefixEnd = charEnd
			continue
		}

		if idx >= tailStartTarget {
			if !suffixStarted {
				suffixStart = idx
				suffixStarted = true
			}
			continue
		}

		removedChars++
	}

	if suffixStart < prefixEnd {
		suffixStart = prefixEnd
	}

	before := s[:prefixEnd]
	after := s[suffixStart:]

	return removedChars, before, after
}

// splitBudget divides a byte budget into a left and right half, giving any odd
// remainder to the right half (matching the reference implementation).
func splitBudget(budget int) (int, int) {
	left := budget / 2
	return left, budget - left
}

// formatTruncationMarker builds the "…N tokens truncated…" or
// "…N chars truncated…" marker.
func formatTruncationMarker(useTokens bool, removedCount uint64) string {
	if useTokens {
		return fmt.Sprintf("…%d tokens truncated…", removedCount)
	}
	return fmt.Sprintf("…%d chars truncated…", removedCount)
}

// removedUnits returns the count placed in the truncation marker: an
// approximate token count derived from removedBytes when counting tokens,
// otherwise the removed character count.
func removedUnits(useTokens bool, removedBytes, removedChars int) uint64 {
	if useTokens {
		return ApproxTokensFromByteCount(removedBytes)
	}
	if removedChars < 0 {
		return 0
	}
	return uint64(removedChars)
}

// assembleTruncatedOutput joins the prefix, marker, and suffix.
func assembleTruncatedOutput(prefix, suffix, marker string) string {
	var out strings.Builder
	out.Grow(len(prefix) + len(marker) + len(suffix) + 1)
	out.WriteString(prefix)
	out.WriteString(marker)
	out.WriteString(suffix)
	return out.String()
}

// saturatingSub returns a - b, clamped to 0 (mirroring Rust's saturating_sub on
// unsigned integers).
func saturatingSub(a, b int) int {
	if a < b {
		return 0
	}
	return a - b
}

// saturatingMul returns a * b, clamped to math.MaxInt on overflow (mirroring
// Rust's saturating_mul).
func saturatingMul(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	product := a * b
	if product/b != a {
		return math.MaxInt
	}
	return product
}
