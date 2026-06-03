package features

// FeatureConfig is implemented by custom feature config structs that need more
// configuration than a simple enabled/disabled boolean. Mirrors the Rust
// `FeatureConfig` trait.
type FeatureConfig interface {
	// Enabled reports the configured enabled state, or nil when unspecified.
	Enabled() *bool
	// SetEnabled forces the enabled state.
	SetEnabled(enabled bool)
}

// MultiAgentV2ConfigToml is the `[features.multi_agent_v2]` config table.
// Mirrors the Rust struct (serde rename: snake_case field names, all optional,
// skip_serializing_if Option::is_none, deny_unknown_fields).
//
// The `enabled` field is named EnabledFlag in Go so the type can also satisfy
// the FeatureConfig interface (which requires an Enabled() method); the wire
// key remains `enabled` via the struct tags.
type MultiAgentV2ConfigToml struct {
	EnabledFlag                    *bool   `toml:"enabled,omitempty" json:"enabled,omitempty"`
	MaxConcurrentThreadsPerSession *uint64 `toml:"max_concurrent_threads_per_session,omitempty" json:"max_concurrent_threads_per_session,omitempty"`
	MinWaitTimeoutMs               *int64  `toml:"min_wait_timeout_ms,omitempty" json:"min_wait_timeout_ms,omitempty"`
	MaxWaitTimeoutMs               *int64  `toml:"max_wait_timeout_ms,omitempty" json:"max_wait_timeout_ms,omitempty"`
	DefaultWaitTimeoutMs           *int64  `toml:"default_wait_timeout_ms,omitempty" json:"default_wait_timeout_ms,omitempty"`
	UsageHintEnabled               *bool   `toml:"usage_hint_enabled,omitempty" json:"usage_hint_enabled,omitempty"`
	UsageHintText                  *string `toml:"usage_hint_text,omitempty" json:"usage_hint_text,omitempty"`
	RootAgentUsageHintText         *string `toml:"root_agent_usage_hint_text,omitempty" json:"root_agent_usage_hint_text,omitempty"`
	SubagentUsageHintText          *string `toml:"subagent_usage_hint_text,omitempty" json:"subagent_usage_hint_text,omitempty"`
	ToolNamespace                  *string `toml:"tool_namespace,omitempty" json:"tool_namespace,omitempty"`
	HideSpawnAgentMetadata         *bool   `toml:"hide_spawn_agent_metadata,omitempty" json:"hide_spawn_agent_metadata,omitempty"`
	NonCodeModeOnly                *bool   `toml:"non_code_mode_only,omitempty" json:"non_code_mode_only,omitempty"`
}

// Enabled implements FeatureConfig.
func (c *MultiAgentV2ConfigToml) Enabled() *bool { return c.EnabledFlag }

// SetEnabled implements FeatureConfig.
func (c *MultiAgentV2ConfigToml) SetEnabled(enabled bool) { c.EnabledFlag = boolPtr(enabled) }

// AppsMcpPathOverrideConfigToml is the `[features.apps_mcp_path_override]`
// config table. Mirrors the Rust struct.
type AppsMcpPathOverrideConfigToml struct {
	EnabledFlag *bool   `toml:"enabled,omitempty" json:"enabled,omitempty"`
	Path        *string `toml:"path,omitempty" json:"path,omitempty"`
}

// Enabled implements FeatureConfig. Mirrors the Rust
// `self.enabled.or(self.path.as_ref().map(|_| true))`: an explicit `enabled`
// wins, otherwise a present `path` implies enabled=true.
func (c *AppsMcpPathOverrideConfigToml) Enabled() *bool {
	if c.EnabledFlag != nil {
		return c.EnabledFlag
	}
	if c.Path != nil {
		return boolPtr(true)
	}
	return nil
}

// SetEnabled implements FeatureConfig.
func (c *AppsMcpPathOverrideConfigToml) SetEnabled(enabled bool) { c.EnabledFlag = boolPtr(enabled) }

// NetworkProxyConfigToml is the `[features.network_proxy]` config table.
// Mirrors the Rust struct.
type NetworkProxyConfigToml struct {
	EnabledFlag                      *bool                                           `toml:"enabled,omitempty" json:"enabled,omitempty"`
	ProxyURL                         *string                                         `toml:"proxy_url,omitempty" json:"proxy_url,omitempty"`
	EnableSocks5                     *bool                                           `toml:"enable_socks5,omitempty" json:"enable_socks5,omitempty"`
	SocksURL                         *string                                         `toml:"socks_url,omitempty" json:"socks_url,omitempty"`
	EnableSocks5Udp                  *bool                                           `toml:"enable_socks5_udp,omitempty" json:"enable_socks5_udp,omitempty"`
	AllowUpstreamProxy               *bool                                           `toml:"allow_upstream_proxy,omitempty" json:"allow_upstream_proxy,omitempty"`
	DangerouslyAllowNonLoopbackProxy *bool                                           `toml:"dangerously_allow_non_loopback_proxy,omitempty" json:"dangerously_allow_non_loopback_proxy,omitempty"`
	DangerouslyAllowAllUnixSockets   *bool                                           `toml:"dangerously_allow_all_unix_sockets,omitempty" json:"dangerously_allow_all_unix_sockets,omitempty"`
	Mode                             *NetworkProxyModeToml                           `toml:"mode,omitempty" json:"mode,omitempty"`
	Domains                          map[string]NetworkProxyDomainPermissionToml     `toml:"domains,omitempty" json:"domains,omitempty"`
	UnixSockets                      map[string]NetworkProxyUnixSocketPermissionToml `toml:"unix_sockets,omitempty" json:"unix_sockets,omitempty"`
	AllowLocalBinding                *bool                                           `toml:"allow_local_binding,omitempty" json:"allow_local_binding,omitempty"`
}

// Enabled implements FeatureConfig.
func (c *NetworkProxyConfigToml) Enabled() *bool { return c.EnabledFlag }

// SetEnabled implements FeatureConfig.
func (c *NetworkProxyConfigToml) SetEnabled(enabled bool) { c.EnabledFlag = boolPtr(enabled) }

// NetworkProxyModeToml mirrors the Rust enum (serde rename_all = "lowercase").
type NetworkProxyModeToml string

const (
	// NetworkProxyModeLimited corresponds to the Rust `Limited` variant.
	NetworkProxyModeLimited NetworkProxyModeToml = "limited"
	// NetworkProxyModeFull corresponds to the Rust `Full` variant.
	NetworkProxyModeFull NetworkProxyModeToml = "full"
)

// NetworkProxyDomainPermissionToml mirrors the Rust enum (rename_all =
// "lowercase").
type NetworkProxyDomainPermissionToml string

const (
	// NetworkProxyDomainAllow corresponds to the Rust `Allow` variant.
	NetworkProxyDomainAllow NetworkProxyDomainPermissionToml = "allow"
	// NetworkProxyDomainDeny corresponds to the Rust `Deny` variant.
	NetworkProxyDomainDeny NetworkProxyDomainPermissionToml = "deny"
)

// NetworkProxyUnixSocketPermissionToml mirrors the Rust enum (rename_all =
// "lowercase").
type NetworkProxyUnixSocketPermissionToml string

const (
	// NetworkProxyUnixSocketAllow corresponds to the Rust `Allow` variant.
	NetworkProxyUnixSocketAllow NetworkProxyUnixSocketPermissionToml = "allow"
	// NetworkProxyUnixSocketDeny corresponds to the Rust `Deny` variant.
	NetworkProxyUnixSocketDeny NetworkProxyUnixSocketPermissionToml = "deny"
)

// boolPtr returns a pointer to b.
func boolPtr(b bool) *bool { return &b }
