package client

import "strings"

// exactChatGPTHosts and chatGPTSubdomainSuffixes mirror the allowlists in the
// Rust `chatgpt_hosts` module.
var (
	exactChatGPTHosts = []string{
		"chatgpt.com",
		"chat.openai.com",
		"chatgpt-staging.com",
	}
	chatGPTSubdomainSuffixes = []string{
		".chatgpt.com",
		".chatgpt-staging.com",
	}
)

// IsAllowedChatGPTHost reports whether host is one of the ChatGPT hosts Codex is
// allowed to treat as first-party ChatGPT traffic. It mirrors the Rust
// `is_allowed_chatgpt_host`.
func IsAllowedChatGPTHost(host string) bool {
	for _, h := range exactChatGPTHosts {
		if host == h {
			return true
		}
	}
	for _, suffix := range chatGPTSubdomainSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
