package appserverproto

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// roundTrip marshals val, asserts the JSON equals want (key-order independent),
// then unmarshals into a fresh dst and re-marshals to verify a stable round-trip.
// dst must be a pointer to the same concrete type as val.
func roundTripInto(t *testing.T, val any, want string, dst any) {
	t.Helper()
	b, err := json.Marshal(val)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !jsonEqual(t, b, []byte(want)) {
		t.Fatalf("marshal = %s, want %s", b, want)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	reb, err := json.Marshal(dst)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !jsonEqual(t, reb, []byte(want)) {
		t.Fatalf("round-trip = %s, want %s", reb, want)
	}
}

// -----------------------------------------------------------------------------
// config.rs
// -----------------------------------------------------------------------------

func TestConfigLayerSourceRoundTrip(t *testing.T) {
	mdmDomain, mdmKey := "com.openai.codex", "config"
	sysFile := protocol.AbsolutePath("/etc/codex/config.toml")
	userFile := protocol.AbsolutePath("/home/u/.codex/config.toml")
	profile := "work"
	proj := protocol.AbsolutePath("/repo/.codex")
	legacy := protocol.AbsolutePath("/etc/managed_config.toml")

	cases := []struct {
		name string
		val  ConfigLayerSource
		want string
	}{
		{
			"mdm",
			ConfigLayerSource{Kind: ConfigLayerSourceMdm, Domain: &mdmDomain, Key: &mdmKey},
			`{"type":"mdm","domain":"com.openai.codex","key":"config"}`,
		},
		{
			"system",
			ConfigLayerSource{Kind: ConfigLayerSourceSystem, File: &sysFile},
			`{"type":"system","file":"/etc/codex/config.toml"}`,
		},
		{
			"user-with-profile",
			ConfigLayerSource{Kind: ConfigLayerSourceUser, File: &userFile, Profile: &profile},
			`{"type":"user","file":"/home/u/.codex/config.toml","profile":"work"}`,
		},
		{
			"user-null-profile",
			ConfigLayerSource{Kind: ConfigLayerSourceUser, File: &userFile},
			`{"type":"user","file":"/home/u/.codex/config.toml","profile":null}`,
		},
		{
			"project",
			ConfigLayerSource{Kind: ConfigLayerSourceProject, DotCodexFolder: &proj},
			`{"type":"project","dotCodexFolder":"/repo/.codex"}`,
		},
		{
			"sessionFlags",
			ConfigLayerSource{Kind: ConfigLayerSourceSessionFlags},
			`{"type":"sessionFlags"}`,
		},
		{
			"legacyFromFile",
			ConfigLayerSource{Kind: ConfigLayerSourceLegacyManagedConfigTomlFromFile, File: &legacy},
			`{"type":"legacyManagedConfigTomlFromFile","file":"/etc/managed_config.toml"}`,
		},
		{
			"legacyFromMdm",
			ConfigLayerSource{Kind: ConfigLayerSourceLegacyManagedConfigTomlFromMdm},
			`{"type":"legacyManagedConfigTomlFromMdm"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roundTripInto(t, tc.val, tc.want, new(ConfigLayerSource))
		})
	}
}

func TestConfigLayerSourceErrors(t *testing.T) {
	if _, err := json.Marshal(ConfigLayerSource{}); err == nil {
		t.Fatal("expected error marshaling empty-kind ConfigLayerSource")
	}
	var c ConfigLayerSource
	if err := json.Unmarshal([]byte(`{"type":"bogus"}`), &c); err == nil {
		t.Fatal("expected error decoding unknown ConfigLayerSource type")
	}
}

func TestSandboxWorkspaceWriteAlwaysEmitsArray(t *testing.T) {
	roundTripInto(t, SandboxWorkspaceWrite{}, // nil WritableRoots -> []
		`{"writable_roots":[],"network_access":false,"exclude_tmpdir_env_var":false,"exclude_slash_tmp":false}`,
		new(SandboxWorkspaceWrite))
	roundTripInto(t, SandboxWorkspaceWrite{WritableRoots: []string{"/a", "/b"}, NetworkAccess: true},
		`{"writable_roots":["/a","/b"],"network_access":true,"exclude_tmpdir_env_var":false,"exclude_slash_tmp":false}`,
		new(SandboxWorkspaceWrite))
}

func TestAnalyticsConfigFlatten(t *testing.T) {
	roundTripInto(t,
		AnalyticsConfig{
			Enabled:    boolPtr(true),
			Additional: map[string]json.RawMessage{"extra": json.RawMessage(`"v"`)},
		},
		`{"enabled":true,"extra":"v"}`,
		new(AnalyticsConfig))
	// Absent enabled is still emitted as null (Option with no skip).
	roundTripInto(t, AnalyticsConfig{}, `{"enabled":null}`, new(AnalyticsConfig))
}

func TestAppToolsConfigFlatten(t *testing.T) {
	auto := AppToolApprovalAuto
	roundTripInto(t,
		AppToolsConfig{Tools: map[string]AppToolConfig{
			"search": {Enabled: boolPtr(true), ApprovalMode: &auto},
		}},
		`{"search":{"enabled":true,"approval_mode":"auto"}}`,
		new(AppToolsConfig))
	// Empty map serializes as an empty object.
	roundTripInto(t, AppToolsConfig{}, `{}`, new(AppToolsConfig))
}

func TestAppsConfigFlatten(t *testing.T) {
	roundTripInto(t,
		AppsConfig{
			Default: &AppsDefaultConfig{Enabled: true, DestructiveEnabled: false, OpenWorldEnabled: true},
			Apps: map[string]AppConfig{
				"my-app": {Enabled: true, DestructiveEnabled: boolPtr(false)},
			},
		},
		`{"_default":{"enabled":true,"destructive_enabled":false,"open_world_enabled":true},`+
			`"my-app":{"enabled":true,"destructive_enabled":false,"open_world_enabled":null,`+
			`"default_tools_approval_mode":null,"default_tools_enabled":null,"tools":null}}`,
		new(AppsConfig))
	// Nil default still emits _default:null.
	roundTripInto(t, AppsConfig{}, `{"_default":null}`, new(AppsConfig))
}

func TestForcedChatgptWorkspaceIdsUntagged(t *testing.T) {
	roundTripInto(t, ForcedChatgptWorkspaceIds{Single: strPtr("ws-1")}, `"ws-1"`, new(ForcedChatgptWorkspaceIds))
	roundTripInto(t, ForcedChatgptWorkspaceIds{Multiple: []string{"a", "b"}}, `["a","b"]`, new(ForcedChatgptWorkspaceIds))

	// IntoVec normalizes both shapes.
	if got := (ForcedChatgptWorkspaceIds{Single: strPtr("x")}).IntoVec(); len(got) != 1 || got[0] != "x" {
		t.Fatalf("IntoVec single = %v", got)
	}
	if got := (ForcedChatgptWorkspaceIds{Multiple: []string{"x", "y"}}).IntoVec(); len(got) != 2 {
		t.Fatalf("IntoVec multiple = %v", got)
	}
}

func TestConfigMinimalAndAdditional(t *testing.T) {
	// A fully-empty Config still emits every named Option key as null, plus the
	// snake_case keys, and no flattened extras.
	want := `{"model":null,"review_model":null,"model_context_window":null,` +
		`"model_auto_compact_token_limit":null,"model_auto_compact_token_limit_scope":null,` +
		`"model_provider":null,"approval_policy":null,"approvals_reviewer":null,` +
		`"sandbox_mode":null,"sandbox_workspace_write":null,"forced_chatgpt_workspace_id":null,` +
		`"forced_login_method":null,"web_search":null,"tools":null,"instructions":null,` +
		`"developer_instructions":null,"compact_prompt":null,"model_reasoning_effort":null,` +
		`"model_reasoning_summary":null,"model_verbosity":null,"service_tier":null,` +
		`"analytics":null,"apps":null,"desktop":null}`
	roundTripInto(t, Config{}, want, new(Config))
}

func TestConfigPopulatedWithExtras(t *testing.T) {
	effort := protocol.ReasoningEffortHigh
	cfg := Config{
		Model:                schemaModel(),
		ApprovalPolicy:       &AskForApprovalV2{Kind: AskForApprovalV2OnRequest},
		SandboxMode:          sandboxPtr(SandboxModeV2WorkspaceWrite),
		WebSearch:            webSearchPtr(protocol.WebSearchModeLive),
		ModelReasoningEffort: &effort,
		Additional: map[string]json.RawMessage{
			"custom_key": json.RawMessage(`{"nested":1}`),
		},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal map: %v", err)
	}
	if string(got["model"]) != `"gpt-5"` {
		t.Fatalf("model = %s", got["model"])
	}
	if string(got["approval_policy"]) != `"on-request"` {
		t.Fatalf("approval_policy = %s", got["approval_policy"])
	}
	if string(got["sandbox_mode"]) != `"workspace-write"` {
		t.Fatalf("sandbox_mode = %s", got["sandbox_mode"])
	}
	if string(got["model_reasoning_effort"]) != `"high"` {
		t.Fatalf("model_reasoning_effort = %s", got["model_reasoning_effort"])
	}
	if string(got["custom_key"]) != `{"nested":1}` {
		t.Fatalf("custom_key = %s", got["custom_key"])
	}
	// Round-trip preserves the flattened extra.
	var back Config
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("decode back: %v", err)
	}
	if back.Additional == nil || string(back.Additional["custom_key"]) != `{"nested":1}` {
		t.Fatalf("Additional lost on decode: %#v", back.Additional)
	}
}

func schemaModel() *string                                          { s := "gpt-5"; return &s }
func sandboxPtr(s SandboxModeV2) *SandboxModeV2                     { return &s }
func webSearchPtr(w protocol.WebSearchMode) *protocol.WebSearchMode { return &w }

func TestConfigReadParamsSkipDefaults(t *testing.T) {
	// includeLayers omitted when false; cwd omitted when nil.
	roundTripInto(t, ConfigReadParams{}, `{}`, new(ConfigReadParams))
	roundTripInto(t, ConfigReadParams{IncludeLayers: true, Cwd: strPtr("/repo")},
		`{"includeLayers":true,"cwd":"/repo"}`, new(ConfigReadParams))
}

func TestConfigLayerSkipDisabledReason(t *testing.T) {
	src := ConfigLayerSource{Kind: ConfigLayerSourceSessionFlags}
	roundTripInto(t,
		ConfigLayer{Name: src, Version: "1", Config: json.RawMessage(`{}`)},
		`{"name":{"type":"sessionFlags"},"version":"1","config":{}}`,
		new(ConfigLayer))
	roundTripInto(t,
		ConfigLayer{Name: src, Version: "1", Config: json.RawMessage(`{}`), DisabledReason: strPtr("nope")},
		`{"name":{"type":"sessionFlags"},"version":"1","config":{},"disabledReason":"nope"}`,
		new(ConfigLayer))
}

func TestConfigWriteResponse(t *testing.T) {
	roundTripInto(t,
		ConfigWriteResponse{Status: WriteStatusOk, Version: "v2", FilePath: protocol.AbsolutePath("/c.toml")},
		`{"status":"ok","version":"v2","filePath":"/c.toml","overriddenMetadata":null}`,
		new(ConfigWriteResponse))
	roundTripInto(t,
		ConfigWriteResponse{
			Status: WriteStatusOkOverridden, Version: "v3", FilePath: protocol.AbsolutePath("/c.toml"),
			OverriddenMetadata: &OverriddenMetadata{
				Message:         "overridden",
				OverridingLayer: ConfigLayerMetadata{Name: ConfigLayerSource{Kind: ConfigLayerSourceSessionFlags}, Version: "x"},
				EffectiveValue:  json.RawMessage(`true`),
			},
		},
		`{"status":"okOverridden","version":"v3","filePath":"/c.toml","overriddenMetadata":`+
			`{"message":"overridden","overridingLayer":{"name":{"type":"sessionFlags"},"version":"x"},"effectiveValue":true}}`,
		new(ConfigWriteResponse))
}

func TestConfigBatchWriteParams(t *testing.T) {
	roundTripInto(t,
		ConfigBatchWriteParams{Edits: []ConfigEdit{{KeyPath: "model", Value: json.RawMessage(`"gpt-5"`), MergeStrategy: MergeStrategyReplace}}},
		`{"edits":[{"keyPath":"model","value":"gpt-5","mergeStrategy":"replace"}]}`,
		new(ConfigBatchWriteParams))
	roundTripInto(t,
		ConfigBatchWriteParams{
			Edits:            []ConfigEdit{{KeyPath: "x", Value: json.RawMessage(`1`), MergeStrategy: MergeStrategyUpsert}},
			FilePath:         strPtr("/c.toml"),
			ExpectedVersion:  strPtr("v1"),
			ReloadUserConfig: true,
		},
		`{"edits":[{"keyPath":"x","value":1,"mergeStrategy":"upsert"}],"filePath":"/c.toml","expectedVersion":"v1","reloadUserConfig":true}`,
		new(ConfigBatchWriteParams))
}

func TestConfigWarningNotificationSkips(t *testing.T) {
	roundTripInto(t,
		ConfigWarningNotification{Summary: "bad", Details: nil},
		`{"summary":"bad","details":null}`,
		new(ConfigWarningNotification))
	roundTripInto(t,
		ConfigWarningNotification{
			Summary: "bad", Details: strPtr("more"), Path: strPtr("/c.toml"),
			Range: &TextRange{Start: TextPosition{Line: 1, Column: 2}, End: TextPosition{Line: 1, Column: 5}},
		},
		`{"summary":"bad","details":"more","path":"/c.toml","range":{"start":{"line":1,"column":2},"end":{"line":1,"column":5}}}`,
		new(ConfigWarningNotification))
}

// -----------------------------------------------------------------------------
// model.rs
// -----------------------------------------------------------------------------

func TestModelListParamsAlwaysEmits(t *testing.T) {
	roundTripInto(t, ModelListParams{}, `{"cursor":null,"limit":null,"includeHidden":null}`, new(ModelListParams))
	roundTripInto(t, ModelListParams{Cursor: strPtr("c"), Limit: u32Ptr(10), IncludeHidden: boolPtr(true)},
		`{"cursor":"c","limit":10,"includeHidden":true}`, new(ModelListParams))
}

func TestModelDefaultsEmitArrays(t *testing.T) {
	// A Model with nil slices emits the default input modalities plus empty arrays.
	roundTripInto(t,
		Model{
			ID: "id", Model: "m", DisplayName: "M", Description: "d",
			DefaultReasoningEffort: protocol.ReasoningEffortMedium,
		},
		`{"id":"id","model":"m","upgrade":null,"upgradeInfo":null,"availabilityNux":null,`+
			`"displayName":"M","description":"d","hidden":false,"supportedReasoningEfforts":[],`+
			`"defaultReasoningEffort":"medium","inputModalities":["text","image"],"supportsPersonality":false,`+
			`"additionalSpeedTiers":[],"serviceTiers":[],"defaultServiceTier":null,"isDefault":false}`,
		new(Model))
}

func TestModelListResponse(t *testing.T) {
	roundTripInto(t,
		ModelListResponse{Data: []Model{}, NextCursor: strPtr("next")},
		`{"data":[],"nextCursor":"next"}`,
		new(ModelListResponse))
}

func TestModelNotifications(t *testing.T) {
	roundTripInto(t,
		ModelReroutedNotification{ThreadID: "t", TurnID: "u", FromModel: "a", ToModel: "b", Reason: ModelRerouteReasonHighRiskCyberActivity},
		`{"threadId":"t","turnId":"u","fromModel":"a","toModel":"b","reason":"highRiskCyberActivity"}`,
		new(ModelReroutedNotification))
	roundTripInto(t,
		ModelVerificationNotification{ThreadID: "t", TurnID: "u", Verifications: []ModelVerification{ModelVerificationTrustedAccessForCyber}},
		`{"threadId":"t","turnId":"u","verifications":["trustedAccessForCyber"]}`,
		new(ModelVerificationNotification))
}

// -----------------------------------------------------------------------------
// plugin.rs + apps / skills / hooks / marketplace / collaboration / experimental
// -----------------------------------------------------------------------------

func TestPluginSourceRoundTrip(t *testing.T) {
	local := protocol.AbsolutePath("/p")
	cases := []struct {
		name string
		val  PluginSource
		want string
	}{
		{"local", PluginSource{Kind: PluginSourceLocal, Path: &local}, `{"type":"local","path":"/p"}`},
		{
			"git",
			PluginSource{Kind: PluginSourceGit, URL: strPtr("https://x"), GitPath: strPtr("sub"), RefName: strPtr("main"), Sha: strPtr("abc")},
			`{"type":"git","url":"https://x","path":"sub","refName":"main","sha":"abc"}`,
		},
		{
			"git-nulls",
			PluginSource{Kind: PluginSourceGit, URL: strPtr("https://x")},
			`{"type":"git","url":"https://x","path":null,"refName":null,"sha":null}`,
		},
		{"remote", PluginSource{Kind: PluginSourceRemote}, `{"type":"remote"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roundTripInto(t, tc.val, tc.want, new(PluginSource))
		})
	}
}

func TestPluginSourceErrors(t *testing.T) {
	if _, err := json.Marshal(PluginSource{}); err == nil {
		t.Fatal("expected error marshaling empty-kind PluginSource")
	}
	var p PluginSource
	if err := json.Unmarshal([]byte(`{"type":"bogus"}`), &p); err == nil {
		t.Fatal("expected error decoding unknown PluginSource type")
	}
}

func TestPluginSummaryDefaults(t *testing.T) {
	roundTripInto(t,
		PluginSummary{
			ID: "p", Name: "P", Source: PluginSource{Kind: PluginSourceRemote},
			InstallPolicy: PluginInstallPolicyAvailable, AuthPolicy: PluginAuthPolicyOnUse,
			Availability: PluginAvailabilityAvailable,
		},
		`{"id":"p","remotePluginId":null,"localVersion":null,"name":"P","shareContext":null,`+
			`"source":{"type":"remote"},"installed":false,"enabled":false,"installPolicy":"AVAILABLE",`+
			`"authPolicy":"ON_USE","availability":"AVAILABLE","interface":null,"keywords":[]}`,
		new(PluginSummary))
}

func TestNormalizePluginAvailability(t *testing.T) {
	cases := map[string]PluginAvailability{
		"ENABLED":           PluginAvailabilityAvailable,
		"AVAILABLE":         PluginAvailabilityAvailable,
		"DISABLED_BY_ADMIN": PluginAvailability("DISABLED_BY_ADMIN"),
	}
	for in, want := range cases {
		if got := NormalizePluginAvailability(in); got != want {
			t.Fatalf("NormalizePluginAvailability(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPluginListResponseEmptyArrays(t *testing.T) {
	roundTripInto(t, PluginListResponse{},
		`{"marketplaces":[],"marketplaceLoadErrors":[],"featuredPluginIds":[]}`,
		new(PluginListResponse))
	roundTripInto(t, PluginInstalledResponse{},
		`{"marketplaces":[],"marketplaceLoadErrors":[]}`,
		new(PluginInstalledResponse))
}

func TestPluginInterfaceEmptyArrays(t *testing.T) {
	roundTripInto(t, PluginInterface{},
		`{"displayName":null,"shortDescription":null,"longDescription":null,"developerName":null,`+
			`"category":null,"capabilities":[],"websiteUrl":null,"privacyPolicyUrl":null,`+
			`"termsOfServiceUrl":null,"defaultPrompt":null,"brandColor":null,"composerIcon":null,`+
			`"composerIconUrl":null,"logo":null,"logoUrl":null,"screenshots":[],"screenshotUrls":[]}`,
		new(PluginInterface))
}

func TestPluginShareEnums(t *testing.T) {
	roundTripInto(t,
		PluginShareTarget{PrincipalType: PluginSharePrincipalTypeGroup, PrincipalID: "g1", Role: PluginShareTargetRoleEditor},
		`{"principalType":"group","principalId":"g1","role":"editor"}`,
		new(PluginShareTarget))
	roundTripInto(t,
		PluginSharePrincipal{PrincipalType: PluginSharePrincipalTypeUser, PrincipalID: "u1", Role: PluginSharePrincipalRoleOwner, Name: "Me"},
		`{"principalType":"user","principalId":"u1","role":"owner","name":"Me"}`,
		new(PluginSharePrincipal))
}

func TestPluginListMarketplaceKindKebab(t *testing.T) {
	roundTripInto(t,
		PluginListParams{MarketplaceKinds: &[]PluginListMarketplaceKind{
			PluginListMarketplaceKindWorkspaceDirectory, PluginListMarketplaceKindSharedWithMe,
		}},
		`{"cwds":null,"marketplaceKinds":["workspace-directory","shared-with-me"]}`,
		new(PluginListParams))
}

func TestSkillsListParamsSkips(t *testing.T) {
	roundTripInto(t, SkillsListParams{}, `{}`, new(SkillsListParams))
	roundTripInto(t, SkillsListParams{Cwds: []string{"/a"}, ForceReload: true},
		`{"cwds":["/a"],"forceReload":true}`, new(SkillsListParams))
}

func TestSkillMetadataSkips(t *testing.T) {
	roundTripInto(t,
		SkillMetadata{Name: "s", Description: "d", Path: protocol.AbsolutePath("/s"), Scope: SkillScopeRepo, Enabled: true},
		`{"name":"s","description":"d","path":"/s","scope":"repo","enabled":true}`,
		new(SkillMetadata))
	roundTripInto(t,
		SkillMetadata{
			Name: "s", Description: "d", Path: protocol.AbsolutePath("/s"), Scope: SkillScopeUser, Enabled: true,
			ShortDescription: strPtr("sd"),
			Dependencies:     &SkillDependencies{Tools: []SkillToolDependency{{Type: "mcp", Value: "x"}}},
		},
		`{"name":"s","description":"d","shortDescription":"sd",`+
			`"dependencies":{"tools":[{"type":"mcp","value":"x"}]},"path":"/s","scope":"user","enabled":true}`,
		new(SkillMetadata))
}

func TestHookMetadata(t *testing.T) {
	roundTripInto(t,
		HookMetadata{
			Key: "k", EventName: HookEventNamePreToolUse, HandlerType: HookHandlerTypeCommand,
			TimeoutSec: 30, SourcePath: protocol.AbsolutePath("/h"), Source: HookSourcePlugin,
			DisplayOrder: 1, Enabled: true, IsManaged: false, CurrentHash: "abc", TrustStatus: HookTrustStatusTrusted,
		},
		`{"key":"k","eventName":"preToolUse","handlerType":"command","matcher":null,"command":null,`+
			`"timeoutSec":30,"statusMessage":null,"sourcePath":"/h","source":"plugin","pluginId":null,`+
			`"displayOrder":1,"enabled":true,"isManaged":false,"currentHash":"abc","trustStatus":"trusted"}`,
		new(HookMetadata))
}

func TestMarketplaceParams(t *testing.T) {
	roundTripInto(t,
		MarketplaceAddParams{Source: "git@x", RefName: strPtr("main"), SparsePaths: &[]string{"a"}},
		`{"source":"git@x","refName":"main","sparsePaths":["a"]}`,
		new(MarketplaceAddParams))
	roundTripInto(t,
		MarketplaceAddParams{Source: "git@x"},
		`{"source":"git@x","refName":null,"sparsePaths":null}`,
		new(MarketplaceAddParams))
	roundTripInto(t,
		MarketplaceRemoveResponse{MarketplaceName: "m"},
		`{"marketplaceName":"m","installedRoot":null}`,
		new(MarketplaceRemoveResponse))
}

func TestAppInfoDefaults(t *testing.T) {
	roundTripInto(t,
		AppInfo{ID: "a", Name: "A", IsEnabled: true},
		`{"id":"a","name":"A","description":null,"logoUrl":null,"logoUrlDark":null,`+
			`"distributionChannel":null,"branding":null,"appMetadata":null,"labels":null,`+
			`"installUrl":null,"isAccessible":false,"isEnabled":true,"pluginDisplayNames":[]}`,
		new(AppInfo))
}

func TestAppScreenshotAliases(t *testing.T) {
	var a AppScreenshot
	if err := json.Unmarshal([]byte(`{"url":"u","file_id":"f","user_prompt":"p"}`), &a); err != nil {
		t.Fatalf("unmarshal aliases: %v", err)
	}
	if a.URL == nil || *a.URL != "u" || a.FileID == nil || *a.FileID != "f" || a.UserPrompt != "p" {
		t.Fatalf("aliases not applied: %#v", a)
	}
	// camelCase keys also decode.
	var b AppScreenshot
	if err := json.Unmarshal([]byte(`{"fileId":"f2","userPrompt":"p2"}`), &b); err != nil {
		t.Fatalf("unmarshal camel: %v", err)
	}
	if b.FileID == nil || *b.FileID != "f2" || b.UserPrompt != "p2" {
		t.Fatalf("camel keys not applied: %#v", b)
	}
}

func TestCollaborationModeMaskV2DoubleOption(t *testing.T) {
	mode := protocol.ModeKindPlan
	effort := protocol.ReasoningEffortLow
	// Value present.
	roundTripInto(t,
		CollaborationModeMaskV2{Name: "n", Mode: &mode, Model: strPtr("m"), ReasoningEffort: NewDoubleOptionValue[*protocol.ReasoningEffort](&effort)},
		`{"name":"n","mode":"plan","model":"m","reasoning_effort":"low"}`,
		new(CollaborationModeMaskV2))
	// Explicit null (present, not set).
	roundTripInto(t,
		CollaborationModeMaskV2{Name: "n", ReasoningEffort: NewDoubleOptionNull[*protocol.ReasoningEffort]()},
		`{"name":"n","mode":null,"model":null,"reasoning_effort":null}`,
		new(CollaborationModeMaskV2))
}

func TestExperimentalFeature(t *testing.T) {
	roundTripInto(t,
		ExperimentalFeature{Name: "f", Stage: ExperimentalFeatureStageBeta, Enabled: true, DefaultEnabled: false},
		`{"name":"f","stage":"beta","displayName":null,"description":null,"announcement":null,"enabled":true,"defaultEnabled":false}`,
		new(ExperimentalFeature))
	roundTripInto(t,
		ExperimentalFeatureListParams{},
		`{"cursor":null,"limit":null,"threadId":null}`,
		new(ExperimentalFeatureListParams))
}

// -----------------------------------------------------------------------------
// Registry coverage: confirm every method/notification this file owns is wired.
// -----------------------------------------------------------------------------

func TestConfigModelPluginMethodsRegistered(t *testing.T) {
	clientMethods := []string{
		"config/read", "config/batchWrite",
		"model/list",
		"app/list", "skills/list", "hooks/list",
		"marketplace/add", "marketplace/remove", "marketplace/upgrade",
		"plugin/list", "plugin/installed", "plugin/read", "plugin/skill/read",
		"plugin/share/save", "plugin/share/updateTargets", "plugin/share/list",
		"plugin/share/checkout", "plugin/share/delete",
		"plugin/install", "plugin/uninstall",
		"collaborationMode/list", "experimentalFeature/list",
	}
	for _, m := range clientMethods {
		spec, ok := Lookup(m)
		if !ok {
			t.Fatalf("method %q not registered", m)
		}
		if spec.NewParams() == nil || spec.NewResult() == nil {
			t.Fatalf("method %q has nil constructors", m)
		}
	}

	// collaborationMode/list is the only experimental method in this set.
	if spec, _ := Lookup("collaborationMode/list"); !spec.Experimental {
		t.Fatal("collaborationMode/list should be experimental")
	}
	if spec, _ := Lookup("model/list"); spec.Experimental {
		t.Fatal("model/list should not be experimental")
	}

	notifications := []string{
		"app/list/updated", "model/rerouted", "model/verification",
		"configWarning", "skills/changed",
	}
	for _, n := range notifications {
		spec, ok := LookupNotification(n)
		if !ok {
			t.Fatalf("notification %q not registered", n)
		}
		if spec.NewParams() == nil {
			t.Fatalf("notification %q has nil constructor", n)
		}
	}
}

func TestDecodeConfigReadParamsViaRegistry(t *testing.T) {
	req := JSONRPCRequest{Method: "config/read", Params: json.RawMessage(`{"includeLayers":true,"cwd":"/repo"}`)}
	v, err := DecodeClientRequestParams(req)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	p, ok := v.(*ConfigReadParams)
	if !ok {
		t.Fatalf("decoded type = %T, want *ConfigReadParams", v)
	}
	if !p.IncludeLayers || p.Cwd == nil || *p.Cwd != "/repo" {
		t.Fatalf("decoded params = %#v", p)
	}
}
