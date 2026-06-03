package connectors

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

func app(id string) appserverproto.AppInfo {
	return appserverproto.AppInfo{
		ID:        id,
		Name:      id,
		IsEnabled: true,
	}
}

func namedApp(id, name string) appserverproto.AppInfo {
	a := app(id)
	a.Name = name
	installURL := ConnectorInstallURL(name, id)
	a.InstallURL = &installURL
	return a
}

func TestFilterDisallowedConnectors(t *testing.T) {
	tests := []struct {
		name       string
		input      []appserverproto.AppInfo
		originator string
		want       []appserverproto.AppInfo
	}{
		{
			name:       "allows non-disallowed connectors",
			input:      []appserverproto.AppInfo{app("asdk_app_hidden"), app("alpha")},
			originator: "codex_cli",
			want:       []appserverproto.AppInfo{app("asdk_app_hidden"), app("alpha")},
		},
		{
			name:       "allows openai prefix",
			input:      []appserverproto.AppInfo{app("connector_openai_foo"), app("connector_openai_bar"), app("gamma")},
			originator: "codex_cli",
			want:       []appserverproto.AppInfo{app("connector_openai_foo"), app("connector_openai_bar"), app("gamma")},
		},
		{
			name: "filters disallowed connector ids",
			input: []appserverproto.AppInfo{
				app("asdk_app_6938a94a61d881918ef32cb999ff937c"),
				app("connector_3f8d1a79f27c4c7ba1a897ab13bf37dc"),
				app("delta"),
			},
			originator: "codex_cli",
			want:       []appserverproto.AppInfo{app("delta")},
		},
		{
			name: "first party chat originator filters target ids",
			input: []appserverproto.AppInfo{
				app("connector_openai_foo"),
				app("asdk_app_6938a94a61d881918ef32cb999ff937c"),
				app("connector_0f9c9d4592e54d0a9a12b3f44a1e2010"),
			},
			originator: "codex_atlas",
			want: []appserverproto.AppInfo{
				app("connector_openai_foo"),
				app("asdk_app_6938a94a61d881918ef32cb999ff937c"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterDisallowedConnectors(tt.input, tt.originator)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterDisallowedConnectors() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterToolSuggestDiscoverableConnectorsKeepsPluginBackedUninstalled(t *testing.T) {
	accessible := namedApp("connector_2128aebfecb84f64a069897515042a44", "Google Calendar")
	accessible.IsAccessible = true

	got := FilterToolSuggestDiscoverableConnectors(
		[]appserverproto.AppInfo{
			namedApp("connector_2128aebfecb84f64a069897515042a44", "Google Calendar"),
			namedApp("connector_68df038e0ba48191908c8434991bbac2", "Gmail"),
			namedApp("connector_other", "Other"),
		},
		[]appserverproto.AppInfo{accessible},
		map[string]struct{}{
			"connector_2128aebfecb84f64a069897515042a44": {},
			"connector_68df038e0ba48191908c8434991bbac2": {},
		},
		"codex_cli",
	)

	want := []appserverproto.AppInfo{namedApp("connector_68df038e0ba48191908c8434991bbac2", "Gmail")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFilterToolSuggestDiscoverableConnectorsExcludesAccessibleEvenDisabled(t *testing.T) {
	calendar := namedApp("connector_2128aebfecb84f64a069897515042a44", "Google Calendar")
	calendar.IsAccessible = true
	gmail := namedApp("connector_68df038e0ba48191908c8434991bbac2", "Gmail")
	gmail.IsAccessible = true
	gmail.IsEnabled = false

	got := FilterToolSuggestDiscoverableConnectors(
		[]appserverproto.AppInfo{
			namedApp("connector_2128aebfecb84f64a069897515042a44", "Google Calendar"),
			namedApp("connector_68df038e0ba48191908c8434991bbac2", "Gmail"),
		},
		[]appserverproto.AppInfo{calendar, gmail},
		map[string]struct{}{
			"connector_2128aebfecb84f64a069897515042a44": {},
			"connector_68df038e0ba48191908c8434991bbac2": {},
		},
		"codex_cli",
	)

	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
