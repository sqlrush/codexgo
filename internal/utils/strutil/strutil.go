// Package strutil provides string and text helpers used in prompts and
// rendering. It is a faithful, drop-in-compatible Go reimplementation of the
// OpenAI Codex Rust crate codex-utils-string (codex 0.136.0), preserving the
// same externally observable behavior and output formats.
//
// All functions are pure: they never mutate their inputs and always return
// freshly allocated results.
package strutil

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxMetricTagLen is the maximum byte length of a sanitized metric tag value.
const maxMetricTagLen = 256

// uuidPattern matches a canonical 8-4-4-4-12 hexadecimal UUID. It mirrors the
// regex used by the reference crate:
//
//	[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}
var uuidPattern = regexp.MustCompile(
	`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`,
)

// TakeBytesAtCharBoundary returns the longest prefix of s whose byte length
// does not exceed maxBytes, truncated at a UTF-8 character boundary.
//
// If s already fits within maxBytes, s is returned unchanged.
func TakeBytesAtCharBoundary(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	lastOK := 0
	for i, ch := range s {
		nb := i + utf8.RuneLen(ch)
		if nb > maxBytes {
			break
		}
		lastOK = nb
	}
	return s[:lastOK]
}

// SanitizeMetricTagValue sanitizes a tag value to comply with metric tag
// validation rules: only ASCII alphanumerics and the characters '.', '_', '-',
// and '/' are allowed. Any other character is replaced with '_'.
//
// Leading and trailing '_' characters are then trimmed. If the trimmed result
// is empty or contains no ASCII alphanumeric character, "unspecified" is
// returned. Otherwise the result is capped at 256 bytes.
func SanitizeMetricTagValue(value string) string {
	var sanitized strings.Builder
	sanitized.Grow(len(value))
	for _, ch := range value {
		if isAllowedTagRune(ch) {
			sanitized.WriteRune(ch)
		} else {
			sanitized.WriteByte('_')
		}
	}

	trimmed := strings.Trim(sanitized.String(), "_")
	if trimmed == "" || !containsASCIIAlphanumeric(trimmed) {
		return "unspecified"
	}
	if len(trimmed) <= maxMetricTagLen {
		return trimmed
	}
	// All retained characters are ASCII, so byte indexing is char-safe here,
	// matching the reference crate's byte-slice cap.
	return trimmed[:maxMetricTagLen]
}

// isAllowedTagRune reports whether ch may appear in a sanitized metric tag.
func isAllowedTagRune(ch rune) bool {
	return isASCIIAlphanumeric(ch) || ch == '.' || ch == '_' || ch == '-' || ch == '/'
}

// isASCIIAlphanumeric reports whether ch is an ASCII letter or digit.
func isASCIIAlphanumeric(ch rune) bool {
	return (ch >= '0' && ch <= '9') ||
		(ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z')
}

// containsASCIIAlphanumeric reports whether s contains at least one ASCII
// letter or digit.
func containsASCIIAlphanumeric(s string) bool {
	for _, ch := range s {
		if isASCIIAlphanumeric(ch) {
			return true
		}
	}
	return false
}

// FindUUIDs returns every canonical 8-4-4-4-12 UUID found in s, in order of
// appearance and without overlaps. The returned slice is always non-nil; when
// no UUIDs are present it is empty.
func FindUUIDs(s string) []string {
	matches := uuidPattern.FindAllString(s, -1)
	if matches == nil {
		return []string{}
	}
	return matches
}

// NormalizeMarkdownHashLocationSuffix converts a markdown-style "#L.." location
// suffix into a terminal-friendly ":line[:column][-line[:column]]" suffix.
//
// The second return value reports whether suffix was a well-formed location
// suffix; when false, the first return value is the empty string. This mirrors
// the Rust Option<String> return.
func NormalizeMarkdownHashLocationSuffix(suffix string) (string, bool) {
	fragment, ok := strings.CutPrefix(suffix, "#")
	if !ok {
		return "", false
	}

	start, end, hasEnd := fragment, "", false
	if before, after, found := strings.Cut(fragment, "-"); found {
		start, end, hasEnd = before, after, true
	}

	startLine, startColumn, hasStartColumn, ok := parseMarkdownHashLocationPoint(start)
	if !ok {
		return "", false
	}

	var normalized strings.Builder
	normalized.WriteByte(':')
	normalized.WriteString(startLine)
	if hasStartColumn {
		normalized.WriteByte(':')
		normalized.WriteString(startColumn)
	}

	if hasEnd {
		endLine, endColumn, hasEndColumn, ok := parseMarkdownHashLocationPoint(end)
		if !ok {
			return "", false
		}
		normalized.WriteByte('-')
		normalized.WriteString(endLine)
		if hasEndColumn {
			normalized.WriteByte(':')
			normalized.WriteString(endColumn)
		}
	}

	return normalized.String(), true
}

// parseMarkdownHashLocationPoint parses a single "L<line>[C<column>]" point.
// It returns the line, the optional column, whether a column was present, and
// whether the point was well-formed (i.e. began with 'L').
func parseMarkdownHashLocationPoint(point string) (line, column string, hasColumn, ok bool) {
	rest, found := strings.CutPrefix(point, "L")
	if !found {
		return "", "", false, false
	}
	if before, after, found := strings.Cut(rest, "C"); found {
		return before, after, true, true
	}
	return rest, "", false, true
}
