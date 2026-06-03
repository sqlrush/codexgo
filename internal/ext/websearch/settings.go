package websearch

import "github.com/sqlrush/codexgo/internal/protocol"

// SearchSettings carries server-side search configuration. Rust: SearchSettings;
// every field uses skip_serializing_if = Option::is_none.
type SearchSettings struct {
	UserLocation      *ApproximateLocation `json:"user_location,omitempty"`
	SearchContextSize *SearchContextSize   `json:"search_context_size,omitempty"`
	Filters           *SearchFilters       `json:"filters,omitempty"`
	ImageSettings     *SearchImageSettings `json:"image_settings,omitempty"`
	AllowedCallers    *[]AllowedCaller     `json:"allowed_callers,omitempty"`
	ExternalWebAccess *bool                `json:"external_web_access,omitempty"`
}

// ApproximateLocation is an approximate user location. Rust: ApproximateLocation.
type ApproximateLocation struct {
	Type     LocationType `json:"type"`
	Country  *string      `json:"country,omitempty"`
	Region   *string      `json:"region,omitempty"`
	City     *string      `json:"city,omitempty"`
	Timezone *string      `json:"timezone,omitempty"`
}

// LocationType is the location type discriminator. Rust: LocationType (lowercase).
type LocationType string

// LocationType variants.
const (
	LocationTypeApproximate LocationType = "approximate"
)

// SearchContextSize bounds how much context the search may use. Rust:
// SearchContextSize (lowercase).
type SearchContextSize string

// SearchContextSize variants.
const (
	SearchContextSizeLow    SearchContextSize = "low"
	SearchContextSizeMedium SearchContextSize = "medium"
	SearchContextSizeHigh   SearchContextSize = "high"
)

// SearchFilters constrains search domains. Rust: SearchFilters.
type SearchFilters struct {
	AllowedDomains *[]string `json:"allowed_domains,omitempty"`
	BlockedDomains *[]string `json:"blocked_domains,omitempty"`
}

// SearchImageSettings configures image search. Rust: SearchImageSettings.
type SearchImageSettings struct {
	MaxResults *uint64 `json:"max_results,omitempty"`
	Caption    *bool   `json:"caption,omitempty"`
}

// AllowedCaller restricts which execution contexts may invoke the tool. Rust:
// AllowedCaller (snake_case).
type AllowedCaller string

// AllowedCaller variants.
const (
	AllowedCallerDirect          AllowedCaller = "direct"
	AllowedCallerShell           AllowedCaller = "shell"
	AllowedCallerCodeInterpreter AllowedCaller = "code_interpreter"
)

// BuildSearchSettings derives the SearchSettings sent with every standalone web
// search request from the resolved config.web_search_config and the active web
// search mode. Rust: search_settings.
func BuildSearchSettings(config *protocol.WebSearchConfig, webSearchMode protocol.WebSearchMode) SearchSettings {
	settings := SearchSettings{
		AllowedCallers:    &[]AllowedCaller{AllowedCallerDirect},
		ExternalWebAccess: boolPtr(webSearchMode == protocol.WebSearchModeLive),
	}
	if config == nil {
		return settings
	}
	if config.UserLocation != nil {
		loc := config.UserLocation
		settings.UserLocation = &ApproximateLocation{
			Type:     LocationTypeApproximate,
			Country:  loc.Country,
			Region:   loc.Region,
			City:     loc.City,
			Timezone: loc.Timezone,
		}
	}
	if config.SearchContextSize != nil {
		settings.SearchContextSize = mapContextSize(*config.SearchContextSize)
	}
	if config.Filters != nil {
		settings.Filters = &SearchFilters{
			AllowedDomains: config.Filters.AllowedDomains,
			BlockedDomains: nil,
		}
	}
	return settings
}

// mapContextSize maps a protocol WebSearchContextSize to the API enum. Rust: the
// match in search_settings.
func mapContextSize(size protocol.WebSearchContextSize) *SearchContextSize {
	switch size {
	case protocol.WebSearchContextSizeLow:
		return ptrContextSize(SearchContextSizeLow)
	case protocol.WebSearchContextSizeMedium:
		return ptrContextSize(SearchContextSizeMedium)
	case protocol.WebSearchContextSizeHigh:
		return ptrContextSize(SearchContextSizeHigh)
	default:
		return nil
	}
}

func ptrContextSize(s SearchContextSize) *SearchContextSize { return &s }

func boolPtr(b bool) *bool { return &b }
