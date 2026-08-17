package features

import (
	"reflect"
	"testing"
)

func TestMultiAgentV2DeserializesBooleanToggle(t *testing.T) {
	ft, err := ParseFeaturesToml([]byte("multi_agent_v2 = true\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	entries := ft.Entries()
	want := map[string]bool{"multi_agent_v2": true}
	if !reflect.DeepEqual(entries, want) {
		t.Errorf("entries = %v, want %v", entries, want)
	}
	if ft.MultiAgentV2 == nil {
		t.Fatal("MultiAgentV2 should be set")
	}
	if b, ok := ft.MultiAgentV2.BoolValue(); !ok || !b {
		t.Errorf("expected Enabled(true) form, got ok=%v b=%v", ok, b)
	}
}

func TestMultiAgentV2DeserializesTable(t *testing.T) {
	src := `
[multi_agent_v2]
enabled = true
max_concurrent_threads_per_session = 4
min_wait_timeout_ms = 2500
max_wait_timeout_ms = 120000
default_wait_timeout_ms = 30000
usage_hint_enabled = false
usage_hint_text = "Custom delegation guidance."
root_agent_usage_hint_text = "Root guidance."
subagent_usage_hint_text = "Subagent guidance."
tool_namespace = "agents"
hide_spawn_agent_metadata = true
non_code_mode_only = true
`
	ft, err := ParseFeaturesToml([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	entries := ft.Entries()
	if !reflect.DeepEqual(entries, map[string]bool{"multi_agent_v2": true}) {
		t.Errorf("entries = %v", entries)
	}
	cfg, ok := ft.MultiAgentV2.Config()
	if !ok {
		t.Fatal("expected config form")
	}
	want := &MultiAgentV2ConfigToml{
		EnabledFlag:                    boolPtr(true),
		MaxConcurrentThreadsPerSession: u64(4),
		MinWaitTimeoutMs:               i64(2500),
		MaxWaitTimeoutMs:               i64(120000),
		DefaultWaitTimeoutMs:           i64(30000),
		UsageHintEnabled:               boolPtr(false),
		UsageHintText:                  strPtr("Custom delegation guidance."),
		RootAgentUsageHintText:         strPtr("Root guidance."),
		SubagentUsageHintText:          strPtr("Subagent guidance."),
		ToolNamespace:                  strPtr("agents"),
		HideSpawnAgentMetadata:         boolPtr(true),
		NonCodeModeOnly:                boolPtr(true),
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("config = %+v, want %+v", cfg, want)
	}
}

func TestMultiAgentV2UsageHintDoesNotEnableFeature(t *testing.T) {
	src := "[multi_agent_v2]\nusage_hint_enabled = false\n"
	ft, err := ParseFeaturesToml([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	resolved := FromSources(
		FeatureConfigSource{Features: &ft},
		FeatureConfigSource{},
		FeatureOverrides{},
	)
	if resolved.Enabled(FeatureMultiAgentV2) {
		t.Error("multi_agent_v2 should not be enabled without enabled flag")
	}
	if entries := ft.Entries(); len(entries) != 0 {
		t.Errorf("entries should be empty, got %v", entries)
	}
	cfg, ok := ft.MultiAgentV2.Config()
	if !ok {
		t.Fatal("expected config form")
	}
	if cfg.EnabledFlag != nil {
		t.Error("EnabledFlag should be nil")
	}
	if cfg.UsageHintEnabled == nil || *cfg.UsageHintEnabled != false {
		t.Errorf("UsageHintEnabled = %v", cfg.UsageHintEnabled)
	}
}

func TestNetworkProxyTableParses(t *testing.T) {
	src := `
[network_proxy]
enabled = true
proxy_url = "http://127.0.0.1:43128"
mode = "limited"
[network_proxy.domains]
"example.com" = "allow"
"blocked.com" = "deny"
`
	ft, err := ParseFeaturesToml([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg, ok := ft.NetworkProxy.Config()
	if !ok {
		t.Fatal("expected config form")
	}
	if cfg.EnabledFlag == nil || !*cfg.EnabledFlag {
		t.Error("network proxy should be enabled")
	}
	if cfg.ProxyURL == nil || *cfg.ProxyURL != "http://127.0.0.1:43128" {
		t.Errorf("proxy_url = %v", cfg.ProxyURL)
	}
	if cfg.Mode == nil || *cfg.Mode != NetworkProxyModeLimited {
		t.Errorf("mode = %v", cfg.Mode)
	}
	if got := cfg.Domains["example.com"]; got != NetworkProxyDomainAllow {
		t.Errorf("example.com = %v", got)
	}
	if got := cfg.Domains["blocked.com"]; got != NetworkProxyDomainDeny {
		t.Errorf("blocked.com = %v", got)
	}
}

func TestAppsMcpPathOverridePathImpliesEnabled(t *testing.T) {
	src := "[apps_mcp_path_override]\npath = \"/custom/path\"\n"
	ft, err := ParseFeaturesToml([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if ft.AppsMcpPathOverride == nil {
		t.Fatal("AppsMcpPathOverride should be set")
	}
	enabled := ft.AppsMcpPathOverride.Enabled()
	if enabled == nil || !*enabled {
		t.Errorf("path should imply enabled=true, got %v", enabled)
	}
	if got := ft.Entries()["apps_mcp_path_override"]; !got {
		t.Error("entries should reflect path-implied enabled")
	}
}

func TestParseFeaturesTomlRejectsUnknownConfigField(t *testing.T) {
	src := "[network_proxy]\nenabled = true\nbogus_field = 1\n"
	if _, err := ParseFeaturesToml([]byte(src)); err == nil {
		t.Error("expected error for unknown field in network_proxy config")
	}
}

func TestParseFeaturesTomlRejectsNonBoolToggle(t *testing.T) {
	src := "shell_tool = \"yes\"\n"
	if _, err := ParseFeaturesToml([]byte(src)); err == nil {
		t.Error("expected error for non-bool toggle")
	}
}

func TestMaterializeResolvedEnabledWritesAllAndPreservesCustomConfig(t *testing.T) {
	features := NewFeaturesWithDefaults()
	features.Enable(FeatureCodeMode)
	features.Enable(FeatureMultiAgentV2)
	features.Enable(FeatureNetworkProxy)

	maCfg := NewFeatureTomlConfig[*MultiAgentV2ConfigToml](&MultiAgentV2ConfigToml{
		EnabledFlag:      boolPtr(false),
		MinWaitTimeoutMs: i64(2500),
	})
	npCfg := NewFeatureTomlConfig[*NetworkProxyConfigToml](&NetworkProxyConfigToml{
		EnabledFlag: boolPtr(false),
		ProxyURL:    strPtr("http://127.0.0.1:43128"),
	})
	ft := FeaturesToml{
		MultiAgentV2: &maCfg,
		NetworkProxy: &npCfg,
		entries:      map[string]bool{},
	}

	ft.MaterializeResolvedEnabled(&features)

	entries := ft.Entries()
	for _, spec := range FEATURES {
		got, ok := entries[spec.Key]
		if !ok {
			t.Errorf("entry %q missing", spec.Key)
			continue
		}
		if got != features.Enabled(spec.ID) {
			t.Errorf("entry %q = %v, want %v", spec.Key, got, features.Enabled(spec.ID))
		}
	}

	maResult, _ := ft.MultiAgentV2.Config()
	if maResult.EnabledFlag == nil || !*maResult.EnabledFlag {
		t.Error("multi_agent_v2 enabled flag should be true after materialize")
	}
	if maResult.MinWaitTimeoutMs == nil || *maResult.MinWaitTimeoutMs != 2500 {
		t.Error("multi_agent_v2 min_wait_timeout_ms should be preserved")
	}
	npResult, _ := ft.NetworkProxy.Config()
	if npResult.EnabledFlag == nil || !*npResult.EnabledFlag {
		t.Error("network_proxy enabled flag should be true after materialize")
	}
	if npResult.ProxyURL == nil || *npResult.ProxyURL != "http://127.0.0.1:43128" {
		t.Error("network_proxy proxy_url should be preserved")
	}

	replayed := FromSources(
		FeatureConfigSource{Features: &ft},
		FeatureConfigSource{},
		FeatureOverrides{},
	)
	if replayed.Enabled(FeatureApplyPatchFreeform) {
		t.Error("replayed apply_patch_freeform should remain false")
	}
}

func TestMaterializeDropsLegacyKeys(t *testing.T) {
	features := NewFeaturesWithDefaults()
	ft := NewFeaturesTomlFromEntries(map[string]bool{
		"collab":    true,
		"telepathy": true,
	})
	ft.MaterializeResolvedEnabled(&features)
	entries := ft.entries
	for _, legacy := range LegacyFeatureKeys() {
		if _, ok := entries[legacy]; ok {
			t.Errorf("legacy key %q should be dropped", legacy)
		}
	}
}

func u64(v uint64) *uint64    { return &v }
func i64(v int64) *int64      { return &v }
func strPtr(s string) *string { return &s }
