package connectors

import (
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// ConnectorDisplayLabel returns the connector's display name. Rust:
// connector_display_label.
func ConnectorDisplayLabel(connector *appserverproto.AppInfo) string {
	return connector.Name
}

// ConnectorMentionSlug returns the @-mention slug for a connector, derived from
// its display label. Rust: connector_mention_slug.
func ConnectorMentionSlug(connector *appserverproto.AppInfo) string {
	return ConnectorMentionSlugFromName(ConnectorDisplayLabel(connector))
}

// ConnectorMentionSlugFromName derives a mention slug from a connector name.
// Rust: connector_mention_slug_from_name.
func ConnectorMentionSlugFromName(name string) string {
	return connectorNameSlug(name)
}

// SanitizeName produces an identifier-safe form of a connector name by slugging
// it and converting dashes to underscores. Rust: sanitize_name.
func SanitizeName(name string) string {
	return strings.ReplaceAll(connectorNameSlug(name), "-", "_")
}

// sortConnectorsByAccessibilityAndName orders connectors with accessible
// connectors first, then by name, then by id. Rust:
// sort_connectors_by_accessibility_and_name. The Rust sort is stable
// (slice::sort_by); sort.SliceStable preserves that behavior.
func sortConnectorsByAccessibilityAndName(connectors []appserverproto.AppInfo) {
	sort.SliceStable(connectors, func(i, j int) bool {
		left, right := connectors[i], connectors[j]
		// right.is_accessible.cmp(&left.is_accessible): accessible (true)
		// sorts before inaccessible (false).
		if left.IsAccessible != right.IsAccessible {
			return left.IsAccessible
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
}
