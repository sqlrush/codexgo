package tools

import (
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/strutil"
)

// telemetryPreview returns a bounded preview of content for telemetry logging.
// It mirrors Rust `telemetry_preview`: it truncates to a byte budget at a UTF-8
// boundary, then to a line budget, appending a truncation notice when either
// limit trims content.
func telemetryPreview(content string) string {
	truncatedSlice := strutil.TakeBytesAtCharBoundary(content, telemetryPreviewMaxBytes)
	truncatedByBytes := len(truncatedSlice) < len(content)

	var preview strings.Builder
	lines := splitLines(truncatedSlice)
	emitted := 0
	for idx := 0; idx < telemetryPreviewMaxLines && idx < len(lines); idx++ {
		if idx > 0 {
			preview.WriteByte('\n')
		}
		preview.WriteString(lines[idx])
		emitted++
	}
	truncatedByLines := emitted < len(lines)

	if !truncatedByBytes && !truncatedByLines {
		return content
	}

	previewStr := preview.String()
	// Re-add a trailing newline that `str::lines` dropped when the slice has more
	// bytes than the preview and the next byte is a newline.
	if len(previewStr) < len(truncatedSlice) &&
		truncatedSlice[len(previewStr)] == '\n' {
		previewStr += "\n"
	}

	if previewStr != "" && !strings.HasSuffix(previewStr, "\n") {
		previewStr += "\n"
	}
	previewStr += telemetryPreviewTruncationNote
	return previewStr
}

// splitLines splits s on '\n', dropping a trailing empty element so it matches
// Rust's `str::lines` (which does not yield a final empty line for a trailing
// newline). A carriage return preceding a newline is also trimmed.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts
}
