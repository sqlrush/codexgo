package features

import (
	"fmt"
	"sort"
)

// LegacyFeatureUsage records that a deprecated/legacy feature key was used,
// along with a user-facing deprecation notice. Mirrors the Rust
// `LegacyFeatureUsage`.
type LegacyFeatureUsage struct {
	// Alias is the legacy key the user supplied.
	Alias string
	// Feature is the canonical feature the alias resolves to.
	Feature Feature
	// Summary is the short deprecation message.
	Summary string
	// Details is the optional longer explanation; nil when absent.
	Details *string
}

// Features holds the effective set of enabled features plus any recorded legacy
// usages. Mirrors the Rust `Features`. The zero value is an empty feature set;
// use NewFeaturesWithDefaults for the built-in defaults.
type Features struct {
	enabled      map[Feature]struct{}
	legacyUsages map[legacyUsageKey]LegacyFeatureUsage
}

// legacyUsageKey is the dedup key for legacy usages. Mirrors the BTreeSet
// uniqueness over the whole LegacyFeatureUsage value, but alias+feature is
// sufficient because summary/details are derived from those.
type legacyUsageKey struct {
	alias   string
	feature Feature
	summary string
	details string
}

// FeatureOverrides carries CLI/explicit overrides applied last in the
// resolution precedence. Mirrors the Rust `FeatureOverrides`.
type FeatureOverrides struct {
	// WebSearchRequest, when non-nil, forces the web_search_request feature.
	WebSearchRequest *bool
}

// FeatureConfigSource is one layer of feature configuration (base or profile).
// Mirrors the Rust `FeatureConfigSource`.
type FeatureConfigSource struct {
	// Features is the optional `[features]` table for this layer.
	Features *FeaturesToml
	// ExperimentalUseUnifiedExecTool is the legacy top-level toggle for this
	// layer.
	ExperimentalUseUnifiedExecTool *bool
}

// apply applies the overrides onto the feature set. Mirrors
// `FeatureOverrides::apply`.
func (o FeatureOverrides) apply(f *Features) {
	if o.WebSearchRequest != nil {
		f.SetEnabled(FeatureWebSearchRequest, *o.WebSearchRequest)
		f.RecordLegacyUsage("web_search_request", FeatureWebSearchRequest)
	}
}

// NewFeaturesWithDefaults returns a Features seeded with the built-in defaults.
// Mirrors `Features::with_defaults`.
func NewFeaturesWithDefaults() Features {
	set := make(map[Feature]struct{})
	for _, spec := range FEATURES {
		if spec.DefaultEnabled {
			set[spec.ID] = struct{}{}
		}
	}
	return Features{
		enabled:      set,
		legacyUsages: map[legacyUsageKey]LegacyFeatureUsage{},
	}
}

// ensureMaps lazily initializes internal maps so the zero value is usable.
func (f *Features) ensureMaps() {
	if f.enabled == nil {
		f.enabled = map[Feature]struct{}{}
	}
	if f.legacyUsages == nil {
		f.legacyUsages = map[legacyUsageKey]LegacyFeatureUsage{}
	}
}

// Enabled reports whether the given feature is enabled.
func (f *Features) Enabled(feature Feature) bool {
	if f.enabled == nil {
		return false
	}
	_, ok := f.enabled[feature]
	return ok
}

// AppsEnabledForAuth reports whether apps are enabled and ChatGPT auth is
// available. Mirrors `Features::apps_enabled_for_auth`.
func (f *Features) AppsEnabledForAuth(hasChatGPTAuth bool) bool {
	return f.Enabled(FeatureApps) && hasChatGPTAuth
}

// UseLegacyLandlock reports whether the legacy Landlock sandbox is enabled.
// Mirrors `Features::use_legacy_landlock`.
func (f *Features) UseLegacyLandlock() bool {
	return f.Enabled(FeatureUseLegacyLandlock)
}

// Enable marks a feature enabled and returns the receiver for chaining. Mirrors
// `Features::enable`.
func (f *Features) Enable(feature Feature) *Features {
	f.ensureMaps()
	f.enabled[feature] = struct{}{}
	return f
}

// Disable marks a feature disabled and returns the receiver for chaining.
// Mirrors `Features::disable`.
func (f *Features) Disable(feature Feature) *Features {
	f.ensureMaps()
	delete(f.enabled, feature)
	return f
}

// SetEnabled enables or disables a feature based on enabled and returns the
// receiver. Mirrors `Features::set_enabled`.
func (f *Features) SetEnabled(feature Feature, enabled bool) *Features {
	if enabled {
		return f.Enable(feature)
	}
	return f.Disable(feature)
}

// RecordLegacyUsageForce records a legacy usage notice unconditionally. Mirrors
// `Features::record_legacy_usage_force`.
func (f *Features) RecordLegacyUsageForce(alias string, feature Feature) {
	f.ensureMaps()
	summary, details := legacyUsageNotice(alias, feature)
	usage := LegacyFeatureUsage{
		Alias:   alias,
		Feature: feature,
		Summary: summary,
		Details: details,
	}
	f.legacyUsages[legacyUsageKeyOf(usage)] = usage
}

// RecordLegacyUsage records a legacy usage notice unless the alias is already
// canonical. Mirrors `Features::record_legacy_usage`.
func (f *Features) RecordLegacyUsage(alias string, feature Feature) {
	if alias == feature.Key() {
		return
	}
	f.RecordLegacyUsageForce(alias, feature)
}

// legacyUsageKeyOf builds the dedup key for a usage.
func legacyUsageKeyOf(u LegacyFeatureUsage) legacyUsageKey {
	details := ""
	if u.Details != nil {
		details = *u.Details
	}
	return legacyUsageKey{alias: u.Alias, feature: u.Feature, summary: u.Summary, details: details}
}

// LegacyFeatureUsages returns the recorded legacy usages sorted by
// (alias, feature, summary, details) to match the Rust BTreeSet iteration
// order. Mirrors `Features::legacy_feature_usages`.
func (f *Features) LegacyFeatureUsages() []LegacyFeatureUsage {
	usages := make([]LegacyFeatureUsage, 0, len(f.legacyUsages))
	for _, u := range f.legacyUsages {
		usages = append(usages, u)
	}
	sort.Slice(usages, func(i, j int) bool {
		return lessLegacyUsage(usages[i], usages[j])
	})
	return usages
}

// lessLegacyUsage orders usages the way the Rust derived Ord does:
// alias, then feature (by registry ordinal), then summary, then details
// (None < Some).
func lessLegacyUsage(a, b LegacyFeatureUsage) bool {
	if a.Alias != b.Alias {
		return a.Alias < b.Alias
	}
	if a.Feature != b.Feature {
		return a.Feature < b.Feature
	}
	if a.Summary != b.Summary {
		return a.Summary < b.Summary
	}
	return lessOptionString(a.Details, b.Details)
}

// lessOptionString orders Option<String> as Rust does: None < Some, then by
// string value.
func lessOptionString(a, b *string) bool {
	switch {
	case a == nil && b == nil:
		return false
	case a == nil:
		return true
	case b == nil:
		return false
	default:
		return *a < *b
	}
}

// MetricsCounter is the minimal interface used to emit feature-state metrics.
// It is satisfied by the otel SessionTelemetry counter; modeled as a small
// interface here to avoid a hard dependency on the telemetry package.
type MetricsCounter interface {
	// Counter increments a counter metric by inc with the given key/value tags.
	Counter(name string, inc int64, tags [][2]string)
}

// EmitMetrics emits a `codex.feature.state` counter for each non-removed
// feature whose enabled state differs from its default. Mirrors
// `Features::emit_metrics`.
func (f *Features) EmitMetrics(otel MetricsCounter) {
	if otel == nil {
		return
	}
	for _, spec := range FEATURES {
		if spec.Stage.Kind == StageRemoved {
			continue
		}
		current := f.Enabled(spec.ID)
		if current != spec.DefaultEnabled {
			otel.Counter("codex.feature.state", 1, [][2]string{
				{"feature", spec.Key},
				{"value", fmt.Sprintf("%t", current)},
			})
		}
	}
}

// ApplyMap applies a map of key -> bool toggles (e.g. from TOML) onto the
// feature set, handling removed/legacy keys exactly as the Rust
// `Features::apply_map`. Keys are processed in sorted order to match BTreeMap
// iteration.
func (f *Features) ApplyMap(m map[string]bool) {
	f.ensureMaps()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		// Special-case bookkeeping mirroring the Rust match on key string.
		switch k {
		case "web_search_request":
			f.RecordLegacyUsageForce("features.web_search_request", FeatureWebSearchRequest)
		case "web_search_cached":
			f.RecordLegacyUsageForce("features.web_search_cached", FeatureWebSearchCached)
		case "tui_app_server", "undo", "js_repl", "js_repl_tools_only",
			"remote_control", "apply_patch_freeform", "tool_search",
			"image_detail_original", "plugin_hooks", "skill_env_var_dependency_prompt":
			continue
		case "use_legacy_landlock":
			f.RecordLegacyUsageForce("features.use_legacy_landlock", FeatureUseLegacyLandlock)
		}

		feat, ok := FeatureForKey(k)
		if !ok {
			// tracing::warn! in Rust; unknown keys are silently ignored here.
			continue
		}
		if feat == FeatureTuiAppServer {
			continue
		}
		if k != feat.Key() {
			f.RecordLegacyUsage(k, feat)
		}
		f.SetEnabled(feat, v)
	}
}

// applyToml applies a FeaturesToml onto the feature set via its resolved
// entries. Mirrors `Features::apply_toml`.
func (f *Features) applyToml(features *FeaturesToml) {
	f.ApplyMap(features.Entries())
}

// FromSources resolves the effective feature set from a base layer, a profile
// layer, and explicit overrides. Resolution precedence is
// overrides > [features] > defaults, with the profile layer applied after the
// base layer. Mirrors `Features::from_sources`.
func FromSources(base, profile FeatureConfigSource, overrides FeatureOverrides) Features {
	features := NewFeaturesWithDefaults()

	for _, source := range []FeatureConfigSource{base, profile} {
		legacyFeatureToggles{
			ExperimentalUseUnifiedExecTool: source.ExperimentalUseUnifiedExecTool,
		}.apply(&features)

		if source.Features != nil {
			features.applyToml(source.Features)
		}
	}

	overrides.apply(&features)
	features.NormalizeDependencies()

	return features
}

// EnabledFeatures returns the enabled features sorted by registry ordinal,
// matching the Rust BTreeSet iteration order. Mirrors
// `Features::enabled_features`.
func (f *Features) EnabledFeatures() []Feature {
	out := make([]Feature, 0, len(f.enabled))
	for feature := range f.enabled {
		out = append(out, feature)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// NormalizeDependencies enables implied features one-way: spawn_csv implies
// collab, code_mode_only implies code_mode. Mirrors
// `Features::normalize_dependencies`.
func (f *Features) NormalizeDependencies() {
	if f.Enabled(FeatureSpawnCsv) && !f.Enabled(FeatureCollab) {
		f.Enable(FeatureCollab)
	}
	if f.Enabled(FeatureCodeModeOnly) && !f.Enabled(FeatureCodeMode) {
		f.Enable(FeatureCodeMode)
	}
}

// Equal reports whether two feature sets have identical enabled features and
// legacy usages. It mirrors the derived PartialEq used by the Rust tests.
func (f *Features) Equal(other *Features) bool {
	if len(f.enabled) != len(other.enabled) {
		return false
	}
	for feature := range f.enabled {
		if _, ok := other.enabled[feature]; !ok {
			return false
		}
	}
	if len(f.legacyUsages) != len(other.legacyUsages) {
		return false
	}
	for k := range f.legacyUsages {
		if _, ok := other.legacyUsages[k]; !ok {
			return false
		}
	}
	return true
}

// FeatureForKey resolves a `[features]` key (canonical or legacy) to a feature.
// Mirrors the free function `feature_for_key`.
func FeatureForKey(key string) (Feature, bool) {
	if spec, ok := specByKey(key); ok {
		return spec.ID, true
	}
	return legacyFeatureForKey(key)
}

// CanonicalFeatureForKey resolves only canonical `[features]` keys to a feature
// (legacy aliases excluded). Mirrors `canonical_feature_for_key`.
func CanonicalFeatureForKey(key string) (Feature, bool) {
	if spec, ok := specByKey(key); ok {
		return spec.ID, true
	}
	return 0, false
}

// IsKnownFeatureKey reports whether the key matches a known feature toggle key
// (canonical or legacy). Mirrors `is_known_feature_key`.
func IsKnownFeatureKey(key string) bool {
	_, ok := FeatureForKey(key)
	return ok
}
