// Package elapsed provides helpers for rendering elapsed time durations as
// compact, human-readable strings.
//
// It is a faithful Go port of the codex-utils-elapsed Rust crate. The
// externally observable output format is preserved exactly so that callers
// relying on the rendered strings behave identically across the two
// implementations.
package elapsed

import (
	"fmt"
	"time"
)

const (
	millisPerSecond = 1000
	millisPerMinute = 60_000
)

// FormatDuration converts a [time.Duration] into a human-readable, compact
// string.
//
// Formatting rules:
//   - < 1 s   -> "{milli}ms"
//   - < 60 s  -> "{sec:.2}s" (two decimal places)
//   - >= 60 s -> "{min}m {sec:02}s"
//
// Negative durations are treated by their millisecond magnitude, mirroring the
// reference implementation which casts to a signed integer count of
// milliseconds. A negative duration therefore falls into the sub-second branch
// and renders with a leading minus sign (for example "-5ms").
func FormatDuration(duration time.Duration) string {
	millis := duration.Milliseconds()
	return formatElapsedMillis(millis)
}

// formatElapsedMillis renders a signed count of milliseconds using the same
// branch logic as the reference implementation.
func formatElapsedMillis(millis int64) string {
	switch {
	case millis < millisPerSecond:
		return fmt.Sprintf("%dms", millis)
	case millis < millisPerMinute:
		return fmt.Sprintf("%.2fs", float64(millis)/float64(millisPerSecond))
	default:
		minutes := millis / millisPerMinute
		seconds := (millis % millisPerMinute) / millisPerSecond
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	}
}
