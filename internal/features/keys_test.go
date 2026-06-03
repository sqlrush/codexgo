package features

import "testing"

func TestFeatureForKeyCanonicalAndLegacy(t *testing.T) {
	tests := []struct {
		key  string
		want Feature
		ok   bool
	}{
		{"apply_patch_freeform", FeatureApplyPatchFreeform, true},
		{"plugin_hooks", FeaturePluginHooks, true},
		{"use_legacy_landlock", FeatureUseLegacyLandlock, true},
		{"use_linux_sandbox_bwrap", FeatureUseLinuxSandboxBwrap, true},
		{"image_detail_original", FeatureImageDetailOriginal, true},
		{"js_repl", FeatureJsRepl, true},
		{"js_repl_tools_only", FeatureJsReplToolsOnly, true},
		{"tool_search", FeatureToolSearch, true},
		{"imagegenext", FeatureImageGenExt, true},
		{"auth_elicitation", FeatureAuthElicitation, true},
		{"mentions_v2", FeatureMentionsV2, true},
		{"remote_control", FeatureRemoteControl, true},
		{"workspace_dependencies", FeatureWorkspaceDependencies, true},
		{"remote_compaction_v2", FeatureRemoteCompactionV2, true},
		{"responses_websocket_response_processed", FeatureResponsesWebsocketResponseProcessed, true},
		// Legacy aliases.
		{"chronicle", FeatureChronicle, true},
		{"telepathy", FeatureChronicle, true},
		{"multi_agent", FeatureCollab, true},
		{"collab", FeatureCollab, true},
		{"hooks", FeatureCodexHooks, true},
		{"codex_hooks", FeatureCodexHooks, true},
		{"connectors", FeatureApps, true},
		{"web_search", FeatureWebSearchRequest, true},
		{"request_permissions", FeatureExecPermissionApprovals, true},
		{"experimental_use_unified_exec_tool", FeatureUnifiedExec, true},
		{"enable_experimental_windows_sandbox", FeatureWindowsSandbox, true},
		// Unknown.
		{"does_not_exist", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, ok := FeatureForKey(tt.key)
			if ok != tt.ok {
				t.Fatalf("FeatureForKey(%q) ok=%v, want %v", tt.key, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("FeatureForKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestCanonicalFeatureForKeyExcludesLegacy(t *testing.T) {
	if feat, ok := CanonicalFeatureForKey("multi_agent"); !ok || feat != FeatureCollab {
		t.Errorf("canonical multi_agent = %v, ok=%v", feat, ok)
	}
	if _, ok := CanonicalFeatureForKey("collab"); ok {
		t.Error("legacy alias collab should not resolve via CanonicalFeatureForKey")
	}
	if _, ok := CanonicalFeatureForKey("telepathy"); ok {
		t.Error("legacy alias telepathy should not resolve via CanonicalFeatureForKey")
	}
}

func TestIsKnownFeatureKey(t *testing.T) {
	if !IsKnownFeatureKey("shell_tool") {
		t.Error("shell_tool should be known")
	}
	if !IsKnownFeatureKey("collab") {
		t.Error("legacy collab should be known")
	}
	if IsKnownFeatureKey("totally_unknown") {
		t.Error("unknown key should not be known")
	}
}

func TestLegacyFeatureKeysOrder(t *testing.T) {
	want := []string{
		"connectors",
		"enable_experimental_windows_sandbox",
		"experimental_use_unified_exec_tool",
		"request_permissions",
		"web_search",
		"collab",
		"memory_tool",
		"telepathy",
		"codex_hooks",
	}
	got := LegacyFeatureKeys()
	if len(got) != len(want) {
		t.Fatalf("LegacyFeatureKeys len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("LegacyFeatureKeys[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
