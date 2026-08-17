package features

import "testing"

func TestUnderDevelopmentFeaturesDisabledByDefault(t *testing.T) {
	for _, spec := range FEATURES {
		if spec.Stage.Kind == StageUnderDevelopment && spec.DefaultEnabled {
			t.Errorf("feature %q is under development and must be disabled by default", spec.Key)
		}
	}
}

func TestDefaultEnabledFeaturesAreStable(t *testing.T) {
	for _, spec := range FEATURES {
		if !spec.DefaultEnabled {
			continue
		}
		ok := spec.Stage.Kind == StageStable ||
			spec.Stage.Kind == StageRemoved ||
			spec.ID == FeatureTerminalResizeReflow
		if !ok {
			t.Errorf("feature %q is enabled by default but not stable/removed (kind=%d)", spec.Key, spec.Stage.Kind)
		}
	}
}

func TestStageMetadataAndDefaults(t *testing.T) {
	tests := []struct {
		name        string
		feature     Feature
		wantStage   StageKind
		wantDefault bool
	}{
		{"use_legacy_landlock", FeatureUseLegacyLandlock, StageDeprecated, false},
		{"use_linux_sandbox_bwrap", FeatureUseLinuxSandboxBwrap, StageRemoved, false},
		{"undo", FeatureGhostCommit, StageRemoved, false},
		{"image_detail_original", FeatureImageDetailOriginal, StageRemoved, false},
		{"apply_patch_freeform", FeatureApplyPatchFreeform, StageRemoved, false},
		{"plugin_hooks", FeaturePluginHooks, StageRemoved, false},
		{"guardian_approval", FeatureGuardianApproval, StageStable, true},
		{"exec_permission_approvals", FeatureExecPermissionApprovals, StageUnderDevelopment, false},
		{"request_permissions_tool", FeatureRequestPermissionsTool, StageUnderDevelopment, false},
		{"remote_compaction_v2", FeatureRemoteCompactionV2, StageUnderDevelopment, false},
		{"responses_websocket_response_processed", FeatureResponsesWebsocketResponseProcessed, StageUnderDevelopment, false},
		{"tool_suggest", FeatureToolSuggest, StageStable, true},
		{"tool_search", FeatureToolSearch, StageRemoved, false},
		{"in_app_browser", FeatureInAppBrowser, StageStable, true},
		{"browser_use", FeatureBrowserUse, StageStable, true},
		{"browser_use_external", FeatureBrowserUseExternal, StageStable, true},
		{"computer_use", FeatureComputerUse, StageStable, true},
		{"image_generation", FeatureImageGeneration, StageStable, true},
		{"imagegenext", FeatureImageGenExt, StageUnderDevelopment, false},
		{"tool_call_mcp_elicitation", FeatureToolCallMcpElicitation, StageStable, true},
		{"auth_elicitation", FeatureAuthElicitation, StageUnderDevelopment, false},
		{"mentions_v2", FeatureMentionsV2, StageUnderDevelopment, false},
		{"remote_control", FeatureRemoteControl, StageRemoved, false},
		{"workspace_dependencies", FeatureWorkspaceDependencies, StageStable, true},
		{"chronicle", FeatureChronicle, StageUnderDevelopment, false},
		{"multi_agent", FeatureCollab, StageStable, true},
		{"enable_fanout", FeatureSpawnCsv, StageUnderDevelopment, false},
		{"js_repl", FeatureJsRepl, StageRemoved, false},
		{"js_repl_tools_only", FeatureJsReplToolsOnly, StageRemoved, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.feature.Stage().Kind; got != tt.wantStage {
				t.Errorf("stage kind = %d, want %d", got, tt.wantStage)
			}
			if got := tt.feature.DefaultEnabled(); got != tt.wantDefault {
				t.Errorf("default enabled = %v, want %v", got, tt.wantDefault)
			}
		})
	}
}

func TestExternalMigrationExperimentalMetadata(t *testing.T) {
	stage := FeatureExternalMigration.Stage()
	if stage.Kind != StageExperimental {
		t.Fatalf("expected experimental stage, got %d", stage.Kind)
	}
	if name, ok := stage.ExperimentalMenuName(); !ok || name != "External migration" {
		t.Errorf("menu name = %q, ok=%v", name, ok)
	}
	wantDesc := "Show a startup prompt when Codex detects migratable external agent config for this machine or project."
	if desc, ok := stage.ExperimentalMenuDescription(); !ok || desc != wantDesc {
		t.Errorf("menu description = %q, ok=%v", desc, ok)
	}
	if ann, ok := stage.ExperimentalAnnouncement(); ok {
		t.Errorf("announcement should be absent, got %q", ann)
	}
	if FeatureExternalMigration.DefaultEnabled() {
		t.Error("external_migration should be disabled by default")
	}
}

func TestTerminalResizeReflowExperimentalEnabledByDefault(t *testing.T) {
	if feat, ok := FeatureForKey("terminal_resize_reflow"); !ok || feat != FeatureTerminalResizeReflow {
		t.Fatalf("feature_for_key terminal_resize_reflow = %v, ok=%v", feat, ok)
	}
	if FeatureTerminalResizeReflow.Stage().Kind != StageExperimental {
		t.Error("terminal_resize_reflow should be experimental")
	}
	if !FeatureTerminalResizeReflow.DefaultEnabled() {
		t.Error("terminal_resize_reflow should be enabled by default")
	}
}

func TestNetworkProxyExperimentalDisabledByDefault(t *testing.T) {
	if feat, ok := FeatureForKey("network_proxy"); !ok || feat != FeatureNetworkProxy {
		t.Fatalf("feature_for_key network_proxy = %v, ok=%v", feat, ok)
	}
	if FeatureNetworkProxy.Stage().Kind != StageExperimental {
		t.Error("network_proxy should be experimental")
	}
	if FeatureNetworkProxy.DefaultEnabled() {
		t.Error("network_proxy should be disabled by default")
	}
}
