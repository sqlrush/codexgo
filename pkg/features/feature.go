package features

import "runtime"

// Feature is a unique feature toggled via configuration. It mirrors the Rust
// `Feature` enum; the underlying values are stable ordinals used for ordering
// (BTreeSet semantics) and lookups.
type Feature int

const (
	// Stable.

	// FeatureShellTool enables the default shell tool.
	FeatureShellTool Feature = iota
	// FeatureCodexHooks enables Claude-style lifecycle hooks loaded from
	// hooks.json files.
	FeatureCodexHooks

	// Experimental / under development.

	// FeatureCodeMode enables JavaScript code mode backed by the in-process V8
	// runtime.
	FeatureCodeMode
	// FeatureCodeModeOnly restricts model-visible tools to code mode
	// entrypoints (exec, wait).
	FeatureCodeModeOnly
	// FeatureUnifiedExec uses the single unified PTY-backed exec tool.
	FeatureUnifiedExec
	// FeatureShellZshFork routes shell tool execution through the zsh exec
	// bridge.
	FeatureShellZshFork
	// FeatureTerminalResizeReflow reflows transcript scrollback when the
	// terminal is resized.
	FeatureTerminalResizeReflow
	// FeatureApplyPatchStreamingEvents streams structured progress while
	// apply_patch input is being generated.
	FeatureApplyPatchStreamingEvents
	// FeatureExecPermissionApprovals allows exec tools to request additional
	// permissions while staying sandboxed.
	FeatureExecPermissionApprovals
	// FeatureRequestPermissionsTool exposes the built-in request_permissions
	// tool.
	FeatureRequestPermissionsTool
	// FeatureWebSearchRequest allows the model to request web searches that
	// fetch live content.
	FeatureWebSearchRequest
	// FeatureWebSearchCached allows the model to request web searches that
	// fetch cached content. Takes precedence over FeatureWebSearchRequest.
	FeatureWebSearchCached
	// FeatureStandaloneWebSearch exposes the extension-backed standalone web
	// search tool.
	FeatureStandaloneWebSearch
	// FeatureUseLegacyLandlock uses the legacy Landlock Linux sandbox fallback
	// instead of the default bubblewrap pipeline.
	FeatureUseLegacyLandlock
	// FeatureShellSnapshot enables experimental shell snapshotting.
	FeatureShellSnapshot
	// FeatureRuntimeMetrics enables runtime metrics snapshots via a manual
	// reader.
	FeatureRuntimeMetrics
	// FeatureMemoryTool enables startup memory extraction and file-backed
	// memory consolidation.
	FeatureMemoryTool
	// FeatureChronicle enables the Chronicle sidecar for passive screen-context
	// memories.
	FeatureChronicle
	// FeatureChildAgentsMd appends additional AGENTS.md guidance to user
	// instructions.
	FeatureChildAgentsMd
	// FeatureEnableRequestCompression compresses request bodies (zstd) when
	// sending streaming requests to codex-backend.
	FeatureEnableRequestCompression
	// FeatureNetworkProxy starts the managed network proxy for sandboxed
	// sessions.
	FeatureNetworkProxy
	// FeatureCollab enables collab tools.
	FeatureCollab
	// FeatureMultiAgentV2 enables task-path-based multi-agent routing.
	FeatureMultiAgentV2
	// FeatureSpawnCsv enables CSV-backed agent job tools.
	FeatureSpawnCsv
	// FeatureApps enables apps.
	FeatureApps
	// FeatureEnableMcpApps enables MCP apps.
	FeatureEnableMcpApps
	// FeatureAppsMcpPathOverride uses the new path for the host-owned apps MCP
	// server.
	FeatureAppsMcpPathOverride
	// FeatureToolSearch is a removed compatibility flag retained as a no-op now
	// that tool_search is always enabled.
	FeatureToolSearch
	// FeatureToolSearchAlwaysDeferMcpTools always defers MCP tools behind
	// tool_search instead of exposing small sets directly.
	FeatureToolSearchAlwaysDeferMcpTools
	// FeatureNonPrefixedMcpToolNames exposes MCP model-visible namespaces
	// without the legacy mcp__ prefix.
	FeatureNonPrefixedMcpToolNames
	// FeatureToolSuggest enables discoverable tool suggestions for apps.
	FeatureToolSuggest
	// FeaturePlugins enables plugins.
	FeaturePlugins
	// FeaturePluginHooks is a removed compatibility flag for plugin-bundled
	// lifecycle hooks.
	FeaturePluginHooks
	// FeatureInAppBrowser allows the in-app browser pane in desktop apps.
	FeatureInAppBrowser
	// FeatureBrowserUse allows Browser Use agent integration in desktop apps.
	FeatureBrowserUse
	// FeatureBrowserUseExternal allows Browser Use integration with external
	// browsers.
	FeatureBrowserUseExternal
	// FeatureComputerUse allows Codex Computer Use.
	FeatureComputerUse
	// FeatureRemotePlugin is a temporary internal-only flag for PS-backed
	// remote plugin catalog development.
	FeatureRemotePlugin
	// FeaturePluginSharing enables remote plugin sharing flows.
	FeaturePluginSharing
	// FeatureExternalMigration shows the startup prompt for migrating external
	// agent config into Codex.
	FeatureExternalMigration
	// FeatureImageGeneration allows the model to invoke the built-in image
	// generation tool.
	FeatureImageGeneration
	// FeatureImageGenExt replaces hosted image generation with the standalone
	// image-generation extension.
	FeatureImageGenExt
	// FeatureSkillMcpDependencyInstall allows prompting and installing missing
	// MCP dependencies.
	FeatureSkillMcpDependencyInstall
	// FeatureSkillEnvVarDependencyPrompt is a removed compatibility flag for
	// deleted skill env var dependency prompting.
	FeatureSkillEnvVarDependencyPrompt
	// FeatureMentionsV2 enables the unified mention popup prototype.
	FeatureMentionsV2
	// FeatureDefaultModeRequestUserInput allows request_user_input in Default
	// collaboration mode.
	FeatureDefaultModeRequestUserInput
	// FeatureGuardianApproval enables automatic review for approval prompts.
	FeatureGuardianApproval
	// FeatureGoals enables persisted thread goals and automatic goal
	// continuation.
	FeatureGoals
	// FeatureToolCallMcpElicitation routes MCP tool approval prompts through the
	// MCP elicitation request path.
	FeatureToolCallMcpElicitation
	// FeatureAuthElicitation prompts Codex Apps connector auth failures through
	// MCP URL elicitations.
	FeatureAuthElicitation
	// FeaturePersonality enables personality selection in the TUI.
	FeaturePersonality
	// FeatureArtifact enables native artifact tools.
	FeatureArtifact
	// FeatureFastMode enables Fast mode selection in the TUI and request layer.
	FeatureFastMode
	// FeatureRealtimeConversation enables experimental realtime voice
	// conversation mode in the TUI.
	FeatureRealtimeConversation
	// FeaturePreventIdleSleep prevents idle system sleep while a turn is
	// actively running.
	FeaturePreventIdleSleep
	// FeatureResponsesWebsocketResponseProcessed sends response.processed over
	// Responses API websockets after a turn response is recorded.
	FeatureResponsesWebsocketResponseProcessed
	// FeatureRemoteCompactionV2 enables remote compaction v2 over the normal
	// Responses API.
	FeatureRemoteCompactionV2
	// FeatureWorkspaceDependencies enables workspace dependency support.
	FeatureWorkspaceDependencies

	// Removed.

	// FeatureGhostCommit is a removed compatibility flag retained as a no-op so
	// old configs can still parse `undo`.
	FeatureGhostCommit
	// FeatureJsRepl is a removed compatibility flag for the deleted JavaScript
	// REPL feature.
	FeatureJsRepl
	// FeatureJsReplToolsOnly is a removed compatibility flag for the deleted
	// JavaScript REPL tool-only mode.
	FeatureJsReplToolsOnly
	// FeatureSearchTool is a legacy search-tool feature flag kept for backward
	// compatibility.
	FeatureSearchTool
	// FeatureUseLinuxSandboxBwrap is a removed legacy Linux bubblewrap opt-in
	// flag retained as a no-op.
	FeatureUseLinuxSandboxBwrap
	// FeatureRequestRule allows the model to request approval and propose exec
	// rules.
	FeatureRequestRule
	// FeatureWindowsSandbox enables the Windows sandbox (restricted token) on
	// Windows.
	FeatureWindowsSandbox
	// FeatureWindowsSandboxElevated uses the elevated Windows sandbox pipeline.
	FeatureWindowsSandboxElevated
	// FeatureRemoteModels is a legacy remote models flag kept for backward
	// compatibility.
	FeatureRemoteModels
	// FeatureCodexGitCommit is a removed legacy git commit attribution guidance
	// flag.
	FeatureCodexGitCommit
	// FeatureSqlite persists rollout metadata to a local SQLite database.
	FeatureSqlite
	// FeatureApplyPatchFreeform is a removed compatibility flag for the deleted
	// apply_patch fallback feature.
	FeatureApplyPatchFreeform
	// FeatureUnavailableDummyTools is a removed compatibility flag for the
	// deleted unavailable-tool placeholder backfill.
	FeatureUnavailableDummyTools
	// FeatureSteer is the steer feature flag; kept for config backward
	// compatibility, behavior is always steer-enabled.
	FeatureSteer
	// FeatureCollaborationModes enables collaboration modes (Plan, Default);
	// kept for config backward compatibility.
	FeatureCollaborationModes
	// FeatureRemoteControl is a removed compatibility flag for the deleted
	// remote control feature.
	FeatureRemoteControl
	// FeatureImageDetailOriginal is a removed compatibility flag retained as a
	// no-op so old wrappers can still pass `--enable image_detail_original`.
	FeatureImageDetailOriginal
	// FeatureTuiAppServer is a removed compatibility flag; the TUI now always
	// uses the app-server implementation.
	FeatureTuiAppServer
	// FeatureWorkspaceOwnerUsageNudge is a removed compatibility flag retained
	// as a no-op now that workspace owner usage nudges are always enabled.
	FeatureWorkspaceOwnerUsageNudge
	// FeatureResponsesWebsockets is a legacy rollout flag for Responses API
	// WebSocket transport experiments.
	FeatureResponsesWebsockets
	// FeatureResponsesWebsocketsV2 is a legacy rollout flag for Responses API
	// WebSocket transport v2 experiments.
	FeatureResponsesWebsocketsV2
)

// Key returns the canonical `[features]` table key for the feature.
func (f Feature) Key() string { return f.info().Key }

// Stage returns the lifecycle stage of the feature.
func (f Feature) Stage() Stage { return f.info().Stage }

// DefaultEnabled reports whether the feature is enabled by default.
func (f Feature) DefaultEnabled() bool { return f.info().DefaultEnabled }

// info returns the FeatureSpec describing the feature. It panics if the feature
// is missing from FEATURES, mirroring the Rust `unreachable!`.
func (f Feature) info() FeatureSpec {
	if spec, ok := specByID(f); ok {
		return spec
	}
	panic("features: missing FeatureSpec for feature")
}

// FeatureSpec is a single, easy-to-read registry entry for a feature
// definition. Mirrors the Rust `FeatureSpec` struct.
type FeatureSpec struct {
	ID             Feature
	Key            string
	Stage          Stage
	DefaultEnabled bool
}

// isWindows reports whether the current platform is Windows, used to mirror the
// Rust `cfg!(windows)` defaults.
func isWindows() bool { return runtime.GOOS == "windows" }

// preventIdleSleepStage mirrors the platform-conditional stage of the
// prevent_idle_sleep feature in Rust.
func preventIdleSleepStage() Stage {
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		return experimental(
			"Prevent sleep while running",
			"Keep your computer awake while Codex is running a thread.",
			"NEW: Prevent sleep while running is now available in /experimental.",
		)
	default:
		return underDevelopment()
	}
}

// FEATURES is the single registry of all feature definitions, ordered to match
// the Rust FEATURES slice.
var FEATURES = []FeatureSpec{
	// Stable features.
	{ID: FeatureGhostCommit, Key: "undo", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureShellTool, Key: "shell_tool", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureUnifiedExec, Key: "unified_exec", Stage: stable(), DefaultEnabled: !isWindows()},
	{ID: FeatureShellZshFork, Key: "shell_zsh_fork", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureShellSnapshot, Key: "shell_snapshot", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureJsRepl, Key: "js_repl", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureCodeMode, Key: "code_mode", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureCodeModeOnly, Key: "code_mode_only", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureJsReplToolsOnly, Key: "js_repl_tools_only", Stage: removed(), DefaultEnabled: false},
	{
		ID:  FeatureTerminalResizeReflow,
		Key: "terminal_resize_reflow",
		Stage: experimental(
			"Terminal resize reflow",
			"Rebuild Codex-owned transcript scrollback when the terminal width changes.",
			"",
		),
		DefaultEnabled: true,
	},
	{ID: FeatureWebSearchRequest, Key: "web_search_request", Stage: deprecated(), DefaultEnabled: false},
	{ID: FeatureWebSearchCached, Key: "web_search_cached", Stage: deprecated(), DefaultEnabled: false},
	{ID: FeatureStandaloneWebSearch, Key: "standalone_web_search", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureSearchTool, Key: "search_tool", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureCodexGitCommit, Key: "codex_git_commit", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureRuntimeMetrics, Key: "runtime_metrics", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureSqlite, Key: "sqlite", Stage: removed(), DefaultEnabled: true},
	{
		ID:  FeatureMemoryTool,
		Key: "memories",
		Stage: experimental(
			"Memories",
			"Allow Codex to create new memories from conversations and bring relevant memories into new conversations.",
			"NEW: Codex can now generate and use memories. Try it now with `/memories`",
		),
		DefaultEnabled: false,
	},
	{ID: FeatureChronicle, Key: "chronicle", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureChildAgentsMd, Key: "child_agents_md", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureApplyPatchFreeform, Key: "apply_patch_freeform", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureApplyPatchStreamingEvents, Key: "apply_patch_streaming_events", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureExecPermissionApprovals, Key: "exec_permission_approvals", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureCodexHooks, Key: "hooks", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureRequestPermissionsTool, Key: "request_permissions_tool", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureUseLinuxSandboxBwrap, Key: "use_linux_sandbox_bwrap", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureUseLegacyLandlock, Key: "use_legacy_landlock", Stage: deprecated(), DefaultEnabled: false},
	{ID: FeatureRequestRule, Key: "request_rule", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureWindowsSandbox, Key: "experimental_windows_sandbox", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureWindowsSandboxElevated, Key: "elevated_windows_sandbox", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureRemoteModels, Key: "remote_models", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureEnableRequestCompression, Key: "enable_request_compression", Stage: stable(), DefaultEnabled: true},
	{
		ID:  FeatureNetworkProxy,
		Key: "network_proxy",
		Stage: experimental(
			"Network proxy",
			"Apply network proxy restrictions to sandboxed sessions that already have network access.",
			"NEW: Network proxy can now be enabled from /experimental. Restart Codex after enabling it.",
		),
		DefaultEnabled: false,
	},
	{ID: FeatureCollab, Key: "multi_agent", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureMultiAgentV2, Key: "multi_agent_v2", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureSpawnCsv, Key: "enable_fanout", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureApps, Key: "apps", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureEnableMcpApps, Key: "enable_mcp_apps", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureAppsMcpPathOverride, Key: "apps_mcp_path_override", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureToolSearch, Key: "tool_search", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureToolSearchAlwaysDeferMcpTools, Key: "tool_search_always_defer_mcp_tools", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureNonPrefixedMcpToolNames, Key: "non_prefixed_mcp_tool_names", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureUnavailableDummyTools, Key: "unavailable_dummy_tools", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureToolSuggest, Key: "tool_suggest", Stage: stable(), DefaultEnabled: true},
	{ID: FeaturePlugins, Key: "plugins", Stage: stable(), DefaultEnabled: true},
	{ID: FeaturePluginHooks, Key: "plugin_hooks", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureInAppBrowser, Key: "in_app_browser", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureBrowserUse, Key: "browser_use", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureBrowserUseExternal, Key: "browser_use_external", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureComputerUse, Key: "computer_use", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureRemotePlugin, Key: "remote_plugin", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeaturePluginSharing, Key: "plugin_sharing", Stage: stable(), DefaultEnabled: true},
	{
		ID:  FeatureExternalMigration,
		Key: "external_migration",
		Stage: experimental(
			"External migration",
			"Show a startup prompt when Codex detects migratable external agent config for this machine or project.",
			"",
		),
		DefaultEnabled: false,
	},
	{ID: FeatureImageGeneration, Key: "image_generation", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureImageGenExt, Key: "imagegenext", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureSkillMcpDependencyInstall, Key: "skill_mcp_dependency_install", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureSkillEnvVarDependencyPrompt, Key: "skill_env_var_dependency_prompt", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureMentionsV2, Key: "mentions_v2", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureSteer, Key: "steer", Stage: removed(), DefaultEnabled: true},
	{ID: FeatureDefaultModeRequestUserInput, Key: "default_mode_request_user_input", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureGuardianApproval, Key: "guardian_approval", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureGoals, Key: "goals", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureCollaborationModes, Key: "collaboration_modes", Stage: removed(), DefaultEnabled: true},
	{ID: FeatureToolCallMcpElicitation, Key: "tool_call_mcp_elicitation", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureAuthElicitation, Key: "auth_elicitation", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeaturePersonality, Key: "personality", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureArtifact, Key: "artifact", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureFastMode, Key: "fast_mode", Stage: stable(), DefaultEnabled: true},
	{ID: FeatureRealtimeConversation, Key: "realtime_conversation", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureRemoteControl, Key: "remote_control", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureImageDetailOriginal, Key: "image_detail_original", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureTuiAppServer, Key: "tui_app_server", Stage: removed(), DefaultEnabled: true},
	{ID: FeaturePreventIdleSleep, Key: "prevent_idle_sleep", Stage: preventIdleSleepStage(), DefaultEnabled: false},
	{ID: FeatureWorkspaceOwnerUsageNudge, Key: "workspace_owner_usage_nudge", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureResponsesWebsockets, Key: "responses_websockets", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureResponsesWebsocketsV2, Key: "responses_websockets_v2", Stage: removed(), DefaultEnabled: false},
	{ID: FeatureResponsesWebsocketResponseProcessed, Key: "responses_websocket_response_processed", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureRemoteCompactionV2, Key: "remote_compaction_v2", Stage: underDevelopment(), DefaultEnabled: false},
	{ID: FeatureWorkspaceDependencies, Key: "workspace_dependencies", Stage: stable(), DefaultEnabled: true},
}

// specByKey returns the FeatureSpec with the given canonical key.
func specByKey(key string) (FeatureSpec, bool) {
	for _, spec := range FEATURES {
		if spec.Key == key {
			return spec, true
		}
	}
	return FeatureSpec{}, false
}

// specByID returns the FeatureSpec for the given feature id.
func specByID(id Feature) (FeatureSpec, bool) {
	for _, spec := range FEATURES {
		if spec.ID == id {
			return spec, true
		}
	}
	return FeatureSpec{}, false
}
