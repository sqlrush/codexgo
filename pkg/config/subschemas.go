package config

import (
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// WebSearchToolConfig is the web-search tool config (lives in protocol upstream;
// represented opaquely here as a TOML value tree to preserve fields faithfully).
type WebSearchToolConfig = TomlValue

// ExperimentalRequestUserInput toggles the request_user_input tool.
// deny_unknown_fields; enabled defaults to true.
type ExperimentalRequestUserInput struct {
	Enabled bool `json:"enabled" toml:"enabled"`
}

// ToolsToml is the nested [tools] table. deny_unknown_fields. The web_search
// field accepts either a bool (legacy enable flag, mapped to None) or a config
// object; we preserve the decoded value tree.
type ToolsToml struct {
	WebSearch                    *WebSearchToolConfig          `json:"web_search" toml:"web_search"`
	ExperimentalRequestUserInput *ExperimentalRequestUserInput `json:"experimental_request_user_input" toml:"experimental_request_user_input"`
}

// AgentsToml configures agent thread limits and roles. deny_unknown_fields with
// a flattened roles map.
type AgentsToml struct {
	MaxThreads           *uint64                  `json:"max_threads" toml:"max_threads"`
	MaxDepth             *int32                   `json:"max_depth" toml:"max_depth"`
	JobMaxRuntimeSeconds *uint64                  `json:"job_max_runtime_seconds" toml:"job_max_runtime_seconds"`
	InterruptMessage     *bool                    `json:"interrupt_message" toml:"interrupt_message"`
	Roles                map[string]AgentRoleToml `json:"-" toml:"-"`
}

// MemoriesToml configures the memories subsystem. deny_unknown_fields. The Rust
// alias no_memories_if_mcp_or_web_search maps to disable_on_external_context and
// is handled during merge via key aliases.
type MemoriesToml struct {
	DisableOnExternalContext       *bool   `json:"disable_on_external_context" toml:"disable_on_external_context"`
	GenerateMemories               *bool   `json:"generate_memories" toml:"generate_memories"`
	UseMemories                    *bool   `json:"use_memories" toml:"use_memories"`
	DedicatedTools                 *bool   `json:"dedicated_tools" toml:"dedicated_tools"`
	MaxRawMemoriesForConsolidation *uint64 `json:"max_raw_memories_for_consolidation" toml:"max_raw_memories_for_consolidation"`
	MaxUnusedDays                  *int64  `json:"max_unused_days" toml:"max_unused_days"`
	MaxRolloutAgeDays              *int64  `json:"max_rollout_age_days" toml:"max_rollout_age_days"`
	MaxRolloutsPerStartup          *uint64 `json:"max_rollouts_per_startup" toml:"max_rollouts_per_startup"`
	MinRolloutIdleHours            *int64  `json:"min_rollout_idle_hours" toml:"min_rollout_idle_hours"`
	MinRateLimitRemainingPercent   *int64  `json:"min_rate_limit_remaining_percent" toml:"min_rate_limit_remaining_percent"`
	ExtractModel                   *string `json:"extract_model" toml:"extract_model"`
	ConsolidationModel             *string `json:"consolidation_model" toml:"consolidation_model"`
}

// OtelHttpProtocol selects the OTLP HTTP payload encoding. kebab-case.
type OtelHttpProtocol string

const (
	OtelHttpProtocolBinary OtelHttpProtocol = "binary"
	OtelHttpProtocolJson   OtelHttpProtocol = "json"
)

// OtelTlsConfig holds optional TLS material for OTLP exporters.
// deny_unknown_fields, kebab-case keys.
type OtelTlsConfig struct {
	CaCertificate     *string `json:"ca-certificate" toml:"ca-certificate"`
	ClientCertificate *string `json:"client-certificate" toml:"client-certificate"`
	ClientPrivateKey  *string `json:"client-private-key" toml:"client-private-key"`
}

// OtelConfigToml configures OpenTelemetry. deny_unknown_fields. exporter values
// are kept as opaque TOML value trees (externally-tagged enums with payloads).
type OtelConfigToml struct {
	LogUserPrompt   *bool                         `json:"log_user_prompt" toml:"log_user_prompt"`
	Environment     *string                       `json:"environment" toml:"environment"`
	Exporter        TomlValue                     `json:"exporter" toml:"exporter"`
	TraceExporter   TomlValue                     `json:"trace_exporter" toml:"trace_exporter"`
	MetricsExporter TomlValue                     `json:"metrics_exporter" toml:"metrics_exporter"`
	SpanAttributes  *map[string]string            `json:"span_attributes" toml:"span_attributes"`
	Tracestate      *map[string]map[string]string `json:"tracestate" toml:"tracestate"`
}

// PluginMcpServerConfig is a plugin-provided MCP server policy overlay.
// deny_unknown_fields; enabled defaults to true.
type PluginMcpServerConfig struct {
	Enabled                  bool                           `json:"enabled" toml:"enabled"`
	DefaultToolsApprovalMode *AppToolApproval               `json:"default_tools_approval_mode,omitempty" toml:"default_tools_approval_mode,omitempty"`
	EnabledTools             *[]string                      `json:"enabled_tools,omitempty" toml:"enabled_tools,omitempty"`
	DisabledTools            *[]string                      `json:"disabled_tools,omitempty" toml:"disabled_tools,omitempty"`
	Tools                    map[string]McpServerToolConfig `json:"tools,omitempty" toml:"tools,omitempty"`
}

// PluginConfig is a user-level plugin entry. deny_unknown_fields; enabled
// defaults to true; mcp_servers skipped when empty.
type PluginConfig struct {
	Enabled    bool                             `json:"enabled" toml:"enabled"`
	McpServers map[string]PluginMcpServerConfig `json:"mcp_servers,omitempty" toml:"mcp_servers,omitempty"`
}

// MarketplaceSourceType selects how a marketplace was installed. snake_case.
type MarketplaceSourceType string

const (
	MarketplaceSourceGit   MarketplaceSourceType = "git"
	MarketplaceSourceLocal MarketplaceSourceType = "local"
)

// MarketplaceConfig is a user-level marketplace entry. deny_unknown_fields.
type MarketplaceConfig struct {
	LastUpdated  *string                `json:"last_updated" toml:"last_updated"`
	LastRevision *string                `json:"last_revision" toml:"last_revision"`
	SourceType   *MarketplaceSourceType `json:"source_type" toml:"source_type"`
	Source       *string                `json:"source" toml:"source"`
	RefName      *string                `json:"ref" toml:"ref"`
	SparsePaths  *[]string              `json:"sparse_paths" toml:"sparse_paths"`
}

// ExternalConfigMigrationPrompts tracks suppressed migration prompts.
// deny_unknown_fields.
type ExternalConfigMigrationPrompts struct {
	Home                  *bool            `json:"home" toml:"home"`
	HomeLastPromptedAt    *int64           `json:"home_last_prompted_at" toml:"home_last_prompted_at"`
	Projects              map[string]bool  `json:"projects" toml:"projects"`
	ProjectLastPromptedAt map[string]int64 `json:"project_last_prompted_at" toml:"project_last_prompted_at"`
}

// Notice tracks acknowledged in-product notices. deny_unknown_fields. The
// gpt-5.1-codex-max key uses a literal serde rename with dashes/dots.
type Notice struct {
	HideFullAccessWarning            *bool                          `json:"hide_full_access_warning" toml:"hide_full_access_warning"`
	HideWorldWritableWarning         *bool                          `json:"hide_world_writable_warning" toml:"hide_world_writable_warning"`
	FastDefaultOptOut                *bool                          `json:"fast_default_opt_out" toml:"fast_default_opt_out"`
	HideRateLimitModelNudge          *bool                          `json:"hide_rate_limit_model_nudge" toml:"hide_rate_limit_model_nudge"`
	HideGpt51MigrationPrompt         *bool                          `json:"hide_gpt5_1_migration_prompt" toml:"hide_gpt5_1_migration_prompt"`
	HideGpt51CodexMaxMigrationPrompt *bool                          `json:"hide_gpt-5.1-codex-max_migration_prompt" toml:"hide_gpt-5.1-codex-max_migration_prompt"`
	ModelMigrations                  map[string]string              `json:"model_migrations" toml:"model_migrations"`
	ExternalConfigMigrationPrompts   ExternalConfigMigrationPrompts `json:"external_config_migration_prompts" toml:"external_config_migration_prompts"`
}

// ToolSuggestDiscoverableType discriminates a discoverable tool. snake_case.
type ToolSuggestDiscoverableType string

const (
	ToolSuggestDiscoverableConnector ToolSuggestDiscoverableType = "connector"
	ToolSuggestDiscoverablePlugin    ToolSuggestDiscoverableType = "plugin"
)

// ToolSuggestDiscoverable identifies a discoverable tool. deny_unknown_fields.
type ToolSuggestDiscoverable struct {
	Kind ToolSuggestDiscoverableType `json:"type" toml:"type"`
	ID   string                      `json:"id" toml:"id"`
}

// ToolSuggestDisabledTool identifies a disabled discoverable tool.
type ToolSuggestDisabledTool struct {
	Kind ToolSuggestDiscoverableType `json:"type" toml:"type"`
	ID   string                      `json:"id" toml:"id"`
}

// ToolSuggestConfig lists discoverable and disabled tools. deny_unknown_fields.
type ToolSuggestConfig struct {
	Discoverables []ToolSuggestDiscoverable `json:"discoverables" toml:"discoverables"`
	DisabledTools []ToolSuggestDisabledTool `json:"disabled_tools" toml:"disabled_tools"`
}

// RealtimeWsMode selects the realtime session type. snake_case; default
// Conversational.
type RealtimeWsMode string

const (
	RealtimeWsModeConversational RealtimeWsMode = "conversational"
	RealtimeWsModeTranscription  RealtimeWsMode = "transcription"
)

// RealtimeTransport selects the realtime transport. snake_case with a rename of
// WebRtc to "webrtc"; default WebRtc.
type RealtimeTransport string

const (
	RealtimeTransportWebRtc    RealtimeTransport = "webrtc"
	RealtimeTransportWebsocket RealtimeTransport = "websocket"
)

// RealtimeToml configures realtime websocket sessions (experimental).
// deny_unknown_fields. The session_type field is renamed to "type".
type RealtimeToml struct {
	Version     *protocol.RealtimeConversationVersion `json:"version" toml:"version"`
	SessionType *RealtimeWsMode                       `json:"type" toml:"type"`
	Transport   *RealtimeTransport                    `json:"transport" toml:"transport"`
	Voice       *protocol.RealtimeVoice               `json:"voice" toml:"voice"`
}

// RealtimeAudioToml holds machine-local audio device preferences.
// deny_unknown_fields.
type RealtimeAudioToml struct {
	Microphone *string `json:"microphone" toml:"microphone"`
	Speaker    *string `json:"speaker" toml:"speaker"`
}

// ThreadStoreToml selects the thread store implementation (internally tagged,
// tag = "type", snake_case). Represented opaquely to round-trip faithfully.
type ThreadStoreToml = TomlValue

// AppsConfigToml, SkillsConfig, PermissionsToml, and the TUI keymap are large
// open-ended schemas. To preserve arbitrary nested structure byte-for-byte they
// are carried as opaque TOML value trees on ConfigToml.
type AppsConfigToml = TomlValue

// SkillsConfig is the user-level skills config; opaque value tree.
type SkillsConfig = TomlValue

// PermissionsToml is the named permission-profiles table; opaque value tree.
type PermissionsToml = TomlValue
