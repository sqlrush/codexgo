package connectors

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

func TestCollectAccessibleConnectorsDedupesAndNormalizes(t *testing.T) {
	tools := []AccessibleConnectorTool{
		{
			ConnectorID:        "calendar",
			ConnectorName:      strptr("  Google Calendar  "),
			PluginDisplayNames: []string{"beta", "alpha"},
		},
		{
			ConnectorID:          "calendar",
			ConnectorName:        nil,
			ConnectorDescription: strptr("Plan events"),
			PluginDisplayNames:   []string{"alpha", "gamma"},
		},
		{
			ConnectorID: "gmail",
		},
	}

	got := CollectAccessibleConnectors(tools)

	calendarInstall := ConnectorInstallURL("Google Calendar", "calendar")
	gmailInstall := ConnectorInstallURL("gmail", "gmail")
	want := []appserverproto.AppInfo{
		{
			ID:                 "calendar",
			Name:               "Google Calendar",
			Description:        strptr("Plan events"),
			InstallURL:         &calendarInstall,
			IsAccessible:       true,
			IsEnabled:          true,
			PluginDisplayNames: []string{"alpha", "beta", "gamma"},
		},
		{
			ID:                 "gmail",
			Name:               "gmail",
			InstallURL:         &gmailInstall,
			IsAccessible:       true,
			IsEnabled:          true,
			PluginDisplayNames: []string{},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCollectAccessibleConnectorsEmptyNameFallsBackToID(t *testing.T) {
	got := CollectAccessibleConnectors([]AccessibleConnectorTool{
		{ConnectorID: "alpha", ConnectorName: strptr("   ")},
	})
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("got %#v, want name fallback to id", got)
	}
}
