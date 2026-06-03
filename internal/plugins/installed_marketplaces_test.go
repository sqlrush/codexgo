package plugins

import (
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
)

func strPtr(s string) *string { return &s }

func TestMarketplaceInstallRoot(t *testing.T) {
	home := "/home/user/.codex"
	want := filepath.Join(home, ".tmp/marketplaces")
	if got := MarketplaceInstallRoot(home); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveConfiguredMarketplaceRoot(t *testing.T) {
	defaultRoot := "/home/.codex/.tmp/marketplaces"
	localType := config.MarketplaceSourceLocal
	gitType := config.MarketplaceSourceGit

	tests := []struct {
		name        string
		marketplace config.MarketplaceConfig
		wantPath    string
		wantOK      bool
	}{
		{
			name:        "git uses default install root",
			marketplace: config.MarketplaceConfig{SourceType: &gitType},
			wantPath:    filepath.Join(defaultRoot, "mp"),
			wantOK:      true,
		},
		{
			name:        "local with source",
			marketplace: config.MarketplaceConfig{SourceType: &localType, Source: strPtr("/abs/local/mp")},
			wantPath:    "/abs/local/mp",
			wantOK:      true,
		},
		{
			name:        "local without source",
			marketplace: config.MarketplaceConfig{SourceType: &localType},
			wantOK:      false,
		},
		{
			name:        "no source type uses default",
			marketplace: config.MarketplaceConfig{},
			wantPath:    filepath.Join(defaultRoot, "mp"),
			wantOK:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ResolveConfiguredMarketplaceRoot("mp", tt.marketplace, defaultRoot)
			if ok != tt.wantOK {
				t.Fatalf("ok got %v want %v", ok, tt.wantOK)
			}
			if ok && got != tt.wantPath {
				t.Fatalf("path got %q want %q", got, tt.wantPath)
			}
		})
	}
}

func TestInstalledMarketplaceRoots(t *testing.T) {
	home := t.TempDir()
	// Configure two marketplaces: one local with a manifest, one local without.
	localType := config.MarketplaceSourceLocal

	withManifest := t.TempDir()
	writeMarketplace(t, withManifest, `{ "name": "mp", "plugins": [] }`)

	withoutManifest := t.TempDir()

	marketplaces := map[string]config.MarketplaceConfig{
		"good":          {SourceType: &localType, Source: strPtr(withManifest)},
		"missing":       {SourceType: &localType, Source: strPtr(withoutManifest)},
		"invalid name!": {SourceType: &localType, Source: strPtr(withManifest)},
	}

	roots := InstalledMarketplaceRoots(marketplaces, home)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d (%v)", len(roots), roots)
	}
	if roots[0].Path() != mustAbs(t, withManifest).Path() {
		t.Fatalf("root got %q", roots[0].Path())
	}
}
