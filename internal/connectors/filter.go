package connectors

import (
	"sort"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// disallowedConnectorIDs are connectors hidden from the default (non-first-party)
// directory surface. Rust: DISALLOWED_CONNECTOR_IDS.
var disallowedConnectorIDs = []string{
	"asdk_app_6938a94a61d881918ef32cb999ff937c",
	"connector_2b0a9009c9c64bf9933a3dae3f2b1254",
	"connector_3f8d1a79f27c4c7ba1a897ab13bf37dc",
	"connector_68de829bf7648191acd70a907364c67c",
	"connector_68e004f14af881919eb50893d3d9f523",
	"connector_69272cb413a081919685ec3c88d1744e",
}

// firstPartyChatDisallowedConnectorIDs are connectors hidden specifically from
// first-party chat originators. Rust: FIRST_PARTY_CHAT_DISALLOWED_CONNECTOR_IDS.
var firstPartyChatDisallowedConnectorIDs = []string{
	"connector_0f9c9d4592e54d0a9a12b3f44a1e2010",
}

// FilterToolSuggestDiscoverableConnectors keeps only plugin-backed,
// not-yet-accessible directory connectors that are discoverable for tool
// suggestions. The result is sorted by name then id. Rust:
// filter_tool_suggest_discoverable_connectors.
func FilterToolSuggestDiscoverableConnectors(
	directoryConnectors []appserverproto.AppInfo,
	accessibleConnectors []appserverproto.AppInfo,
	discoverableConnectorIDs map[string]struct{},
	originatorValue string,
) []appserverproto.AppInfo {
	accessibleConnectorIDs := make(map[string]struct{})
	for i := range accessibleConnectors {
		if accessibleConnectors[i].IsAccessible {
			accessibleConnectorIDs[accessibleConnectors[i].ID] = struct{}{}
		}
	}

	allowed := FilterDisallowedConnectors(directoryConnectors, originatorValue)
	connectors := make([]appserverproto.AppInfo, 0, len(allowed))
	for _, connector := range allowed {
		if _, blocked := accessibleConnectorIDs[connector.ID]; blocked {
			continue
		}
		if _, discoverable := discoverableConnectorIDs[connector.ID]; !discoverable {
			continue
		}
		connectors = append(connectors, connector)
	}
	sort.SliceStable(connectors, func(i, j int) bool {
		if connectors[i].Name != connectors[j].Name {
			return connectors[i].Name < connectors[j].Name
		}
		return connectors[i].ID < connectors[j].ID
	})
	return connectors
}

// FilterDisallowedConnectors removes connectors that are not allowed for the
// given originator, preserving input order. Rust: filter_disallowed_connectors.
func FilterDisallowedConnectors(
	connectors []appserverproto.AppInfo,
	originatorValue string,
) []appserverproto.AppInfo {
	firstPartyChatOriginator := isFirstPartyChatOriginator(originatorValue)
	out := make([]appserverproto.AppInfo, 0, len(connectors))
	for _, connector := range connectors {
		if isConnectorIDAllowed(connector.ID, firstPartyChatOriginator) {
			out = append(out, connector)
		}
	}
	return out
}

// isFirstPartyChatOriginator reports whether the originator is one of the
// first-party chat clients. Rust: is_first_party_chat_originator.
func isFirstPartyChatOriginator(originatorValue string) bool {
	return originatorValue == "codex_atlas" || originatorValue == "codex_chatgpt_desktop"
}

// isConnectorIDAllowed reports whether the connector id passes the applicable
// disallow list. Rust: is_connector_id_allowed.
func isConnectorIDAllowed(connectorID string, firstPartyChatOriginator bool) bool {
	disallowed := disallowedConnectorIDs
	if firstPartyChatOriginator {
		disallowed = firstPartyChatDisallowedConnectorIDs
	}
	for _, id := range disallowed {
		if id == connectorID {
			return false
		}
	}
	return true
}
