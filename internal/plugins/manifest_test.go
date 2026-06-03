package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, pluginRoot string, version *string, iface string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(pluginRoot, ".codex-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	versionLine := ""
	if version != nil {
		versionLine = "  \"version\": \"" + *version + "\",\n"
	}
	contents := "{\n  \"name\": \"demo-plugin\",\n" + versionLine + "  \"interface\": " + iface + "\n}"
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeRawManifest(t *testing.T, pluginRoot, relativePath, contents string) {
	t.Helper()
	full := filepath.Join(pluginRoot, relativePath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestPluginInterfaceAcceptsLegacyDefaultPromptString(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	writeManifest(t, pluginRoot, nil, `{
    "displayName": "Demo Plugin",
    "defaultPrompt": "  Summarize   my inbox  "
  }`)

	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if manifest.Interface == nil || manifest.Interface.DefaultPrompt == nil {
		t.Fatal("expected default prompt")
	}
	if got := *manifest.Interface.DefaultPrompt; !reflect.DeepEqual(got, []string{"Summarize my inbox"}) {
		t.Fatalf("got %v", got)
	}
}

func TestPluginInterfaceNormalizesDefaultPromptArray(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	tooLong := strings.Repeat("x", maxDefaultPromptLen+1)
	iface := `{
    "displayName": "Demo Plugin",
    "defaultPrompt": [
      " Summarize my inbox ",
      123,
      "` + tooLong + `",
      "   ",
      "Draft the reply  ",
      "Find   my next action",
      "Archive old mail"
    ]
  }`
	writeManifest(t, pluginRoot, nil, iface)

	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	want := []string{"Summarize my inbox", "Draft the reply", "Find my next action"}
	if got := *manifest.Interface.DefaultPrompt; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPluginInterfaceIgnoresInvalidDefaultPromptShape(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	writeManifest(t, pluginRoot, nil, `{
    "displayName": "Demo Plugin",
    "defaultPrompt": { "text": "Summarize my inbox" }
  }`)

	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if manifest.Interface.DefaultPrompt != nil {
		t.Fatalf("expected nil default prompt, got %v", *manifest.Interface.DefaultPrompt)
	}
}

func TestPluginManifestReadsTrimmedVersion(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	v := " 1.2.3-beta+7 "
	writeManifest(t, pluginRoot, &v, `{ "displayName": "Demo Plugin" }`)

	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if manifest.Version == nil || *manifest.Version != "1.2.3-beta+7" {
		t.Fatalf("got %v", manifest.Version)
	}
}

func TestPluginManifestReadsKeywords(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	writeRawManifest(t, pluginRoot, ".codex-plugin/plugin.json", `{
  "name": "demo-plugin",
  "keywords": ["api-key", "developer tools"]
}`)

	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if !reflect.DeepEqual(manifest.Keywords, []string{"api-key", "developer tools"}) {
		t.Fatalf("got %v", manifest.Keywords)
	}
}

func TestPluginManifestUsesAlternateDiscoverablePath(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	writeRawManifest(t, pluginRoot, ".claude-plugin/plugin.json", `{
  "name": "demo-plugin",
  "version": " 2.0.0 ",
  "interface": { "displayName": "Fallback Plugin" }
}`)

	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if manifest.Version == nil || *manifest.Version != "2.0.0" {
		t.Fatalf("version got %v", manifest.Version)
	}
	if manifest.Interface == nil || manifest.Interface.DisplayName == nil || *manifest.Interface.DisplayName != "Fallback Plugin" {
		t.Fatalf("display name got %+v", manifest.Interface)
	}
}

func TestPluginManifestNameFallsBackToDir(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "my-plugin")
	writeRawManifest(t, pluginRoot, ".codex-plugin/plugin.json", `{ "name": "   " }`)

	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if manifest.Name != "my-plugin" {
		t.Fatalf("got %q", manifest.Name)
	}
}

func TestPluginManifestResolvesPaths(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	writeRawManifest(t, pluginRoot, ".codex-plugin/plugin.json", `{
  "name": "demo-plugin",
  "skills": "./skills",
  "mcpServers": "./.mcp.json",
  "apps": "./bad/../escape",
  "hooks": "./hooks/hooks.json"
}`)

	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if manifest.Paths.Skills == nil || manifest.Paths.Skills.Path() != filepath.Join(pluginRoot, "skills") {
		t.Fatalf("skills got %v", manifest.Paths.Skills)
	}
	if manifest.Paths.McpServers == nil || manifest.Paths.McpServers.Path() != filepath.Join(pluginRoot, ".mcp.json") {
		t.Fatalf("mcp got %v", manifest.Paths.McpServers)
	}
	if manifest.Paths.Apps != nil {
		t.Fatalf("expected apps to be dropped due to '..', got %v", manifest.Paths.Apps)
	}
	if manifest.Paths.Hooks == nil || manifest.Paths.Hooks.Kind != PluginManifestHooksPaths {
		t.Fatalf("hooks got %+v", manifest.Paths.Hooks)
	}
}

func TestPluginManifestRejectsNonDotSlashPath(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	writeRawManifest(t, pluginRoot, ".codex-plugin/plugin.json", `{
  "name": "demo-plugin",
  "skills": "skills"
}`)
	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if manifest.Paths.Skills != nil {
		t.Fatalf("expected nil skills path, got %v", manifest.Paths.Skills)
	}
}

func TestPluginManifestInlineHooks(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	writeRawManifest(t, pluginRoot, ".codex-plugin/plugin.json", `{
  "name": "demo-plugin",
  "hooks": { "hooks": { "PreToolUse": [] } }
}`)
	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	if manifest.Paths.Hooks == nil || manifest.Paths.Hooks.Kind != PluginManifestHooksInline {
		t.Fatalf("expected inline hooks, got %+v", manifest.Paths.Hooks)
	}
	if len(manifest.Paths.Hooks.Inline) != 1 {
		t.Fatalf("expected one inline hooks file, got %d", len(manifest.Paths.Hooks.Inline))
	}
}

func TestLoadPluginManifestMissing(t *testing.T) {
	if _, ok := LoadPluginManifest(t.TempDir()); ok {
		t.Fatal("expected no manifest")
	}
}

func TestPluginManifestInterfaceURLAliases(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "demo-plugin")
	writeManifest(t, pluginRoot, nil, `{
    "websiteURL": "https://example.com",
    "privacyPolicyURL": "https://example.com/privacy",
    "termsOfServiceURL": "https://example.com/tos"
  }`)
	manifest, ok := LoadPluginManifest(pluginRoot)
	if !ok {
		t.Fatal("expected manifest")
	}
	iface := manifest.Interface
	if iface == nil || iface.WebsiteURL == nil || *iface.WebsiteURL != "https://example.com" {
		t.Fatalf("website alias not applied: %+v", iface)
	}
	if iface.PrivacyPolicyURL == nil || *iface.PrivacyPolicyURL != "https://example.com/privacy" {
		t.Fatalf("privacy alias not applied: %+v", iface)
	}
}
