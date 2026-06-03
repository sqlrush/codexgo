// Package connectors ports the codex connectors crate: SaaS app connector
// directory metadata, merging, filtering, accessibility, and the on-disk
// directory cache. Connector entries are represented with the shared
// appserverproto.AppInfo type so JSON shapes match codex byte-for-byte.
package connectors

import "strings"

// ConnectorInstallURL returns the canonical install URL for a connector. Rust:
// connector_install_url.
func ConnectorInstallURL(name, connectorID string) string {
	slug := connectorNameSlug(name)
	return "https://chatgpt.com/apps/" + slug + "/" + connectorID
}

// connectorNameSlug lowercases the name and replaces every non-alphanumeric
// character with a dash, trimming leading/trailing dashes. An empty result
// becomes "app". Rust: connector_name_slug.
func connectorNameSlug(name string) string {
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

// isASCIIAlphanumeric mirrors Rust char::is_ascii_alphanumeric.
func isASCIIAlphanumeric(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// toASCIILower mirrors Rust char::to_ascii_lowercase (ASCII-only folding).
func toASCIILower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// normalizeConnectorName trims the name, falling back to the connector id when
// the trimmed name is empty. Rust: normalize_connector_name.
func normalizeConnectorName(name, connectorID string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return connectorID
	}
	return trimmed
}

// normalizeConnectorValue trims an optional value, returning nil when it is
// absent or blank. Rust: normalize_connector_value.
func normalizeConnectorValue(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
