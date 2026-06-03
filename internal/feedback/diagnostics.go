// Package feedback is a faithful port of codex-rs/feedback. It captures
// full-fidelity logs into a bounded ring buffer (4 MiB), collects connectivity
// diagnostics and doctor/sandbox attachments, and uploads them to Sentry using
// the same DSN and tag conventions as codex.
//
// Feedback respects privacy defaults: log and diagnostic attachments are only
// included when the caller has obtained user consent (include_logs is set by
// the caller after a consent gate).
package feedback

import (
	"fmt"
	"sort"
	"strings"
)

// FeedbackDiagnosticsAttachmentFilename is the filename used for the
// connectivity diagnostics attachment. Mirrors Rust
// FEEDBACK_DIAGNOSTICS_ATTACHMENT_FILENAME.
const FeedbackDiagnosticsAttachmentFilename = "codex-connectivity-diagnostics.txt"

// proxyEnvVars are the proxy environment variables surfaced in diagnostics.
// Mirrors Rust PROXY_ENV_VARS (order is preserved for the attachment text).
var proxyEnvVars = []string{
	"HTTP_PROXY",
	"http_proxy",
	"HTTPS_PROXY",
	"https_proxy",
	"ALL_PROXY",
	"all_proxy",
}

// FeedbackDiagnostic is one diagnostic with a headline and detail lines.
// Mirrors Rust `FeedbackDiagnostic`.
type FeedbackDiagnostic struct {
	Headline string
	Details  []string
}

// FeedbackDiagnostics is a collection of diagnostics. Mirrors Rust
// `FeedbackDiagnostics`.
type FeedbackDiagnostics struct {
	diagnostics []FeedbackDiagnostic
}

// NewFeedbackDiagnostics wraps an explicit list of diagnostics. Mirrors Rust
// `FeedbackDiagnostics::new`.
func NewFeedbackDiagnostics(diagnostics []FeedbackDiagnostic) FeedbackDiagnostics {
	return FeedbackDiagnostics{diagnostics: diagnostics}
}

// CollectFromEnv collects diagnostics from the current process environment.
// Mirrors Rust `FeedbackDiagnostics::collect_from_env`.
func CollectFromEnv(env func(string) (string, bool)) FeedbackDiagnostics {
	var diagnostics []FeedbackDiagnostic

	var proxyDetails []string
	for _, key := range proxyEnvVars {
		if value, ok := env(key); ok {
			proxyDetails = append(proxyDetails, fmt.Sprintf("%s = %s", key, value))
		}
	}
	if len(proxyDetails) > 0 {
		diagnostics = append(diagnostics, FeedbackDiagnostic{
			Headline: "Proxy environment variables are set and may affect connectivity.",
			Details:  proxyDetails,
		})
	}

	return FeedbackDiagnostics{diagnostics: diagnostics}
}

// CollectFromPairs collects diagnostics from explicit key/value pairs. The
// proxy detail order follows proxyEnvVars (not the input order), matching the
// Rust HashMap-then-fixed-order iteration. Pairs are deduplicated by last write.
func CollectFromPairs(pairs map[string]string) FeedbackDiagnostics {
	return CollectFromEnv(func(key string) (string, bool) {
		v, ok := pairs[key]
		return v, ok
	})
}

// IsEmpty reports whether there are no diagnostics. Mirrors Rust `is_empty`.
func (d FeedbackDiagnostics) IsEmpty() bool {
	return len(d.diagnostics) == 0
}

// Diagnostics returns the underlying diagnostics. Mirrors Rust `diagnostics`.
func (d FeedbackDiagnostics) Diagnostics() []FeedbackDiagnostic {
	return d.diagnostics
}

// AttachmentText renders the diagnostics as the connectivity attachment text,
// or returns ("", false) when empty. Mirrors Rust `attachment_text`.
func (d FeedbackDiagnostics) AttachmentText() (string, bool) {
	if len(d.diagnostics) == 0 {
		return "", false
	}
	lines := []string{"Connectivity diagnostics", ""}
	for _, diag := range d.diagnostics {
		lines = append(lines, fmt.Sprintf("- %s", diag.Headline))
		for _, detail := range diag.Details {
			lines = append(lines, fmt.Sprintf("  - %s", detail))
		}
	}
	return strings.Join(lines, "\n"), true
}

// sortedKeys is a small helper retained for deterministic iteration where the
// Rust source relies on BTreeMap ordering.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
