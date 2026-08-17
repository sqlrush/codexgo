package config

import (
	"github.com/sqlrush/codexgo/pkg/features"
	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ConfigToml is the base configuration deserialized from ~/.codexgo/config.toml.
// It mirrors the Rust ConfigToml struct field-for-field and order-for-order so
// that JSON serialization matches byte-for-byte after key-order canonicalization.
//
// AbsolutePathBuf fields are modeled as *string (serde serializes them as plain
// path strings). Open-ended nested schemas (apps, skills, permissions, desktop)
// are carried as opaque TOML value trees to preserve arbitrary structure.
type ConfigToml struct {
	Model                            *string                                        `json:"model"`
	ReviewModel                      *string                                        `json:"review_model"`
	ModelProvider                    *string                                        `json:"model_provider"`
	ModelContextWindow               *int64                                         `json:"model_context_window"`
	ModelAutoCompactTokenLimit       *int64                                         `json:"model_auto_compact_token_limit"`
	ModelAutoCompactTokenLimitScope  *protocol.AutoCompactTokenLimitScope           `json:"model_auto_compact_token_limit_scope"`
	ApprovalPolicy                   *protocol.AskForApproval                       `json:"approval_policy"`
	ApprovalsReviewer                *protocol.ApprovalsReviewer                    `json:"approvals_reviewer"`
	AutoReview                       *AutoReviewToml                                `json:"auto_review"`
	ShellEnvironmentPolicy           ShellEnvironmentPolicyToml                     `json:"shell_environment_policy"`
	AllowLoginShell                  *bool                                          `json:"allow_login_shell"`
	SandboxMode                      *protocol.SandboxMode                          `json:"sandbox_mode"`
	SandboxWorkspaceWrite            *SandboxWorkspaceWrite                         `json:"sandbox_workspace_write"`
	DefaultPermissions               *string                                        `json:"default_permissions"`
	Permissions                      PermissionsToml                                `json:"permissions"`
	Notify                           *[]string                                      `json:"notify"`
	Instructions                     *string                                        `json:"instructions"`
	DeveloperInstructions            *string                                        `json:"developer_instructions"`
	IncludePermissionsInstructions   *bool                                          `json:"include_permissions_instructions"`
	IncludeAppsInstructions          *bool                                          `json:"include_apps_instructions"`
	IncludeCollaborationModeInstr    *bool                                          `json:"include_collaboration_mode_instructions"`
	IncludeEnvironmentContext        *bool                                          `json:"include_environment_context"`
	ModelInstructionsFile            *string                                        `json:"model_instructions_file"`
	CompactPrompt                    *string                                        `json:"compact_prompt"`
	ForcedChatgptWorkspaceID         *ForcedChatgptWorkspaceIds                     `json:"forced_chatgpt_workspace_id"`
	ForcedLoginMethod                *protocol.ForcedLoginMethod                    `json:"forced_login_method"`
	CliAuthCredentialsStore          *AuthCredentialsStoreMode                      `json:"cli_auth_credentials_store"`
	McpServers                       map[string]McpServerConfig                     `json:"mcp_servers"`
	McpOAuthCredentialsStore         *OAuthCredentialsStoreMode                     `json:"mcp_oauth_credentials_store"`
	McpOAuthCallbackPort             *uint16                                        `json:"mcp_oauth_callback_port"`
	McpOAuthCallbackURL              *string                                        `json:"mcp_oauth_callback_url"`
	ModelProviders                   map[string]modelproviderinfo.ModelProviderInfo `json:"model_providers"`
	ProjectDocMaxBytes               *uint64                                        `json:"project_doc_max_bytes"`
	ProjectDocFallbackFilenames      *[]string                                      `json:"project_doc_fallback_filenames"`
	ToolOutputTokenLimit             *uint64                                        `json:"tool_output_token_limit"`
	BackgroundTerminalMaxTimeout     *uint64                                        `json:"background_terminal_max_timeout"`
	JsReplNodePath                   *string                                        `json:"js_repl_node_path"`
	JsReplNodeModuleDirs             *[]string                                      `json:"js_repl_node_module_dirs"`
	Profile                          *string                                        `json:"profile"`
	Profiles                         map[string]ConfigProfile                       `json:"profiles"`
	History                          *History                                       `json:"history"`
	SqliteHome                       *string                                        `json:"sqlite_home"`
	LogDir                           *string                                        `json:"log_dir"`
	Debug                            *DebugToml                                     `json:"debug"`
	FileOpener                       *UriBasedFileOpener                            `json:"file_opener"`
	Tui                              *Tui                                           `json:"tui"`
	HideAgentReasoning               *bool                                          `json:"hide_agent_reasoning"`
	ShowRawAgentReasoning            *bool                                          `json:"show_raw_agent_reasoning"`
	ModelReasoningEffort             *protocol.ReasoningEffort                      `json:"model_reasoning_effort"`
	PlanModeReasoningEffort          *protocol.ReasoningEffort                      `json:"plan_mode_reasoning_effort"`
	ModelReasoningSummary            *protocol.ReasoningSummary                     `json:"model_reasoning_summary"`
	ModelVerbosity                   *protocol.Verbosity                            `json:"model_verbosity"`
	ModelSupportsReasoningSummaries  *bool                                          `json:"model_supports_reasoning_summaries"`
	ModelCatalogJSON                 *string                                        `json:"model_catalog_json"`
	Personality                      *protocol.Personality                          `json:"personality"`
	ServiceTier                      *string                                        `json:"service_tier"`
	ChatgptBaseURL                   *string                                        `json:"chatgpt_base_url"`
	AppsMcpProductSKU                *string                                        `json:"apps_mcp_product_sku"`
	OpenAIBaseURL                    *string                                        `json:"openai_base_url"`
	Audio                            *RealtimeAudioToml                             `json:"audio"`
	ExperimentalRealtimeWsBaseURL    *string                                        `json:"experimental_realtime_ws_base_url"`
	ExperimentalRealtimeWsModel      *string                                        `json:"experimental_realtime_ws_model"`
	Realtime                         *RealtimeToml                                  `json:"realtime"`
	ExperimentalRealtimeWsBackendPr  *string                                        `json:"experimental_realtime_ws_backend_prompt"`
	ExperimentalRealtimeWsStartupCtx *string                                        `json:"experimental_realtime_ws_startup_context"`
	ExperimentalRealtimeStartInstr   *string                                        `json:"experimental_realtime_start_instructions"`
	ExperimentalThreadConfigEndpoint *string                                        `json:"experimental_thread_config_endpoint"`
	ExperimentalThreadStoreEndpoint  *string                                        `json:"experimental_thread_store_endpoint"`
	ExperimentalThreadStore          ThreadStoreToml                                `json:"experimental_thread_store"`
	Projects                         *map[string]ProjectConfig                      `json:"projects"`
	WebSearch                        *protocol.WebSearchMode                        `json:"web_search"`
	Tools                            *ToolsToml                                     `json:"tools"`
	ToolSuggest                      *ToolSuggestConfig                             `json:"tool_suggest"`
	Agents                           *AgentsToml                                    `json:"agents"`
	Memories                         *MemoriesToml                                  `json:"memories"`
	Skills                           SkillsConfig                                   `json:"skills"`
	Hooks                            *HooksToml                                     `json:"hooks"`
	Plugins                          map[string]PluginConfig                        `json:"plugins"`
	Marketplaces                     map[string]MarketplaceConfig                   `json:"marketplaces"`
	Features                         *features.FeaturesToml                         `json:"features"`
	SuppressUnstableFeaturesWarning  *bool                                          `json:"suppress_unstable_features_warning"`
	GhostSnapshot                    *GhostSnapshotToml                             `json:"ghost_snapshot"`
	ProjectRootMarkers               *[]string                                      `json:"project_root_markers"`
	CheckForUpdateOnStartup          *bool                                          `json:"check_for_update_on_startup"`
	DisablePasteBurst                *bool                                          `json:"disable_paste_burst"`
	Analytics                        *AnalyticsConfigToml                           `json:"analytics"`
	Feedback                         *FeedbackConfigToml                            `json:"feedback"`
	Apps                             AppsConfigToml                                 `json:"apps"`
	Desktop                          *map[string]TomlValue                          `json:"desktop"`
	Otel                             *OtelConfigToml                                `json:"otel"`
	Windows                          *WindowsToml                                   `json:"windows"`
	Notice                           *Notice                                        `json:"notice"`
	ExperimentalCompactPromptFile    *string                                        `json:"experimental_compact_prompt_file"`
	ExperimentalUseUnifiedExecTool   *bool                                          `json:"experimental_use_unified_exec_tool"`
	OssProvider                      *string                                        `json:"oss_provider"`
}

// DefaultConfigToml returns a ConfigToml with the serde defaults that the Rust
// crate applies on deserialization of an empty document:
//   - allow_login_shell = Some(true)
//   - history = Some(History::default())
//   - project_doc_max_bytes = Some(32 KiB)
//   - project_doc_fallback_filenames = Some([])
//   - hide_agent_reasoning = Some(false)
//
// All other fields default to their zero values (nil for optionals, empty maps).
func DefaultConfigToml() ConfigToml {
	t := true
	f := false
	maxBytes := uint64(DefaultProjectDocMaxBytes)
	empty := []string{}
	hist := DefaultHistory()
	return ConfigToml{
		AllowLoginShell:             &t,
		History:                     &hist,
		ProjectDocMaxBytes:          &maxBytes,
		ProjectDocFallbackFilenames: &empty,
		HideAgentReasoning:          &f,
		McpServers:                  map[string]McpServerConfig{},
		ModelProviders:              map[string]modelproviderinfo.ModelProviderInfo{},
		Profiles:                    map[string]ConfigProfile{},
		Plugins:                     map[string]PluginConfig{},
		Marketplaces:                map[string]MarketplaceConfig{},
	}
}
