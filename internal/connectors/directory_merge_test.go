package connectors

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

func TestMergeDirectoryAppsFoldsMetadataAndBranding(t *testing.T) {
	existing := DirectoryApp{
		ID:   "alpha",
		Name: "", // blank: should be replaced by incoming non-blank name
		Branding: &appserverproto.AppBranding{
			Category: nil, // absent: should be filled from incoming
		},
		AppMetadata: &appserverproto.AppMetadata{
			Version: nil, // absent: should be filled from incoming
		},
	}
	incoming := DirectoryApp{
		ID:          "alpha",
		Name:        "Alpha",
		Description: strptr("desc"),
		LogoURL:     strptr("https://logo"),
		LogoURLDark: strptr("https://logo-dark"),
		Branding: &appserverproto.AppBranding{
			Category:          strptr("calendar"),
			Developer:         strptr("acme"),
			IsDiscoverableApp: true,
		},
		AppMetadata: &appserverproto.AppMetadata{
			Version:                    strptr("1.0"),
			ShowInComposerWhenUnlinked: boolPtrLocal(true),
		},
		Labels: &map[string]string{"k": "v"},
	}

	merged := mergeDirectoryApps([]DirectoryApp{existing, incoming})
	if len(merged) != 1 {
		t.Fatalf("len = %d, want 1", len(merged))
	}
	app := merged[0]
	if app.Name != "Alpha" {
		t.Errorf("name = %q, want Alpha", app.Name)
	}
	if app.Description == nil || *app.Description != "desc" {
		t.Errorf("description = %v", app.Description)
	}
	if app.LogoURL == nil || *app.LogoURL != "https://logo" {
		t.Errorf("logo url = %v", app.LogoURL)
	}
	if app.Branding == nil || app.Branding.Category == nil || *app.Branding.Category != "calendar" {
		t.Errorf("branding category not folded: %#v", app.Branding)
	}
	if !app.Branding.IsDiscoverableApp {
		t.Errorf("discoverable flag should be OR-ed to true")
	}
	if app.AppMetadata == nil || app.AppMetadata.Version == nil || *app.AppMetadata.Version != "1.0" {
		t.Errorf("metadata version not folded: %#v", app.AppMetadata)
	}
	if app.Labels == nil {
		t.Errorf("labels should be folded")
	}
}

func TestMergeDirectoryAppKeepsExistingNonBlankName(t *testing.T) {
	existing := DirectoryApp{ID: "x", Name: "Existing"}
	incoming := DirectoryApp{ID: "x", Name: "Incoming"}
	merged := mergeDirectoryApps([]DirectoryApp{existing, incoming})
	if merged[0].Name != "Existing" {
		t.Errorf("name = %q, want Existing (existing non-blank wins)", merged[0].Name)
	}
}

func TestDirectoryAppUnmarshalAcceptsCamelCaseAliases(t *testing.T) {
	raw := `{
		"id": "alpha",
		"name": "Alpha",
		"appMetadata": {"version": "1.0"},
		"logoUrl": "https://logo",
		"logoUrlDark": "https://logo-dark",
		"distributionChannel": "workspace"
	}`
	var app DirectoryApp
	if err := json.Unmarshal([]byte(raw), &app); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if app.LogoURL == nil || *app.LogoURL != "https://logo" {
		t.Errorf("logoUrl alias not applied: %v", app.LogoURL)
	}
	if app.LogoURLDark == nil || *app.LogoURLDark != "https://logo-dark" {
		t.Errorf("logoUrlDark alias not applied: %v", app.LogoURLDark)
	}
	if app.DistributionChannel == nil || *app.DistributionChannel != "workspace" {
		t.Errorf("distributionChannel alias not applied: %v", app.DistributionChannel)
	}
	if app.AppMetadata == nil || app.AppMetadata.Version == nil || *app.AppMetadata.Version != "1.0" {
		t.Errorf("appMetadata alias not applied: %#v", app.AppMetadata)
	}
}

func TestDirectoryListResponseUnmarshalAcceptsNextTokenAlias(t *testing.T) {
	var resp DirectoryListResponse
	if err := json.Unmarshal([]byte(`{"apps":[],"nextToken":"abc"}`), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.NextToken == nil || *resp.NextToken != "abc" {
		t.Errorf("nextToken alias not applied: %v", resp.NextToken)
	}
}

func TestSanitizeNameAndDisplayHelpers(t *testing.T) {
	if got := SanitizeName("Google Calendar"); got != "google_calendar" {
		t.Errorf("SanitizeName = %q, want google_calendar", got)
	}
	if got := SanitizeName("!!!"); got != "app" {
		t.Errorf("SanitizeName(!!!) = %q, want app", got)
	}
	connector := appserverproto.AppInfo{Name: "My App"}
	if got := ConnectorDisplayLabel(&connector); got != "My App" {
		t.Errorf("ConnectorDisplayLabel = %q", got)
	}
	if got := ConnectorMentionSlugFromName("My App"); got != "my-app" {
		t.Errorf("ConnectorMentionSlugFromName = %q", got)
	}
}

func TestConnectorInstallURLSlugging(t *testing.T) {
	if got := ConnectorInstallURL("  Spaced Name  ", "id1"); got != "https://chatgpt.com/apps/spaced-name/id1" {
		t.Errorf("install url = %q", got)
	}
}

func boolPtrLocal(b bool) *bool { return &b }
