package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func writeMarketplace(t *testing.T, root, contents string) abspath.AbsolutePathBuf {
	t.Helper()
	manifestDir := filepath.Join(root, ".agents", "plugins")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifestPath := filepath.Join(manifestDir, "marketplace.json")
	if err := os.WriteFile(manifestPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return mustAbs(t, manifestPath)
}

func TestLoadMarketplaceLocalPlugins(t *testing.T) {
	root := t.TempDir()
	// Create a local plugin with a manifest so version/keywords resolve.
	pluginDir := filepath.Join(root, "plugins", "demo")
	writeSourcePlugin(t, pluginDir, "demo", "1.0.0")

	manifestPath := writeMarketplace(t, root, `{
  "name": "local-mp",
  "interface": { "displayName": "Local Marketplace" },
  "plugins": [
    { "name": "demo", "source": "./plugins/demo" }
  ]
}`)

	marketplace, err := LoadMarketplace(manifestPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if marketplace.Name != "local-mp" {
		t.Fatalf("name got %q", marketplace.Name)
	}
	if marketplace.Interface == nil || *marketplace.Interface.DisplayName != "Local Marketplace" {
		t.Fatalf("interface got %+v", marketplace.Interface)
	}
	if len(marketplace.Plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(marketplace.Plugins))
	}
	p := marketplace.Plugins[0]
	if p.Name != "demo" {
		t.Fatalf("plugin name got %q", p.Name)
	}
	if p.Source.Kind != MarketplaceSourceLocal {
		t.Fatalf("source kind got %v", p.Source.Kind)
	}
	if p.Source.Path.Path() != filepath.Join(root, "plugins", "demo") {
		t.Fatalf("source path got %q", p.Source.Path.Path())
	}
	if p.LocalVersion == nil || *p.LocalVersion != "1.0.0" {
		t.Fatalf("local version got %v", p.LocalVersion)
	}
}

func TestLoadMarketplaceGitSourceNormalization(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeMarketplace(t, root, `{
  "name": "git-mp",
  "plugins": [
    { "name": "shorthand", "source": { "source": "url", "url": "owner/repo" } },
    { "name": "https", "source": { "source": "url", "url": "https://github.com/owner/repo" } },
    { "name": "subdir", "source": { "source": "git-subdir", "url": "owner/repo", "path": "./sub/dir", "ref": " main " } }
  ]
}`)

	marketplace, err := LoadMarketplace(manifestPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	byName := map[string]MarketplacePlugin{}
	for _, p := range marketplace.Plugins {
		byName[p.Name] = p
	}

	if got := byName["shorthand"].Source.URL; got != "https://github.com/owner/repo.git" {
		t.Fatalf("shorthand url got %q", got)
	}
	if got := byName["https"].Source.URL; got != "https://github.com/owner/repo.git" {
		t.Fatalf("https url got %q", got)
	}
	sub := byName["subdir"].Source
	if sub.Kind != MarketplaceSourceGit || sub.SubPath == nil || *sub.SubPath != "sub/dir" {
		t.Fatalf("subdir source got %+v", sub)
	}
	if sub.RefName == nil || *sub.RefName != "main" {
		t.Fatalf("ref got %v", sub.RefName)
	}
}

func TestLoadMarketplaceSkipsInvalidEntries(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeMarketplace(t, root, `{
  "name": "mp",
  "plugins": [
    { "name": "bad name", "source": { "source": "url", "url": "owner/repo" } },
    { "name": "unsupported", "source": { "source": "nope" } },
    { "name": "ok", "source": { "source": "url", "url": "owner/repo" } }
  ]
}`)
	marketplace, err := LoadMarketplace(manifestPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != "ok" {
		t.Fatalf("expected only `ok`, got %+v", marketplace.Plugins)
	}
}

func TestFindInstallableMarketplacePluginPolicy(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeMarketplace(t, root, `{
  "name": "mp",
  "plugins": [
    { "name": "notavail", "source": { "source": "url", "url": "owner/repo" }, "policy": { "installation": "NOT_AVAILABLE" } },
    { "name": "avail", "source": { "source": "url", "url": "owner/repo" } },
    { "name": "codexonly", "source": { "source": "url", "url": "owner/repo" }, "policy": { "products": ["codex"] } }
  ]
}`)

	if _, err := FindInstallableMarketplacePlugin(manifestPath, "notavail", nil); err == nil {
		t.Fatal("expected not-available error")
	}
	if _, err := FindInstallableMarketplacePlugin(manifestPath, "avail", nil); err != nil {
		t.Fatalf("expected avail to be installable: %v", err)
	}
	// Product gating: codex restriction allows, chatgpt does not.
	codex := ProductCodex
	if _, err := FindInstallableMarketplacePlugin(manifestPath, "codexonly", &codex); err != nil {
		t.Fatalf("expected codexonly installable for codex: %v", err)
	}
	chatgpt := ProductChatgpt
	if _, err := FindInstallableMarketplacePlugin(manifestPath, "codexonly", &chatgpt); err == nil {
		t.Fatal("expected codexonly not installable for chatgpt")
	}
}

func TestFindMarketplacePluginNotFound(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeMarketplace(t, root, `{ "name": "mp", "plugins": [] }`)
	_, err := FindMarketplacePlugin(manifestPath, "missing")
	if err == nil {
		t.Fatal("expected not found")
	}
	want := "plugin `missing` was not found in marketplace `mp`"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

func TestValidateMarketplaceRoot(t *testing.T) {
	root := t.TempDir()
	writeMarketplace(t, root, `{ "name": "mp", "plugins": [] }`)
	name, err := ValidateMarketplaceRoot(root)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if name != "mp" {
		t.Fatalf("name got %q", name)
	}
	if _, err := ValidateMarketplaceRoot(t.TempDir()); err == nil {
		t.Fatal("expected error for root without manifest")
	}
}

func TestListMarketplacesWithHome(t *testing.T) {
	home := t.TempDir()
	writeMarketplace(t, home, `{ "name": "home-mp", "plugins": [] }`)
	root := t.TempDir()
	writeMarketplace(t, root, `{ "name": "root-mp", "plugins": [] }`)

	outcome, err := ListMarketplacesWithHome([]abspath.AbsolutePathBuf{mustAbs(t, root)}, &home)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, m := range outcome.Marketplaces {
		names[m.Name] = true
	}
	if !names["home-mp"] || !names["root-mp"] {
		t.Fatalf("expected both marketplaces, got %v", names)
	}
}
