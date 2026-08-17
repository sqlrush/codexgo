package mcp

import (
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/pkg/config"
)

// EnvLookup resolves an environment variable, returning its value and whether it
// was present. It is satisfied by os.LookupEnv and overridden in tests.
type EnvLookup func(name string) (string, bool)

// BuildStdioEnv assembles the environment for a local stdio MCP server. It mirrors
// the reference `create_env_for_mcp_server`: a fixed allowlist of default
// variables (DefaultStdioEnvVars) plus any names explicitly listed in env_vars
// are forwarded from the current environment, then literal extra entries from the
// server config override anything inherited.
//
// An env_vars entry whose source is "remote" is rejected for the local launcher,
// matching the reference behavior. lookup may be nil to read the process
// environment.
func BuildStdioEnv(extra map[string]string, envVars []config.McpServerEnvVar, lookup EnvLookup) (map[string]string, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}

	for _, ev := range envVars {
		if ev.Source != nil && *ev.Source == "remote" {
			return nil, fmt.Errorf("mcp: env_vars entry %q uses source `remote`, which requires remote MCP stdio", ev.Name)
		}
	}

	out := make(map[string]string)
	for _, name := range DefaultStdioEnvVars {
		if value, ok := lookup(name); ok {
			out[name] = value
		}
	}
	for _, ev := range envVars {
		if value, ok := lookup(ev.Name); ok {
			out[ev.Name] = value
		}
	}
	for key, value := range extra {
		out[key] = value
	}
	return out, nil
}
