package features

// legacyAlias maps a legacy `[features]` key to its canonical feature.
type legacyAlias struct {
	LegacyKey string
	Feature   Feature
}

// legacyAliases mirrors the Rust ALIASES table, in the same order so
// LegacyFeatureKeys iterates deterministically.
var legacyAliases = []legacyAlias{
	{LegacyKey: "connectors", Feature: FeatureApps},
	{LegacyKey: "enable_experimental_windows_sandbox", Feature: FeatureWindowsSandbox},
	{LegacyKey: "experimental_use_unified_exec_tool", Feature: FeatureUnifiedExec},
	{LegacyKey: "request_permissions", Feature: FeatureExecPermissionApprovals},
	{LegacyKey: "web_search", Feature: FeatureWebSearchRequest},
	{LegacyKey: "collab", Feature: FeatureCollab},
	{LegacyKey: "memory_tool", Feature: FeatureMemoryTool},
	{LegacyKey: "telepathy", Feature: FeatureChronicle},
	{LegacyKey: "codex_hooks", Feature: FeatureCodexHooks},
}

// LegacyFeatureKeys returns the legacy alias keys in registry order. Mirrors
// `legacy::legacy_feature_keys`.
func LegacyFeatureKeys() []string {
	keys := make([]string, 0, len(legacyAliases))
	for _, alias := range legacyAliases {
		keys = append(keys, alias.LegacyKey)
	}
	return keys
}

// legacyFeatureForKey resolves a legacy alias key to its feature. Mirrors
// `legacy::feature_for_key` (without the tracing side effect).
func legacyFeatureForKey(key string) (Feature, bool) {
	for _, alias := range legacyAliases {
		if alias.LegacyKey == key {
			return alias.Feature, true
		}
	}
	return 0, false
}

// legacyFeatureToggles holds legacy top-level boolean toggles that map onto
// features. Mirrors the Rust `LegacyFeatureToggles`.
type legacyFeatureToggles struct {
	// ExperimentalUseUnifiedExecTool, when set, toggles FeatureUnifiedExec.
	ExperimentalUseUnifiedExecTool *bool
}

// apply applies the legacy toggles onto the feature set, recording legacy usage
// notices. Mirrors `LegacyFeatureToggles::apply`.
func (t legacyFeatureToggles) apply(f *Features) {
	setIfSome(f, FeatureUnifiedExec, t.ExperimentalUseUnifiedExecTool, "experimental_use_unified_exec_tool")
}

// setIfSome toggles a feature when maybeValue is non-nil and records the legacy
// usage. Mirrors the Rust `set_if_some`.
func setIfSome(f *Features, feature Feature, maybeValue *bool, aliasKey string) {
	if maybeValue == nil {
		return
	}
	f.SetEnabled(feature, *maybeValue)
	f.RecordLegacyUsage(aliasKey, feature)
}
