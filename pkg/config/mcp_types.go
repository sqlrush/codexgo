package config

import (
	"encoding/json"
	"fmt"
)

// AppToolApproval is the approval mode for an app/MCP tool. snake_case; default
// Auto.
type AppToolApproval string

const (
	AppToolApprovalAuto    AppToolApproval = "auto"
	AppToolApprovalPrompt  AppToolApproval = "prompt"
	AppToolApprovalApprove AppToolApproval = "approve"
)

// McpServerToolConfig holds per-tool approval settings. deny_unknown_fields;
// approval_mode is skipped when None.
type McpServerToolConfig struct {
	ApprovalMode *AppToolApproval `json:"approval_mode,omitempty" toml:"approval_mode,omitempty"`
}

// McpServerOAuthConfig holds OAuth client settings. deny_unknown_fields.
type McpServerOAuthConfig struct {
	ClientID *string `json:"client_id,omitempty" toml:"client_id,omitempty"`
}

// McpServerEnvVar is an untagged enum: either a bare name string or a {name,
// source} config. deny_unknown_fields on the config arm.
type McpServerEnvVar struct {
	// Name is always populated.
	Name string
	// Source is the optional source ("local" or "remote"); nil for the bare
	// string form.
	Source *string
	// isConfig records whether the original form was the object form, so we
	// round-trip the representation faithfully.
	isConfig bool
}

// MarshalJSON emits the bare-string or {name, source} form.
func (e McpServerEnvVar) MarshalJSON() ([]byte, error) {
	if !e.isConfig {
		return json.Marshal(e.Name)
	}
	obj := struct {
		Name   string  `json:"name"`
		Source *string `json:"source,omitempty"`
	}{Name: e.Name, Source: e.Source}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes the bare-string or object form.
func (e *McpServerEnvVar) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		e.Name = s
		e.Source = nil
		e.isConfig = false
		return nil
	}
	var obj struct {
		Name   string  `json:"name"`
		Source *string `json:"source"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("env_vars entry must be a string or {name, source}: %w", err)
	}
	e.Name = obj.Name
	e.Source = obj.Source
	e.isConfig = true
	return nil
}

// ValidateSource checks the optional source value.
func (e McpServerEnvVar) ValidateSource() error {
	if e.Source == nil {
		return nil
	}
	switch *e.Source {
	case "local", "remote":
		return nil
	default:
		return fmt.Errorf("unsupported env_vars source `%s`; expected `local` or `remote`", *e.Source)
	}
}

// McpServerTransportKind discriminates the transport union.
type McpServerTransportKind int

const (
	// McpTransportStdio is the stdio transport.
	McpTransportStdio McpServerTransportKind = iota
	// McpTransportStreamableHTTP is the streamable HTTP transport.
	McpTransportStreamableHTTP
)

// McpServerTransportConfig is the untagged transport union. Only the fields for
// the active Kind are populated.
type McpServerTransportConfig struct {
	Kind McpServerTransportKind

	// Stdio fields.
	Command string
	Args    []string
	Env     *map[string]string
	EnvVars []McpServerEnvVar
	Cwd     *string

	// StreamableHTTP fields.
	URL               string
	BearerTokenEnvVar *string
	HTTPHeaders       *map[string]string
	EnvHTTPHeaders    *map[string]string
}

// MarshalJSON emits the flattened transport fields with serde skip rules.
func (t McpServerTransportConfig) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	switch t.Kind {
	case McpTransportStdio:
		m["command"] = t.Command
		m["args"] = t.Args
		if t.Env != nil {
			m["env"] = *t.Env
		}
		if len(t.EnvVars) > 0 {
			m["env_vars"] = t.EnvVars
		}
		if t.Cwd != nil {
			m["cwd"] = *t.Cwd
		}
	case McpTransportStreamableHTTP:
		m["url"] = t.URL
		if t.BearerTokenEnvVar != nil {
			m["bearer_token_env_var"] = *t.BearerTokenEnvVar
		}
		if t.HTTPHeaders != nil {
			m["http_headers"] = *t.HTTPHeaders
		}
		if t.EnvHTTPHeaders != nil {
			m["env_http_headers"] = *t.EnvHTTPHeaders
		}
	}
	return json.Marshal(m)
}

// McpServerConfig is the resolved per-server config derived from the raw input
// shape. The transport is flattened on serialize.
type McpServerConfig struct {
	Transport                McpServerTransportConfig
	EnvironmentID            string
	Enabled                  bool
	Required                 bool
	SupportsParallelToolCall bool
	// StartupTimeoutSec is the startup timeout in seconds (serde with
	// option_duration_secs => f64 seconds), skipped when nil.
	StartupTimeoutSec *float64
	// ToolTimeoutSec is the tool timeout in seconds; serialized as null when nil
	// (no skip).
	ToolTimeoutSec           *float64
	DefaultToolsApprovalMode *AppToolApproval
	EnabledTools             *[]string
	DisabledTools            *[]string
	Scopes                   *[]string
	OAuth                    *McpServerOAuthConfig
	OAuthResource            *string
	Tools                    map[string]McpServerToolConfig
}

// MarshalJSON serializes McpServerConfig with the flattened transport and serde
// skip rules. disabled_reason is skipped (serde skip).
func (c McpServerConfig) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	transportJSON, err := c.Transport.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var transportMap map[string]any
	if err := json.Unmarshal(transportJSON, &transportMap); err != nil {
		return nil, err
	}
	for k, v := range transportMap {
		out[k] = v
	}
	out["environment_id"] = c.EnvironmentID
	out["enabled"] = c.Enabled
	if c.Required {
		out["required"] = c.Required
	}
	if c.SupportsParallelToolCall {
		out["supports_parallel_tool_calls"] = c.SupportsParallelToolCall
	}
	if c.StartupTimeoutSec != nil {
		out["startup_timeout_sec"] = *c.StartupTimeoutSec
	}
	// tool_timeout_sec has no skip_serializing_if; always emitted (null if nil).
	if c.ToolTimeoutSec != nil {
		out["tool_timeout_sec"] = *c.ToolTimeoutSec
	} else {
		out["tool_timeout_sec"] = nil
	}
	if c.DefaultToolsApprovalMode != nil {
		out["default_tools_approval_mode"] = *c.DefaultToolsApprovalMode
	}
	if c.EnabledTools != nil {
		out["enabled_tools"] = *c.EnabledTools
	}
	if c.DisabledTools != nil {
		out["disabled_tools"] = *c.DisabledTools
	}
	if c.Scopes != nil {
		out["scopes"] = *c.Scopes
	}
	if c.OAuth != nil {
		out["oauth"] = c.OAuth
	}
	if c.OAuthResource != nil {
		out["oauth_resource"] = *c.OAuthResource
	}
	if len(c.Tools) > 0 {
		out["tools"] = c.Tools
	}
	return json.Marshal(out)
}

// IsLocalEnvironment reports whether the server runs in the local environment.
func (c McpServerConfig) IsLocalEnvironment() bool {
	return c.EnvironmentID == DefaultMcpServerEnvironmentID
}

// rawMcpServerConfig is the input shape used for deserialization, mirroring the
// Rust RawMcpServerConfig. deny_unknown_fields.
type rawMcpServerConfig struct {
	Command            *string                         `json:"command"`
	Args               *[]string                       `json:"args"`
	Env                *map[string]string              `json:"env"`
	EnvVars            *[]McpServerEnvVar              `json:"env_vars"`
	Cwd                *string                         `json:"cwd"`
	HTTPHeaders        *map[string]string              `json:"http_headers"`
	EnvHTTPHeaders     *map[string]string              `json:"env_http_headers"`
	URL                *string                         `json:"url"`
	BearerToken        *string                         `json:"bearer_token"`
	BearerTokenEnvVar  *string                         `json:"bearer_token_env_var"`
	EnvironmentID      *string                         `json:"environment_id"`
	StartupTimeoutSec  *float64                        `json:"startup_timeout_sec"`
	StartupTimeoutMS   *uint64                         `json:"startup_timeout_ms"`
	ToolTimeoutSec     *float64                        `json:"tool_timeout_sec"`
	Enabled            *bool                           `json:"enabled"`
	Required           *bool                           `json:"required"`
	SupportsParallel   *bool                           `json:"supports_parallel_tool_calls"`
	DefaultToolsApprov *AppToolApproval                `json:"default_tools_approval_mode"`
	EnabledTools       *[]string                       `json:"enabled_tools"`
	DisabledTools      *[]string                       `json:"disabled_tools"`
	Scopes             *[]string                       `json:"scopes"`
	OAuth              *McpServerOAuthConfig           `json:"oauth"`
	OAuthResource      *string                         `json:"oauth_resource"`
	Name               *string                         `json:"name"`
	Tools              *map[string]McpServerToolConfig `json:"tools"`
}

// UnmarshalJSON parses the raw input shape and converts it to the resolved
// McpServerConfig, applying transport validation and defaults.
func (c *McpServerConfig) UnmarshalJSON(data []byte) error {
	var raw rawMcpServerConfig
	dec := json.NewDecoder(bytesReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("decode mcp server config: %w", err)
	}
	resolved, err := raw.toConfig()
	if err != nil {
		return err
	}
	*c = resolved
	return nil
}

func (raw rawMcpServerConfig) toConfig() (McpServerConfig, error) {
	startup, err := resolveStartupTimeout(raw.StartupTimeoutSec, raw.StartupTimeoutMS)
	if err != nil {
		return McpServerConfig{}, err
	}

	var transport McpServerTransportConfig
	switch {
	case raw.Command != nil:
		if err := throwIfSetStr("stdio", "url", raw.URL); err != nil {
			return McpServerConfig{}, err
		}
		if err := throwIfSetStr("stdio", "bearer_token_env_var", raw.BearerTokenEnvVar); err != nil {
			return McpServerConfig{}, err
		}
		if err := throwIfSetStr("stdio", "bearer_token", raw.BearerToken); err != nil {
			return McpServerConfig{}, err
		}
		if err := throwIfSetMap("stdio", "http_headers", raw.HTTPHeaders); err != nil {
			return McpServerConfig{}, err
		}
		if err := throwIfSetMap("stdio", "env_http_headers", raw.EnvHTTPHeaders); err != nil {
			return McpServerConfig{}, err
		}
		if raw.OAuth != nil {
			return McpServerConfig{}, fmt.Errorf("oauth is not supported for stdio")
		}
		if err := throwIfSetStr("stdio", "oauth_resource", raw.OAuthResource); err != nil {
			return McpServerConfig{}, err
		}
		envVars := derefSlice(raw.EnvVars)
		for _, ev := range envVars {
			if err := ev.ValidateSource(); err != nil {
				return McpServerConfig{}, err
			}
		}
		transport = McpServerTransportConfig{
			Kind:    McpTransportStdio,
			Command: *raw.Command,
			Args:    derefStringSlice(raw.Args),
			Env:     raw.Env,
			EnvVars: envVars,
			Cwd:     raw.Cwd,
		}
	case raw.URL != nil:
		if err := throwIfSet("streamable_http", "args", raw.Args); err != nil {
			return McpServerConfig{}, err
		}
		if raw.Env != nil {
			return McpServerConfig{}, fmt.Errorf("env is not supported for streamable_http")
		}
		if raw.EnvVars != nil {
			return McpServerConfig{}, fmt.Errorf("env_vars is not supported for streamable_http")
		}
		if err := throwIfSetStr("streamable_http", "cwd", raw.Cwd); err != nil {
			return McpServerConfig{}, err
		}
		if err := throwIfSetStr("streamable_http", "bearer_token", raw.BearerToken); err != nil {
			return McpServerConfig{}, err
		}
		transport = McpServerTransportConfig{
			Kind:              McpTransportStreamableHTTP,
			URL:               *raw.URL,
			BearerTokenEnvVar: raw.BearerTokenEnvVar,
			HTTPHeaders:       raw.HTTPHeaders,
			EnvHTTPHeaders:    raw.EnvHTTPHeaders,
		}
	default:
		return McpServerConfig{}, fmt.Errorf("invalid transport")
	}

	environmentID := DefaultMcpServerEnvironmentID
	if raw.EnvironmentID != nil {
		environmentID = *raw.EnvironmentID
	}
	if err := validateRemoteStdioCwd(transport, environmentID); err != nil {
		return McpServerConfig{}, err
	}

	enabled := true
	if raw.Enabled != nil {
		enabled = *raw.Enabled
	}
	return McpServerConfig{
		Transport:                transport,
		EnvironmentID:            environmentID,
		StartupTimeoutSec:        startup,
		ToolTimeoutSec:           raw.ToolTimeoutSec,
		Enabled:                  enabled,
		Required:                 derefBool(raw.Required),
		SupportsParallelToolCall: derefBool(raw.SupportsParallel),
		DefaultToolsApprovalMode: raw.DefaultToolsApprov,
		EnabledTools:             raw.EnabledTools,
		DisabledTools:            raw.DisabledTools,
		Scopes:                   raw.Scopes,
		OAuth:                    raw.OAuth,
		OAuthResource:            raw.OAuthResource,
		Tools:                    derefToolsMap(raw.Tools),
	}, nil
}

func resolveStartupTimeout(sec *float64, ms *uint64) (*float64, error) {
	switch {
	case sec != nil:
		if *sec < 0 {
			return nil, fmt.Errorf("startup_timeout_sec must be non-negative")
		}
		v := *sec
		return &v, nil
	case ms != nil:
		v := float64(*ms) / 1000.0
		return &v, nil
	default:
		return nil, nil
	}
}

func validateRemoteStdioCwd(transport McpServerTransportConfig, environmentID string) error {
	if environmentID == DefaultMcpServerEnvironmentID {
		return nil
	}
	if transport.Kind != McpTransportStdio {
		return nil
	}
	if transport.Cwd == nil {
		return fmt.Errorf("remote stdio MCP servers require an absolute cwd when environment_id is `%s`", environmentID)
	}
	if isAbsPath(*transport.Cwd) {
		return nil
	}
	return fmt.Errorf("remote stdio MCP servers require an absolute cwd when environment_id is `%s`, got `%s`", environmentID, *transport.Cwd)
}

func throwIfSet[T any](transport, field string, value *[]T) error {
	if value == nil {
		return nil
	}
	return fmt.Errorf("%s is not supported for %s", field, transport)
}

func throwIfSetStr(transport, field string, value *string) error {
	if value == nil {
		return nil
	}
	return fmt.Errorf("%s is not supported for %s", field, transport)
}

func throwIfSetMap(transport, field string, value *map[string]string) error {
	if value == nil {
		return nil
	}
	return fmt.Errorf("%s is not supported for %s", field, transport)
}

func derefSlice(v *[]McpServerEnvVar) []McpServerEnvVar {
	if v == nil {
		return nil
	}
	return *v
}

func derefStringSlice(v *[]string) []string {
	if v == nil {
		return nil
	}
	return *v
}

func derefBool(v *bool) bool {
	return v != nil && *v
}

func derefToolsMap(v *map[string]McpServerToolConfig) map[string]McpServerToolConfig {
	if v == nil {
		return map[string]McpServerToolConfig{}
	}
	return *v
}
