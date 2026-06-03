package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeServer(t *testing.T, body string) (McpServerConfig, error) {
	t.Helper()
	value, err := ParseTomlValue([]byte(body))
	if err != nil {
		t.Fatalf("parse toml: %v", err)
	}
	data, err := json.Marshal(value.(map[string]any)["server"])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var cfg McpServerConfig
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

func TestMcpStdioTransport(t *testing.T) {
	cfg, err := decodeServer(t, `
[server]
command = "my-server"
args = ["--flag"]
env = { FOO = "bar" }
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport.Kind != McpTransportStdio {
		t.Fatalf("transport kind = %v", cfg.Transport.Kind)
	}
	if cfg.Transport.Command != "my-server" {
		t.Fatalf("command = %q", cfg.Transport.Command)
	}
	if len(cfg.Transport.Args) != 1 || cfg.Transport.Args[0] != "--flag" {
		t.Fatalf("args = %v", cfg.Transport.Args)
	}
	if cfg.EnvironmentID != DefaultMcpServerEnvironmentID {
		t.Fatalf("environment_id = %q", cfg.EnvironmentID)
	}
	if !cfg.Enabled {
		t.Fatalf("enabled default should be true")
	}
}

func TestMcpStreamableHTTPTransport(t *testing.T) {
	cfg, err := decodeServer(t, `
[server]
url = "https://example.com/mcp"
bearer_token_env_var = "TOKEN"
`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Transport.Kind != McpTransportStreamableHTTP {
		t.Fatalf("transport kind = %v", cfg.Transport.Kind)
	}
	if cfg.Transport.URL != "https://example.com/mcp" {
		t.Fatalf("url = %q", cfg.Transport.URL)
	}
	if cfg.Transport.BearerTokenEnvVar == nil || *cfg.Transport.BearerTokenEnvVar != "TOKEN" {
		t.Fatalf("bearer_token_env_var = %v", cfg.Transport.BearerTokenEnvVar)
	}
}

func TestMcpTransportValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "stdio with url",
			body: "[server]\ncommand = \"x\"\nurl = \"https://y\"\n",
			want: "url is not supported for stdio",
		},
		{
			name: "http with args",
			body: "[server]\nurl = \"https://y\"\nargs = [\"a\"]\n",
			want: "args is not supported for streamable_http",
		},
		{
			name: "no transport",
			body: "[server]\nenabled = true\n",
			want: "invalid transport",
		},
		{
			name: "remote stdio without abs cwd",
			body: "[server]\ncommand = \"x\"\nenvironment_id = \"remote\"\n",
			want: "require an absolute cwd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeServer(t, tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestMcpStartupTimeoutMsConversion(t *testing.T) {
	cfg, err := decodeServer(t, "[server]\ncommand = \"x\"\nstartup_timeout_ms = 2500\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.StartupTimeoutSec == nil || *cfg.StartupTimeoutSec != 2.5 {
		t.Fatalf("startup_timeout_sec = %v", cfg.StartupTimeoutSec)
	}
}

func TestMcpServerMarshalSkipsAndTransport(t *testing.T) {
	cfg, err := decodeServer(t, "[server]\ncommand = \"x\"\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := out["command"]; !ok {
		t.Fatalf("flattened command missing: %v", out)
	}
	if _, ok := out["tool_timeout_sec"]; !ok {
		t.Fatalf("tool_timeout_sec should always serialize (got %v)", out)
	}
	if _, ok := out["required"]; ok {
		t.Fatalf("required should be skipped when false: %v", out)
	}
}

func TestMcpEnvVarForms(t *testing.T) {
	var bare McpServerEnvVar
	if err := json.Unmarshal([]byte(`"FOO"`), &bare); err != nil {
		t.Fatalf("bare: %v", err)
	}
	if bare.Name != "FOO" || bare.Source != nil {
		t.Fatalf("bare = %#v", bare)
	}
	out, _ := json.Marshal(bare)
	if string(out) != `"FOO"` {
		t.Fatalf("bare marshal = %s", out)
	}

	var cfg McpServerEnvVar
	if err := json.Unmarshal([]byte(`{"name":"BAR","source":"remote"}`), &cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Name != "BAR" || cfg.Source == nil || *cfg.Source != "remote" {
		t.Fatalf("config = %#v", cfg)
	}
	if err := cfg.ValidateSource(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	bad := McpServerEnvVar{Name: "x", Source: strPtr("bogus")}
	if err := bad.ValidateSource(); err == nil {
		t.Fatalf("expected validate error")
	}
}

func strPtr(s string) *string { return &s }
