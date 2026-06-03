package secrets

import "regexp"

// Redaction sentinels and patterns mirror codex's `sanitizer` module. The
// patterns are compiled once at package init; an invalid pattern panics, which
// the load_regex-equivalent test guards against.
const redactedSecret = "[REDACTED_SECRET]"

var (
	// openAIKeyRegex matches OpenAI-style secret keys ("sk-" followed by at
	// least 20 alphanumerics).
	openAIKeyRegex = regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)

	// awsAccessKeyIDRegex matches AWS access key IDs ("AKIA" + 16 uppercase
	// alphanumerics) bounded by word boundaries.
	awsAccessKeyIDRegex = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)

	// bearerTokenRegex matches "Bearer <token>" authorization headers
	// case-insensitively.
	bearerTokenRegex = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-]{16,}\b`)

	// secretAssignmentRegex matches assignments such as `api_key: "value"` or
	// `password=value`, redacting the value while preserving the key, separator,
	// and optional opening quote.
	secretAssignmentRegex = regexp.MustCompile(
		`(?i)\b(api[_-]?key|token|secret|password)\b(\s*[:=]\s*)(["']?)[^\s"']{8,}`)
)

// RedactSecrets removes secrets and keys from input on a best-effort basis using
// a set of well-known patterns. It mirrors codex's `redact_secrets`.
func RedactSecrets(input string) string {
	redacted := openAIKeyRegex.ReplaceAllString(input, redactedSecret)
	redacted = awsAccessKeyIDRegex.ReplaceAllString(redacted, redactedSecret)
	redacted = bearerTokenRegex.ReplaceAllString(redacted, "Bearer "+redactedSecret)
	redacted = secretAssignmentRegex.ReplaceAllString(redacted, "${1}${2}${3}"+redactedSecret)
	return redacted
}
