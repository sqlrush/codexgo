package features

import "fmt"

// legacyUsageNotice builds the (summary, details) deprecation notice for a
// legacy alias usage. Mirrors the Rust `legacy_usage_notice`.
func legacyUsageNotice(alias string, feature Feature) (string, *string) {
	canonical := feature.Key()
	switch feature {
	case FeatureWebSearchRequest, FeatureWebSearchCached:
		label := alias
		switch alias {
		case "web_search":
			label = "[features].web_search"
		case "features.web_search_request", "web_search_request":
			label = "[features].web_search_request"
		case "features.web_search_cached", "web_search_cached":
			label = "[features].web_search_cached"
		}
		summary := fmt.Sprintf("`%s` is deprecated because web search is enabled by default.", label)
		details := webSearchDetails
		return summary, &details
	case FeatureUseLegacyLandlock:
		label := alias
		switch alias {
		case "features.use_legacy_landlock", "use_legacy_landlock":
			label = "[features].use_legacy_landlock"
		}
		summary := fmt.Sprintf("`%s` is deprecated and will be removed soon.", label)
		details := "Remove this setting to stop opting into the legacy Linux sandbox behavior."
		return summary, &details
	default:
		label := alias
		if !containsDot(alias) && !startsWithBracket(alias) {
			label = fmt.Sprintf("[features].%s", alias)
		}
		summary := fmt.Sprintf("`%s` is deprecated. Use `[features].%s` instead.", label, canonical)
		if alias == canonical {
			return summary, nil
		}
		details := fmt.Sprintf(
			"Enable it with `--enable %s` or `[features].%s` in config.toml. See https://developers.openai.com/codex/config-basic#feature-flags for details.",
			canonical, canonical,
		)
		return summary, &details
	}
}

// webSearchDetails is the shared web-search deprecation detail string. Mirrors
// `web_search_details`.
const webSearchDetails = "Set `web_search` to `\"live\"`, `\"cached\"`, or `\"disabled\"` at the top level (or under a profile) in config.toml if you want to override it."

// containsDot reports whether s contains a '.' character.
func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

// startsWithBracket reports whether s begins with '['.
func startsWithBracket(s string) bool {
	return len(s) > 0 && s[0] == '['
}
