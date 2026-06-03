package features

import "testing"

func TestCodeModeOnlyRequiresCodeMode(t *testing.T) {
	f := NewFeaturesWithDefaults()
	f.Enable(FeatureCodeModeOnly)
	f.NormalizeDependencies()
	if !f.Enabled(FeatureCodeModeOnly) {
		t.Error("code_mode_only should remain enabled")
	}
	if !f.Enabled(FeatureCodeMode) {
		t.Error("code_mode should be implied by code_mode_only")
	}
}

func TestSpawnCsvNormalizationEnablesCollabOneWay(t *testing.T) {
	fanout := NewFeaturesWithDefaults()
	fanout.Disable(FeatureCollab)
	fanout.Enable(FeatureSpawnCsv)
	fanout.NormalizeDependencies()
	if !fanout.Enabled(FeatureSpawnCsv) {
		t.Error("spawn_csv should remain enabled")
	}
	if !fanout.Enabled(FeatureCollab) {
		t.Error("collab should be implied by spawn_csv")
	}

	collab := NewFeaturesWithDefaults()
	collab.Enable(FeatureCollab)
	collab.NormalizeDependencies()
	if !collab.Enabled(FeatureCollab) {
		t.Error("collab should remain enabled")
	}
	if collab.Enabled(FeatureSpawnCsv) {
		t.Error("collab must not imply spawn_csv")
	}
}

func TestAppsRequireFeatureFlagAndChatGPTAuth(t *testing.T) {
	f := NewFeaturesWithDefaults()
	f.Disable(FeatureApps)
	if f.AppsEnabledForAuth(false) {
		t.Error("apps disabled + no auth should be false")
	}
	f.Enable(FeatureApps)
	if f.AppsEnabledForAuth(false) {
		t.Error("apps enabled but no auth should be false")
	}
	if !f.AppsEnabledForAuth(true) {
		t.Error("apps enabled + auth should be true")
	}
}

func TestFromSourcesAppliesBaseProfileAndOverrides(t *testing.T) {
	base := NewFeaturesTomlFromEntries(map[string]bool{"plugins": true})
	profile := NewFeaturesTomlFromEntries(map[string]bool{"code_mode_only": true})

	disabled := false
	f := FromSources(
		FeatureConfigSource{Features: &base},
		FeatureConfigSource{Features: &profile},
		FeatureOverrides{WebSearchRequest: &disabled},
	)

	checks := []struct {
		feature Feature
		want    bool
	}{
		{FeaturePlugins, true},
		{FeatureCodeModeOnly, true},
		{FeatureCodeMode, true},
		{FeatureApplyPatchFreeform, false},
		{FeatureWebSearchRequest, false},
	}
	for _, c := range checks {
		if got := f.Enabled(c.feature); got != c.want {
			t.Errorf("feature %v = %v, want %v", c.feature, got, c.want)
		}
	}
}

func TestFromSourcesIgnoresRemovedFeatureKeys(t *testing.T) {
	tests := []struct {
		name    string
		entries map[string]bool
	}{
		{"image_detail_original", map[string]bool{"image_detail_original": true}},
		{"undo", map[string]bool{"undo": true}},
		{"js_repl", map[string]bool{"js_repl": true, "js_repl_tools_only": true}},
		{"apply_patch_freeform", map[string]bool{"apply_patch_freeform": true}},
		{"plugin_hooks", map[string]bool{"plugin_hooks": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := NewFeaturesTomlFromEntries(tt.entries)
			got := FromSources(
				FeatureConfigSource{Features: &ft},
				FeatureConfigSource{},
				FeatureOverrides{},
			)
			defaults := NewFeaturesWithDefaults()
			if !got.Equal(&defaults) {
				t.Errorf("expected feature set to equal defaults for %s", tt.name)
			}
		})
	}
}

func TestRemoteControlConfigIsIgnored(t *testing.T) {
	f := NewFeaturesWithDefaults()
	f.ApplyMap(map[string]bool{"remote_control": true})
	if f.Enabled(FeatureRemoteControl) {
		t.Error("remote_control config should be ignored")
	}
}

func TestExperimentalUseUnifiedExecToolLegacyToggle(t *testing.T) {
	off := false
	f := FromSources(
		FeatureConfigSource{ExperimentalUseUnifiedExecTool: &off},
		FeatureConfigSource{},
		FeatureOverrides{},
	)
	if f.Enabled(FeatureUnifiedExec) {
		t.Error("experimental_use_unified_exec_tool=false should disable unified_exec")
	}
	usages := f.LegacyFeatureUsages()
	found := false
	for _, u := range usages {
		if u.Alias == "experimental_use_unified_exec_tool" && u.Feature == FeatureUnifiedExec {
			found = true
		}
	}
	if !found {
		t.Error("expected legacy usage for experimental_use_unified_exec_tool")
	}
}

func TestUseLegacyLandlockRecordsDeprecationNotice(t *testing.T) {
	f := NewFeaturesWithDefaults()
	f.ApplyMap(map[string]bool{"use_legacy_landlock": true})

	usages := f.LegacyFeatureUsages()
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	u := usages[0]
	if u.Alias != "features.use_legacy_landlock" {
		t.Errorf("alias = %q", u.Alias)
	}
	if u.Feature != FeatureUseLegacyLandlock {
		t.Errorf("feature = %v", u.Feature)
	}
	if u.Summary != "`[features].use_legacy_landlock` is deprecated and will be removed soon." {
		t.Errorf("summary = %q", u.Summary)
	}
	if u.Details == nil || *u.Details != "Remove this setting to stop opting into the legacy Linux sandbox behavior." {
		t.Errorf("details = %v", u.Details)
	}
}

func TestApplyMapLegacyAliasRecordsUsage(t *testing.T) {
	f := NewFeaturesWithDefaults()
	f.ApplyMap(map[string]bool{"collab": true})
	if !f.Enabled(FeatureCollab) {
		t.Error("collab alias should enable multi_agent")
	}
	usages := f.LegacyFeatureUsages()
	found := false
	for _, u := range usages {
		if u.Alias == "collab" && u.Feature == FeatureCollab {
			found = true
			want := "`[features].collab` is deprecated. Use `[features].multi_agent` instead."
			if u.Summary != want {
				t.Errorf("summary = %q, want %q", u.Summary, want)
			}
		}
	}
	if !found {
		t.Error("expected legacy usage for collab")
	}
}

func TestEnabledFeaturesSortedByOrdinal(t *testing.T) {
	f := Features{enabled: map[Feature]struct{}{}}
	f.Enable(FeatureWorkspaceDependencies)
	f.Enable(FeatureShellTool)
	f.Enable(FeatureCodexHooks)
	got := f.EnabledFeatures()
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("EnabledFeatures not sorted ascending: %v", got)
		}
	}
}

// fakeCounter captures EmitMetrics output for assertions.
type fakeCounter struct {
	calls []metricCall
}

type metricCall struct {
	name string
	inc  int64
	tags [][2]string
}

func (c *fakeCounter) Counter(name string, inc int64, tags [][2]string) {
	c.calls = append(c.calls, metricCall{name: name, inc: inc, tags: tags})
}

func TestEmitMetricsOnlyForNonDefaultNonRemoved(t *testing.T) {
	f := NewFeaturesWithDefaults()
	// Toggle a stable feature off (differs from default true).
	f.Disable(FeatureShellTool)
	// Toggle an under-development feature on (differs from default false).
	f.Enable(FeatureCodeMode)
	// Toggle a removed feature; should be skipped.
	f.Enable(FeatureToolSearch)

	c := &fakeCounter{}
	f.EmitMetrics(c)

	sawShell := false
	sawCodeMode := false
	for _, call := range c.calls {
		if call.name != "codex.feature.state" {
			t.Errorf("unexpected metric name %q", call.name)
		}
		key := call.tags[0][1]
		if key == "shell_tool" {
			sawShell = true
			if call.tags[1][1] != "false" {
				t.Errorf("shell_tool value = %q", call.tags[1][1])
			}
		}
		if key == "code_mode" {
			sawCodeMode = true
			if call.tags[1][1] != "true" {
				t.Errorf("code_mode value = %q", call.tags[1][1])
			}
		}
		if key == "tool_search" {
			t.Error("removed feature tool_search should be skipped")
		}
	}
	if !sawShell || !sawCodeMode {
		t.Errorf("missing metric: shell=%v code_mode=%v", sawShell, sawCodeMode)
	}
}
