package features

import (
	"reflect"
	"testing"
)

func TestStageExperimentalAccessorsForNonExperimental(t *testing.T) {
	s := stable()
	if _, ok := s.ExperimentalMenuName(); ok {
		t.Error("stable stage should have no menu name")
	}
	if _, ok := s.ExperimentalMenuDescription(); ok {
		t.Error("stable stage should have no menu description")
	}
	if _, ok := s.ExperimentalAnnouncement(); ok {
		t.Error("stable stage should have no announcement")
	}
}

func TestStageExperimentalAnnouncementPresent(t *testing.T) {
	stage := FeatureMemoryTool.Stage()
	ann, ok := stage.ExperimentalAnnouncement()
	if !ok {
		t.Fatal("memories should have an announcement")
	}
	want := "NEW: Codex can now generate and use memories. Try it now with `/memories`"
	if ann != want {
		t.Errorf("announcement = %q, want %q", ann, want)
	}
}

func TestUseLegacyLandlockAccessor(t *testing.T) {
	f := NewFeaturesWithDefaults()
	if f.UseLegacyLandlock() {
		t.Error("legacy landlock should be off by default")
	}
	f.Enable(FeatureUseLegacyLandlock)
	if !f.UseLegacyLandlock() {
		t.Error("legacy landlock should be on after enable")
	}
}

func TestFeaturesTomlSetEntryIsImmutable(t *testing.T) {
	base := NewFeaturesTomlFromEntries(map[string]bool{"plugins": true})
	updated := base.SetEntry("apps", false)

	if _, ok := base.entries["apps"]; ok {
		t.Error("SetEntry must not mutate the receiver")
	}
	if v, ok := updated.entries["apps"]; !ok || v {
		t.Error("SetEntry should set the new key on the copy")
	}
	if v, ok := updated.entries["plugins"]; !ok || !v {
		t.Error("SetEntry should preserve existing entries")
	}
}

func TestSortedEntryKeys(t *testing.T) {
	ft := NewFeaturesTomlFromEntries(map[string]bool{
		"plugins":    true,
		"apps":       true,
		"shell_tool": false,
	})
	got := ft.SortedEntryKeys()
	want := []string{"apps", "plugins", "shell_tool"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SortedEntryKeys = %v, want %v", got, want)
	}
}

func TestFeatureTomlConfigAndBoolAccessors(t *testing.T) {
	enabled := NewFeatureTomlEnabled[*MultiAgentV2ConfigToml](true)
	if enabled.IsConfig() {
		t.Error("bool form should not be config")
	}
	if _, ok := enabled.Config(); ok {
		t.Error("bool form should not return config")
	}
	if b, ok := enabled.BoolValue(); !ok || !b {
		t.Errorf("BoolValue = %v, ok=%v", b, ok)
	}
	if e := enabled.Enabled(); e == nil || !*e {
		t.Errorf("Enabled = %v", e)
	}
	enabled.SetEnabled(false)
	if b, _ := enabled.BoolValue(); b {
		t.Error("SetEnabled(false) on bool form should clear value")
	}

	cfg := NewFeatureTomlConfig[*MultiAgentV2ConfigToml](&MultiAgentV2ConfigToml{})
	if !cfg.IsConfig() {
		t.Error("config form should report IsConfig")
	}
	if _, ok := cfg.BoolValue(); ok {
		t.Error("config form should not return a bool value")
	}
	cfg.SetEnabled(true)
	if e := cfg.Enabled(); e == nil || !*e {
		t.Errorf("config Enabled after SetEnabled(true) = %v", e)
	}
}

func TestAppsMcpPathOverrideSetEnabled(t *testing.T) {
	c := &AppsMcpPathOverrideConfigToml{}
	c.SetEnabled(true)
	if c.EnabledFlag == nil || !*c.EnabledFlag {
		t.Error("SetEnabled(true) should set EnabledFlag")
	}
	if e := c.Enabled(); e == nil || !*e {
		t.Errorf("Enabled = %v", e)
	}
}

func TestLegacyUsageOrdering(t *testing.T) {
	f := NewFeaturesWithDefaults()
	// Record two distinct legacy usages; expect sorted by alias.
	f.RecordLegacyUsage("collab", FeatureCollab)
	f.RecordLegacyUsage("telepathy", FeatureChronicle)
	usages := f.LegacyFeatureUsages()
	if len(usages) != 2 {
		t.Fatalf("expected 2 usages, got %d", len(usages))
	}
	if usages[0].Alias != "collab" || usages[1].Alias != "telepathy" {
		t.Errorf("usages not sorted by alias: %v, %v", usages[0].Alias, usages[1].Alias)
	}
}

func TestEqualDetectsDifferences(t *testing.T) {
	a := NewFeaturesWithDefaults()
	b := NewFeaturesWithDefaults()
	if !a.Equal(&b) {
		t.Error("two default sets should be equal")
	}
	b.Enable(FeatureCodeMode)
	if a.Equal(&b) {
		t.Error("differing enabled sets should not be equal")
	}
	b.Disable(FeatureCodeMode)
	b.RecordLegacyUsage("collab", FeatureCollab)
	if a.Equal(&b) {
		t.Error("differing legacy usages should not be equal")
	}
}

func TestEmitMetricsNilCounterIsNoop(t *testing.T) {
	f := NewFeaturesWithDefaults()
	f.EmitMetrics(nil) // must not panic
}
