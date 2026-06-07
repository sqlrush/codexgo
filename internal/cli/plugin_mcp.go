package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/plugins"
)

// pluginRootPlaceholders are substituted with the resolved plugin install root
// in a plugin's .mcp.json command/args/cwd/env values. CODEX_PLUGIN_ROOT is the
// codex convention; the CLAUDE_/bare aliases match the reference's plugin env.
var pluginRootPlaceholders = []string{"${CODEX_PLUGIN_ROOT}", "${CLAUDE_PLUGIN_ROOT}", "${PLUGIN_ROOT}"}

// discoverPluginMcpServers resolves the MCP servers contributed by enabled,
// installed plugins. For each enabled plugin in the [plugins] config table it
// resolves the active install root from the plugin store, loads the manifest,
// reads the referenced .mcp.json, substitutes the plugin-root placeholders, and
// parses the entries into McpServerConfig. First definition wins on name
// collisions across plugins. A broken plugin yields a warning and is skipped
// rather than aborting discovery.
func discoverPluginMcpServers(codexHome string, pluginsCfg map[string]config.PluginConfig) (map[string]config.McpServerConfig, []string) {
	out := map[string]config.McpServerConfig{}
	if codexHome == "" || len(pluginsCfg) == 0 {
		return out, nil
	}
	store, err := plugins.NewPluginStore(codexHome)
	if err != nil {
		return out, []string{fmt.Sprintf("plugin MCP discovery unavailable: %v", err)}
	}

	var warnings []string
	for _, configName := range sortedKeys(pluginsCfg) {
		pcfg := pluginsCfg[configName]
		if !pcfg.Enabled {
			continue
		}
		id, err := plugins.ParsePluginID(configName)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("plugin %q: %v", configName, err))
			continue
		}
		root, ok := store.ActivePluginRoot(id)
		if !ok {
			continue // listed but not installed in the cache; nothing to launch
		}
		manifest, ok := plugins.LoadPluginManifest(root.Path())
		if !ok || manifest.Paths.McpServers == nil {
			continue // no manifest, or no mcpServers declared
		}
		mcpPath := manifest.Paths.McpServers.Path()
		data, err := os.ReadFile(mcpPath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("plugin %q: read %s: %v", configName, mcpPath, err))
			continue
		}
		servers, err := parsePluginMcpJSON(data)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("plugin %q: parse %s: %v", configName, mcpPath, err))
			continue
		}
		for _, name := range sortedKeys(servers) {
			if _, dup := out[name]; dup {
				continue // first plugin to define a name wins
			}
			out[name] = substitutePluginRoot(servers[name], root.Path())
		}
	}
	return out, warnings
}

// parsePluginMcpJSON parses a plugin .mcp.json, supporting both the wrapped
// `{"mcpServers": {name: cfg}}` form and the bare `{name: cfg}` form.
func parsePluginMcpJSON(data []byte) (map[string]config.McpServerConfig, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	entries := top
	if inner, ok := top["mcpServers"]; ok && len(top) == 1 {
		var wrapped map[string]json.RawMessage
		if err := json.Unmarshal(inner, &wrapped); err != nil {
			return nil, fmt.Errorf("mcpServers: %w", err)
		}
		entries = wrapped
	}
	out := make(map[string]config.McpServerConfig, len(entries))
	for name, raw := range entries {
		var scfg config.McpServerConfig
		if err := json.Unmarshal(raw, &scfg); err != nil {
			return nil, fmt.Errorf("server %q: %w", name, err)
		}
		out[name] = scfg
	}
	return out, nil
}

// substitutePluginRoot returns a copy of cfg with the plugin-root placeholders
// in its stdio transport (command/args/cwd/env) replaced by root. The input is
// not mutated (the maps/slices are rebuilt).
func substitutePluginRoot(cfg config.McpServerConfig, root string) config.McpServerConfig {
	t := cfg.Transport
	t.Command = replacePluginRoot(t.Command, root)
	if len(t.Args) > 0 {
		args := make([]string, len(t.Args))
		for i, a := range t.Args {
			args[i] = replacePluginRoot(a, root)
		}
		t.Args = args
	}
	if t.Cwd != nil {
		c := replacePluginRoot(*t.Cwd, root)
		t.Cwd = &c
	}
	if t.Env != nil {
		env := make(map[string]string, len(*t.Env))
		for k, v := range *t.Env {
			env[k] = replacePluginRoot(v, root)
		}
		t.Env = &env
	}
	cfg.Transport = t
	return cfg
}

func replacePluginRoot(s, root string) string {
	for _, ph := range pluginRootPlaceholders {
		s = strings.ReplaceAll(s, ph, root)
	}
	return s
}

// sortedKeys returns the map keys in sorted order for deterministic iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
