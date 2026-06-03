package ollama

import "strings"

// IsOpenAICompatibleBaseURL reports whether a base_url points at an
// OpenAI-compatible root (".../v1").
//
// It mirrors the Rust is_openai_compatible_base_url.
func IsOpenAICompatibleBaseURL(baseURL string) bool {
	return strings.HasSuffix(strings.TrimRight(baseURL, "/"), "/v1")
}

// BaseURLToHostRoot converts a provider base_url into the native Ollama host
// root. For example, "http://localhost:11434/v1" -> "http://localhost:11434".
//
// It mirrors the Rust base_url_to_host_root.
func BaseURLToHostRoot(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return strings.TrimRight(strings.TrimSuffix(trimmed, "/v1"), "/")
	}
	return trimmed
}
