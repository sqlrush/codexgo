package cli

import (
	"context"
	"fmt"
	"os"

	syskeyring "github.com/sqlrush/codexgo/internal/keyring/system"
	"github.com/sqlrush/codexgo/pkg/config"
	"github.com/sqlrush/codexgo/pkg/mcp"
)

// effectiveMcpServers merges the explicitly-configured [mcp_servers] with the
// servers auto-discovered from enabled, installed plugins. Configured servers
// take precedence on name collisions (a user override wins over a plugin's
// default), mirroring the reference effective_mcp_servers precedence. Plugin
// discovery warnings are logged but never abort the assembly.
func effectiveMcpServers(codexHome string, configured map[string]config.McpServerConfig, pluginsCfg map[string]config.PluginConfig) map[string]config.McpServerConfig {
	out := map[string]config.McpServerConfig{}
	discovered, warnings := discoverPluginMcpServers(codexHome, pluginsCfg)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	for name, cfg := range discovered {
		out[name] = cfg
	}
	for name, cfg := range configured {
		out[name] = cfg // configured [mcp_servers] overrides a plugin's server
	}
	return out
}

// buildMcpManager launches the configured MCP servers and returns the manager,
// or nil when none are configured (or startup of the whole set errors). It is
// the bridge that connects the previously-unwired MCP client to the runtime:
// the returned manager is injected into the per-thread tool router so external
// MCP server tools (e.g. the gaussdb DB plugin) become callable by the model.
//
// Per-server startup failures are logged and skipped — a broken server must not
// take down the assembly — mirroring NewManager's partial-failure contract.
// Servers run for the process lifetime; their stdio subprocesses exit when
// codexgo exits and closes their stdin, so no explicit shutdown is wired here
// (the manager is process-wide and shared across threads).
func buildMcpManager(ctx context.Context, codexHome string, servers map[string]config.McpServerConfig) *mcp.Manager {
	if len(servers) == 0 {
		return nil
	}
	manager, results, err := mcp.NewManager(ctx, servers, mcp.ManagerOptions{
		CodexHome:   codexHome,
		FallbackCwd: resolveCwd(),
		Keyring:     syskeyring.NewDefaultStore(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: MCP manager init failed, MCP tools disabled: %v\n", err)
		return nil
	}
	ready := 0
	for _, r := range results {
		switch r.Status {
		case mcp.StartupReady:
			ready++
		case mcp.StartupFailed:
			fmt.Fprintf(os.Stderr, "warning: MCP server %q failed to start: %v\n", r.ServerName, r.Err)
		}
	}
	if ready == 0 {
		return nil
	}
	return manager
}
