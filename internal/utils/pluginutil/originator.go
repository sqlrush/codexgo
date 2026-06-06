package pluginutil

import "os"

// Originator helpers: a minimal hand-rolled equivalent of the parts of
// `codex_login::default_client` that the MCP connector allow-list depends on.
//
// The full Rust implementation maintains a process-global, write-once originator
// (also used to build HTTP headers and User-Agent strings). The connector
// allow-list only reads the originator's string value, so this port models just
// that: the env-var override and the default, without the global cache or header
// construction. Set the default via a higher-level package if/when one exists;
// the override env var below always takes precedence in Codex.

const (
	// defaultOriginator mirrors the Rust DEFAULT_ORIGINATOR constant.
	defaultOriginator = "codex_cli_rs"

	// originatorOverrideEnvVar mirrors the Rust
	// CODEX_INTERNAL_ORIGINATOR_OVERRIDE_ENV_VAR constant.
	originatorOverrideEnvVar = "CODEXGO_INTERNAL_ORIGINATOR_OVERRIDE"
)

// originatorValue returns the current originator string, mirroring the value
// read by the Rust `originator()` function: the override environment variable
// when set, otherwise the default originator.
//
// Unlike the Rust version this does not cache a process-global value or build an
// HTTP header; the connector allow-list only consumes the string value.
func originatorValue() string {
	if override, ok := os.LookupEnv(originatorOverrideEnvVar); ok {
		return override
	}
	return defaultOriginator
}

// IsFirstPartyChatOriginator mirrors the Rust `is_first_party_chat_originator`.
//
// It reports whether the given originator value identifies a first-party chat
// surface (the Codex Atlas or ChatGPT desktop apps).
func IsFirstPartyChatOriginator(originatorValue string) bool {
	return originatorValue == "codex_atlas" || originatorValue == "codex_chatgpt_desktop"
}
