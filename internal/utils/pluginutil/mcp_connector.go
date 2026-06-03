package pluginutil

import "strings"

// MCP connector helpers ported from the Rust `mcp_connector.rs`.

// disallowedConnectorIDs lists connector ids blocked for non first-party-chat
// originators, mirroring the Rust DISALLOWED_CONNECTOR_IDS constant.
var disallowedConnectorIDs = [...]string{
	"asdk_app_6938a94a61d881918ef32cb999ff937c",
	"connector_2b0a9009c9c64bf9933a3dae3f2b1254",
	"connector_3f8d1a79f27c4c7ba1a897ab13bf37dc",
	"connector_68de829bf7648191acd70a907364c67c",
	"connector_68e004f14af881919eb50893d3d9f523",
	"connector_69272cb413a081919685ec3c88d1744e",
}

// firstPartyChatDisallowedConnectorIDs lists connector ids blocked for
// first-party-chat originators, mirroring the Rust
// FIRST_PARTY_CHAT_DISALLOWED_CONNECTOR_IDS constant.
var firstPartyChatDisallowedConnectorIDs = [...]string{
	"connector_0f9c9d4592e54d0a9a12b3f44a1e2010",
}

// IsConnectorIDAllowed mirrors the Rust `is_connector_id_allowed`.
//
// It reports whether the given connector id may be used, using the current
// process originator to select the applicable disallow-list.
func IsConnectorIDAllowed(connectorID string) bool {
	return isConnectorIDAllowedForOriginator(connectorID, originatorValue())
}

// isConnectorIDAllowedForOriginator mirrors the Rust
// `is_connector_id_allowed_for_originator`.
//
// First-party chat originators use a dedicated disallow-list; all other
// originators use the general disallow-list.
func isConnectorIDAllowedForOriginator(connectorID, originatorValue string) bool {
	var disallowed []string
	if IsFirstPartyChatOriginator(originatorValue) {
		disallowed = firstPartyChatDisallowedConnectorIDs[:]
	} else {
		disallowed = disallowedConnectorIDs[:]
	}
	for _, id := range disallowed {
		if id == connectorID {
			return false
		}
	}
	return true
}

// SanitizeName mirrors the Rust `sanitize_name`.
//
// It slugifies the name (see sanitizeSlug) and then replaces hyphens with
// underscores, yielding an identifier-friendly token.
func SanitizeName(name string) string {
	return strings.ReplaceAll(sanitizeSlug(name), "-", "_")
}

// sanitizeSlug mirrors the Rust `sanitize_slug`.
//
// Each ASCII alphanumeric rune is lowercased; every other rune (including
// non-ASCII) becomes a hyphen. Leading and trailing hyphens are trimmed, and an
// empty result becomes the literal "app".
func sanitizeSlug(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if isASCIIAlphanumeric(r) {
			b.WriteRune(toASCIILower(r))
		} else {
			b.WriteByte('-')
		}
	}
	normalized := strings.Trim(b.String(), "-")
	if normalized == "" {
		return "app"
	}
	return normalized
}

// isASCIIAlphanumeric reports whether r is an ASCII letter or digit, matching
// Rust's `char::is_ascii_alphanumeric`.
func isASCIIAlphanumeric(r rune) bool {
	return (r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

// toASCIILower lowercases an ASCII uppercase letter, leaving all other runes
// unchanged, matching Rust's `char::to_ascii_lowercase`.
func toASCIILower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
