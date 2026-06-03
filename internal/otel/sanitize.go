package otel

import "strings"

// maxTagLen mirrors the MAX_LEN cap in Rust sanitize_metric_tag_value.
const maxTagLen = 256

// SanitizeMetricTagValue sanitizes a tag value to comply with metric tag
// validation rules: only ASCII alphanumeric, '.', '_', '-', and '/' are
// allowed; all other runes become '_'. Underscores are trimmed from both ends.
// If the result is empty or has no alphanumeric character, "unspecified" is
// returned. The value is capped at 256 bytes. Mirrors Rust
// `sanitize_metric_tag_value`.
func SanitizeMetricTagValue(value string) string {
	var b strings.Builder
	for _, ch := range value {
		if isASCIIAlphanumeric(ch) || ch == '.' || ch == '_' || ch == '-' || ch == '/' {
			b.WriteRune(ch)
		} else {
			b.WriteByte('_')
		}
	}
	trimmed := strings.Trim(b.String(), "_")
	if trimmed == "" || !hasASCIIAlphanumeric(trimmed) {
		return "unspecified"
	}
	if len(trimmed) <= maxTagLen {
		return trimmed
	}
	// Rust slices by byte index [..MAX_LEN]; replicate the byte truncation.
	return trimmed[:maxTagLen]
}

func isASCIIAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func hasASCIIAlphanumeric(s string) bool {
	for _, ch := range s {
		if isASCIIAlphanumeric(ch) {
			return true
		}
	}
	return false
}
