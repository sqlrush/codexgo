package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
)

// installFakePlugin lays out a plugin in the store cache under codexHome with the
// given manifest .mcp.json body, returning the resolved install root.
func installFakePlugin(t *testing.T, codexHome, marketplace, name, mcpJSON string) string {
	t.Helper()
	root := filepath.Join(codexHome, "plugins", "cache", marketplace, name, "local")
	manifestDir := filepath.Join(root, ".codex-plugin")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{"name":"` + name + `","mcpServers":"./.mcp.json"}`
	if err := os.WriteFile(filepath.Join(manifestDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(mcpJSON), 0o644); err != nil {
		t.Fatalf("write .mcp.json: %v", err)
	}
	return root
}

func TestParsePluginMcpJSONBothForms(t *testing.T) {
	wrapped := `{"mcpServers":{"demo":{"command":"/bin/demo","args":["x"]}}}`
	bare := `{"demo":{"command":"/bin/demo","args":["x"]}}`
	for _, body := range []string{wrapped, bare} {
		servers, err := parsePluginMcpJSON([]byte(body))
		if err != nil {
			t.Fatalf("parse %s: %v", body, err)
		}
		s, ok := servers["demo"]
		if !ok {
			t.Fatalf("missing demo in %s", body)
		}
		if s.Transport.Command != "/bin/demo" {
			t.Errorf("command = %q want /bin/demo", s.Transport.Command)
		}
	}
}

func TestDiscoverPluginMcpServersSubstitutesRoot(t *testing.T) {
	home := t.TempDir()
	root := installFakePlugin(t, home, "local", "myplugin",
		`{"mcpServers":{"demo":{"command":"${CODEX_PLUGIN_ROOT}/bin/demo","args":["--root","${CODEX_PLUGIN_ROOT}"]}}}`)

	servers, warnings := discoverPluginMcpServers(home, map[string]config.PluginConfig{
		"myplugin@local": {Enabled: true},
	})
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	s, ok := servers["demo"]
	if !ok {
		t.Fatalf("demo not discovered; got %v", servers)
	}
	if want := filepath.Join(root, "bin", "demo"); s.Transport.Command != want {
		t.Errorf("command = %q, want %q", s.Transport.Command, want)
	}
	if len(s.Transport.Args) != 2 || s.Transport.Args[1] != root {
		t.Errorf("args = %v, want [--root %q]", s.Transport.Args, root)
	}
}

func TestDiscoverPluginMcpServersSkipsDisabledAndUninstalled(t *testing.T) {
	home := t.TempDir()
	installFakePlugin(t, home, "local", "on",
		`{"mcpServers":{"on_srv":{"command":"${CODEX_PLUGIN_ROOT}/x"}}}`)
	// "off" is installed but disabled; "ghost" is enabled but not installed.
	installFakePlugin(t, home, "local", "off",
		`{"mcpServers":{"off_srv":{"command":"x"}}}`)

	servers, _ := discoverPluginMcpServers(home, map[string]config.PluginConfig{
		"on@local":    {Enabled: true},
		"off@local":   {Enabled: false},
		"ghost@local": {Enabled: true},
	})
	if _, ok := servers["on_srv"]; !ok {
		t.Error("expected on_srv from enabled+installed plugin")
	}
	if _, ok := servers["off_srv"]; ok {
		t.Error("off_srv should be skipped (plugin disabled)")
	}
	if len(servers) != 1 {
		t.Errorf("expected exactly 1 server, got %d: %v", len(servers), servers)
	}
}

func TestEffectiveMcpServersConfigWins(t *testing.T) {
	home := t.TempDir()
	installFakePlugin(t, home, "local", "p",
		`{"mcpServers":{"shared":{"command":"${CODEX_PLUGIN_ROOT}/plugin-cmd"},"plugin_only":{"command":"x"}}}`)

	var configured config.McpServerConfig
	if err := jsonUnmarshalServer(`{"command":"/usr/bin/config-cmd"}`, &configured); err != nil {
		t.Fatalf("config server: %v", err)
	}

	eff := effectiveMcpServers(home,
		map[string]config.McpServerConfig{"shared": configured},
		map[string]config.PluginConfig{"p@local": {Enabled: true}},
	)
	// Configured [mcp_servers] overrides the plugin's "shared" definition.
	if eff["shared"].Transport.Command != "/usr/bin/config-cmd" {
		t.Errorf("shared command = %q, want /usr/bin/config-cmd (config wins)", eff["shared"].Transport.Command)
	}
	// Plugin-only servers still come through.
	if _, ok := eff["plugin_only"]; !ok {
		t.Error("plugin_only should be present from plugin discovery")
	}
}

func jsonUnmarshalServer(s string, dst *config.McpServerConfig) error {
	return dst.UnmarshalJSON([]byte(s))
}
