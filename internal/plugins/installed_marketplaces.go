package plugins

// Installed (configured) marketplace discovery. Ports
// `codex-rs/core-plugins/src/installed_marketplaces.rs`.
//
// The upstream Rust reads the effective user config from a ConfigLayerStack; to
// keep this package free of that dependency, the public entry point accepts the
// already-resolved `marketplaces` table as a typed map. Path helpers match the
// Rust on-disk layout exactly.

import (
	"path/filepath"
	"sort"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/pkg/config"
)

// InstalledMarketplacesDir is the installed-marketplace cache directory relative
// to codex_home, mirroring the Rust INSTALLED_MARKETPLACES_DIR.
const InstalledMarketplacesDir = ".tmp/marketplaces"

// MarketplaceInstallRoot mirrors the Rust `marketplace_install_root`.
func MarketplaceInstallRoot(codexHome string) string {
	return filepath.Join(codexHome, InstalledMarketplacesDir)
}

// InstalledMarketplaceRoots mirrors the Rust
// `installed_marketplace_roots_from_layer_stack`, taking the resolved
// `marketplaces` config table directly.
//
// It returns the absolute roots of every configured marketplace whose name is a
// valid plugin segment and whose root contains a supported marketplace manifest,
// sorted by path. Invalid names are skipped. The input map is not modified.
func InstalledMarketplaceRoots(
	marketplaces map[string]config.MarketplaceConfig,
	codexHome string,
) []abspath.AbsolutePathBuf {
	if len(marketplaces) == 0 {
		return nil
	}
	defaultInstallRoot := MarketplaceInstallRoot(codexHome)

	var roots []abspath.AbsolutePathBuf
	for name, marketplace := range marketplaces {
		if err := ValidatePluginSegment(name, "marketplace name"); err != nil {
			continue
		}
		path, ok := ResolveConfiguredMarketplaceRoot(name, marketplace, defaultInstallRoot)
		if !ok {
			continue
		}
		if _, ok := FindMarketplaceManifestPath(path); !ok {
			continue
		}
		if resolved, err := abspath.FromAbsolutePathChecked(path); err == nil {
			roots = append(roots, resolved)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Path() < roots[j].Path() })
	return roots
}

// ResolveConfiguredMarketplaceRoot mirrors the Rust
// `resolve_configured_marketplace_root`.
//
// For a "local" source type it uses the configured (non-empty) source path; for
// any other source type it uses <default_install_root>/<marketplace_name>. It
// returns ok=false when a local source has no usable path.
func ResolveConfiguredMarketplaceRoot(
	marketplaceName string,
	marketplace config.MarketplaceConfig,
	defaultInstallRoot string,
) (string, bool) {
	if marketplace.SourceType != nil && *marketplace.SourceType == config.MarketplaceSourceLocal {
		if marketplace.Source != nil && *marketplace.Source != "" {
			return *marketplace.Source, true
		}
		return "", false
	}
	return filepath.Join(defaultInstallRoot, marketplaceName), true
}
