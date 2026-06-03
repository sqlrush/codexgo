package connectors

import (
	"sort"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// MergeConnectors combines directory connectors with the account's accessible
// connectors, preferring richer metadata and unioning plugin display names. The
// result is sorted by accessibility then name. Rust: merge_connectors.
func MergeConnectors(
	connectors []appserverproto.AppInfo,
	accessibleConnectors []appserverproto.AppInfo,
) []appserverproto.AppInfo {
	merged := make(map[string]*appserverproto.AppInfo, len(connectors))
	order := make([]string, 0, len(connectors))
	for i := range connectors {
		connector := connectors[i]
		connector.IsAccessible = false
		if _, exists := merged[connector.ID]; !exists {
			order = append(order, connector.ID)
		}
		c := connector
		merged[connector.ID] = &c
	}

	for i := range accessibleConnectors {
		connector := accessibleConnectors[i]
		connector.IsAccessible = true
		connectorID := connector.ID
		if existing, ok := merged[connectorID]; ok {
			existing.IsAccessible = true
			if existing.Name == existing.ID && connector.Name != connector.ID {
				existing.Name = connector.Name
			}
			if existing.Description == nil && connector.Description != nil {
				existing.Description = connector.Description
			}
			if existing.LogoURL == nil && connector.LogoURL != nil {
				existing.LogoURL = connector.LogoURL
			}
			if existing.LogoURLDark == nil && connector.LogoURLDark != nil {
				existing.LogoURLDark = connector.LogoURLDark
			}
			if existing.DistributionChannel == nil && connector.DistributionChannel != nil {
				existing.DistributionChannel = connector.DistributionChannel
			}
			existing.PluginDisplayNames = append(existing.PluginDisplayNames, connector.PluginDisplayNames...)
		} else {
			c := connector
			merged[connectorID] = &c
			order = append(order, connectorID)
		}
	}

	out := make([]appserverproto.AppInfo, 0, len(merged))
	for _, id := range order {
		out = append(out, *merged[id])
	}
	for i := range out {
		if out[i].InstallURL == nil {
			installURL := ConnectorInstallURL(out[i].Name, out[i].ID)
			out[i].InstallURL = &installURL
		}
		out[i].PluginDisplayNames = sortDedup(out[i].PluginDisplayNames)
	}
	sortConnectorsByAccessibilityAndName(out)
	return out
}

// MergePluginConnectors appends placeholder connectors for any plugin app id not
// already present, then re-sorts. Rust: merge_plugin_connectors.
func MergePluginConnectors(
	connectors []appserverproto.AppInfo,
	pluginAppIDs []string,
) []appserverproto.AppInfo {
	merged := make([]appserverproto.AppInfo, len(connectors))
	copy(merged, connectors)
	connectorIDs := make(map[string]struct{}, len(merged))
	for i := range merged {
		connectorIDs[merged[i].ID] = struct{}{}
	}
	for _, connectorID := range pluginAppIDs {
		if _, exists := connectorIDs[connectorID]; !exists {
			connectorIDs[connectorID] = struct{}{}
			merged = append(merged, PluginConnectorToAppInfo(connectorID))
		}
	}
	sortConnectorsByAccessibilityAndName(merged)
	return merged
}

// MergePluginConnectorsWithAccessible builds placeholder connectors for plugin
// app ids that are accessible, then merges them with the accessible connectors.
// Rust: merge_plugin_connectors_with_accessible.
func MergePluginConnectorsWithAccessible(
	pluginAppIDs []string,
	accessibleConnectors []appserverproto.AppInfo,
) []appserverproto.AppInfo {
	accessibleConnectorIDs := make(map[string]struct{}, len(accessibleConnectors))
	for i := range accessibleConnectors {
		accessibleConnectorIDs[accessibleConnectors[i].ID] = struct{}{}
	}
	pluginConnectors := make([]appserverproto.AppInfo, 0, len(pluginAppIDs))
	for _, connectorID := range pluginAppIDs {
		if _, ok := accessibleConnectorIDs[connectorID]; ok {
			pluginConnectors = append(pluginConnectors, PluginConnectorToAppInfo(connectorID))
		}
	}
	return MergeConnectors(pluginConnectors, accessibleConnectors)
}

// PluginConnectorToAppInfo builds a placeholder AppInfo for a plugin app id. The
// name is left as the id so MergeConnectors can replace it with canonical
// metadata. Rust: plugin_connector_to_app_info.
func PluginConnectorToAppInfo(connectorID string) appserverproto.AppInfo {
	name := connectorID
	installURL := ConnectorInstallURL(name, connectorID)
	return appserverproto.AppInfo{
		ID:                 connectorID,
		Name:               name,
		InstallURL:         &installURL,
		IsAccessible:       false,
		IsEnabled:          true,
		PluginDisplayNames: []string{},
	}
}

// sortDedup sorts a slice and removes adjacent duplicates, mirroring
// sort_unstable + dedup. It returns a nil slice unchanged so AppInfo's
// MarshalJSON emits an empty array (never null) only when intended.
func sortDedup(values []string) []string {
	if len(values) == 0 {
		return values
	}
	sort.Strings(values)
	out := values[:1]
	for _, v := range values[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
