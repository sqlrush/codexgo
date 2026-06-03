package connectors

import (
	"sort"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// AccessibleConnectorTool describes one connector-backed tool discovered for the
// current account. Rust: AccessibleConnectorTool.
type AccessibleConnectorTool struct {
	ConnectorID          string
	ConnectorName        *string
	ConnectorDescription *string
	PluginDisplayNames   []string
}

// accessibleAccumulator pairs an in-progress AppInfo with the deduplicated set
// of plugin display names accumulated for that connector. Rust uses a
// (AppInfo, BTreeSet<String>) tuple; the BTreeSet keeps names sorted+unique.
type accessibleAccumulator struct {
	info               appserverproto.AppInfo
	pluginDisplayNames map[string]struct{}
}

// CollectAccessibleConnectors folds connector-backed tools into a deduplicated,
// sorted list of accessible connectors. Rust: collect_accessible_connectors.
func CollectAccessibleConnectors(tools []AccessibleConnectorTool) []appserverproto.AppInfo {
	connectors := make(map[string]*accessibleAccumulator)
	// Preserve first-seen id order for deterministic iteration before the final
	// sort, mirroring how the Rust HashMap is later sorted into a stable order.
	for _, tool := range tools {
		connectorID := tool.ConnectorID
		connectorName := connectorID
		if normalized := normalizeConnectorValue(tool.ConnectorName); normalized != nil {
			connectorName = *normalized
		}
		connectorDescription := normalizeConnectorValue(tool.ConnectorDescription)

		if existing, ok := connectors[connectorID]; ok {
			if existing.info.Name == connectorID && connectorName != connectorID {
				existing.info.Name = connectorName
			}
			if existing.info.Description == nil && connectorDescription != nil {
				existing.info.Description = connectorDescription
			}
			for _, name := range tool.PluginDisplayNames {
				existing.pluginDisplayNames[name] = struct{}{}
			}
			continue
		}

		acc := &accessibleAccumulator{
			info: appserverproto.AppInfo{
				ID:           connectorID,
				Name:         connectorName,
				Description:  connectorDescription,
				IsAccessible: true,
				IsEnabled:    true,
			},
			pluginDisplayNames: make(map[string]struct{}),
		}
		for _, name := range tool.PluginDisplayNames {
			acc.pluginDisplayNames[name] = struct{}{}
		}
		connectors[connectorID] = acc
	}

	accessible := make([]appserverproto.AppInfo, 0, len(connectors))
	for _, acc := range connectors {
		connector := acc.info
		connector.PluginDisplayNames = sortedKeys(acc.pluginDisplayNames)
		installURL := ConnectorInstallURL(connector.Name, connector.ID)
		connector.InstallURL = &installURL
		accessible = append(accessible, connector)
	}
	sortConnectorsByAccessibilityAndName(accessible)
	return accessible
}

// sortedKeys returns the set keys in sorted order, mirroring BTreeSet iteration.
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
