package otel

import "strings"

// Tracing target prefixes used to route events to logs vs traces. Mirrors
// codex-rs/otel/src/targets.rs.
const (
	OtelTargetPrefix    = "codex_otel"
	OtelLogOnlyTarget   = "codex_otel.log_only"
	OtelTraceSafeTarget = "codex_otel.trace_safe"
)

// IsLogExportTarget reports whether a target should be exported as a log.
// Mirrors Rust `is_log_export_target`.
func IsLogExportTarget(target string) bool {
	return strings.HasPrefix(target, OtelTargetPrefix) && !IsTraceSafeTarget(target)
}

// IsTraceSafeTarget reports whether a target is trace-safe. Mirrors Rust
// `is_trace_safe_target`.
func IsTraceSafeTarget(target string) bool {
	return strings.HasPrefix(target, OtelTraceSafeTarget)
}
