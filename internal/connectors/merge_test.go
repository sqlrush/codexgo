package connectors

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

func strptr(s string) *string { return &s }

func googleCalendarAccessibleConnector(pluginDisplayNames []string) appserverproto.AppInfo {
	return appserverproto.AppInfo{
		ID:                  "calendar",
		Name:                "Google Calendar",
		Description:         strptr("Plan events"),
		LogoURL:             strptr("https://example.com/logo.png"),
		LogoURLDark:         strptr("https://example.com/logo-dark.png"),
		DistributionChannel: strptr("workspace"),
		IsAccessible:        true,
		IsEnabled:           true,
		PluginDisplayNames:  pluginDisplayNames,
	}
}

func TestMergeConnectorsReplacesPlaceholderName(t *testing.T) {
	plugin := PluginConnectorToAppInfo("calendar")
	accessible := googleCalendarAccessibleConnector(nil)

	merged := MergeConnectors([]appserverproto.AppInfo{plugin}, []appserverproto.AppInfo{accessible})

	installURL := ConnectorInstallURL("calendar", "calendar")
	want := []appserverproto.AppInfo{{
		ID:                  "calendar",
		Name:                "Google Calendar",
		Description:         strptr("Plan events"),
		LogoURL:             strptr("https://example.com/logo.png"),
		LogoURLDark:         strptr("https://example.com/logo-dark.png"),
		DistributionChannel: strptr("workspace"),
		InstallURL:          &installURL,
		IsAccessible:        true,
		IsEnabled:           true,
		PluginDisplayNames:  []string{},
	}}
	if !reflect.DeepEqual(merged, want) {
		t.Fatalf("merged = %#v, want %#v", merged, want)
	}
	if slug := ConnectorMentionSlug(&merged[0]); slug != "google-calendar" {
		t.Errorf("mention slug = %q, want google-calendar", slug)
	}
}

func TestMergeConnectorsUnionsAndDedupesPluginDisplayNames(t *testing.T) {
	plugin := PluginConnectorToAppInfo("calendar")
	plugin.PluginDisplayNames = []string{"sample", "alpha", "sample"}
	accessible := googleCalendarAccessibleConnector([]string{"beta", "alpha"})

	merged := MergeConnectors([]appserverproto.AppInfo{plugin}, []appserverproto.AppInfo{accessible})

	installURL := ConnectorInstallURL("calendar", "calendar")
	want := []appserverproto.AppInfo{{
		ID:                  "calendar",
		Name:                "Google Calendar",
		Description:         strptr("Plan events"),
		LogoURL:             strptr("https://example.com/logo.png"),
		LogoURLDark:         strptr("https://example.com/logo-dark.png"),
		DistributionChannel: strptr("workspace"),
		InstallURL:          &installURL,
		IsAccessible:        true,
		IsEnabled:           true,
		PluginDisplayNames:  []string{"alpha", "beta", "sample"},
	}}
	if !reflect.DeepEqual(merged, want) {
		t.Fatalf("merged = %#v, want %#v", merged, want)
	}
}

func TestMergePluginConnectorsAddsPlaceholders(t *testing.T) {
	existing := namedApp("alpha", "Alpha")
	merged := MergePluginConnectors([]appserverproto.AppInfo{existing}, []string{"alpha", "beta"})

	ids := make([]string, len(merged))
	for i, c := range merged {
		ids[i] = c.ID
	}
	// Sorted by accessibility (all false) then name: Alpha, beta.
	if !reflect.DeepEqual(ids, []string{"alpha", "beta"}) {
		t.Fatalf("ids = %v, want [alpha beta]", ids)
	}
}

func TestMergePluginConnectorsWithAccessibleFiltersToAccessible(t *testing.T) {
	accessible := namedApp("alpha", "Alpha")
	accessible.IsAccessible = true

	merged := MergePluginConnectorsWithAccessible(
		[]string{"alpha", "not-accessible"},
		[]appserverproto.AppInfo{accessible},
	)

	if len(merged) != 1 || merged[0].ID != "alpha" || !merged[0].IsAccessible {
		t.Fatalf("merged = %#v, want single accessible alpha", merged)
	}
}
