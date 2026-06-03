package websearch

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestBuildSearchSettingsLiveMode(t *testing.T) {
	got := BuildSearchSettings(nil, protocol.WebSearchModeLive)
	want := SearchSettings{
		AllowedCallers:    &[]AllowedCaller{AllowedCallerDirect},
		ExternalWebAccess: boolPtr(true),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestBuildSearchSettingsCachedModeDisablesExternalAccess(t *testing.T) {
	got := BuildSearchSettings(nil, protocol.WebSearchModeCached)
	if got.ExternalWebAccess == nil || *got.ExternalWebAccess {
		t.Errorf("external web access = %v, want false", got.ExternalWebAccess)
	}
}

func TestBuildSearchSettingsMapsConfig(t *testing.T) {
	size := protocol.WebSearchContextSizeHigh
	config := &protocol.WebSearchConfig{
		UserLocation: &protocol.WebSearchUserLocation{
			Type:    protocol.WebSearchUserLocationTypeApproximate,
			Country: strptr("US"),
			City:    strptr("SF"),
		},
		SearchContextSize: &size,
		Filters: &protocol.WebSearchFilters{
			AllowedDomains: &[]string{"example.com"},
		},
	}
	got := BuildSearchSettings(config, protocol.WebSearchModeLive)

	if got.UserLocation == nil || got.UserLocation.Type != LocationTypeApproximate ||
		got.UserLocation.Country == nil || *got.UserLocation.Country != "US" {
		t.Errorf("user location mismatch: %#v", got.UserLocation)
	}
	if got.SearchContextSize == nil || *got.SearchContextSize != SearchContextSizeHigh {
		t.Errorf("context size mismatch: %#v", got.SearchContextSize)
	}
	if got.Filters == nil || got.Filters.AllowedDomains == nil ||
		!reflect.DeepEqual(*got.Filters.AllowedDomains, []string{"example.com"}) {
		t.Errorf("filters mismatch: %#v", got.Filters)
	}
	if got.Filters.BlockedDomains != nil {
		t.Errorf("blocked domains should be nil")
	}
}

func TestSearchSettingsMarshalOmitsEmpty(t *testing.T) {
	settings := BuildSearchSettings(nil, protocol.WebSearchModeLive)
	got, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"allowed_callers":["direct"],"external_web_access":true}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
