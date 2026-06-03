package plugins

// Marketplace discovery, loading and lookup. Ports the loading/listing logic of
// `codex-rs/core-plugins/src/marketplace.rs`.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/gitutils"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// MarketplaceListError mirrors the Rust `MarketplaceListError`.
type MarketplaceListError struct {
	Path    abspath.AbsolutePathBuf
	Message string
}

// MarketplaceListOutcome mirrors the Rust `MarketplaceListOutcome`.
type MarketplaceListOutcome struct {
	Marketplaces []Marketplace
	Errors       []MarketplaceListError
}

// HomeDir mirrors the Rust `home_dir`: it prefers an absolute HOME/USERPROFILE,
// falling back to the OS home directory. It returns ok=false when no absolute
// home can be determined.
func HomeDir() (string, bool) {
	for _, key := range []string{"HOME", "USERPROFILE"} {
		value := os.Getenv(key)
		if value != "" && filepath.IsAbs(value) {
			return value, true
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && filepath.IsAbs(home) {
		return home, true
	}
	return "", false
}

// FindMarketplaceManifestPath mirrors the Rust `find_marketplace_manifest_path`:
// the first discoverable marketplace manifest under root that is a regular file.
func FindMarketplaceManifestPath(root string) (abspath.AbsolutePathBuf, bool) {
	for _, relativePath := range marketplaceManifestRelativePaths {
		candidate := filepath.Join(root, relativePath)
		if !isFile(candidate) {
			continue
		}
		if resolved, err := abspath.FromAbsolutePathChecked(candidate); err == nil {
			return resolved, true
		}
	}
	return abspath.AbsolutePathBuf{}, false
}

// marketplaceRootDir mirrors the Rust `marketplace_root_dir`: it strips the
// known manifest layout suffix from a manifest path to recover the marketplace
// root.
func marketplaceRootDir(marketplacePath abspath.AbsolutePathBuf) (abspath.AbsolutePathBuf, error) {
	for _, relativePath := range marketplaceManifestRelativePaths {
		if root, ok := marketplaceRootFromLayout(marketplacePath.Path(), relativePath); ok {
			resolved, err := abspath.FromAbsolutePathChecked(root)
			if err != nil {
				return abspath.AbsolutePathBuf{}, invalidMarketplaceLayoutError(marketplacePath)
			}
			return resolved, nil
		}
	}
	return abspath.AbsolutePathBuf{}, invalidMarketplaceLayoutError(marketplacePath)
}

// marketplaceRootFromLayout walks the relative layout suffix backwards off the
// manifest path, returning the recovered root when the layout matches.
func marketplaceRootFromLayout(marketplacePath, relativePath string) (string, bool) {
	current := marketplacePath
	segments := strings.Split(filepath.ToSlash(relativePath), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		expected := segments[i]
		if expected == "" {
			continue
		}
		if filepath.Base(current) != expected {
			return "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
	return current, true
}

func invalidMarketplaceLayoutError(marketplacePath abspath.AbsolutePathBuf) *MarketplaceError {
	return &MarketplaceError{
		Kind:    MarketplaceErrorInvalidFile,
		Message: fmt.Sprintf("invalid marketplace file `%s`: marketplace file is not in a supported location", marketplacePath.String()),
	}
}

// LoadMarketplace mirrors the Rust `load_marketplace`: it parses the manifest at
// path and resolves every plugin entry, dropping invalid entries.
func LoadMarketplace(path abspath.AbsolutePathBuf) (Marketplace, error) {
	raw, err := loadRawMarketplaceManifest(path)
	if err != nil {
		return Marketplace{}, err
	}

	var plugins []MarketplacePlugin
	for _, rawPlugin := range raw.Plugins {
		resolved, ok, err := resolveMarketplacePluginEntry(path, raw.Name, rawPlugin)
		if err != nil {
			var merr *MarketplaceError
			if errors.As(err, &merr) && merr.Kind == MarketplaceErrorInvalidPlugin {
				// Skip invalid plugin entries, matching the Rust warn+continue.
				continue
			}
			return Marketplace{}, err
		}
		if !ok {
			continue
		}

		var localVersion *string
		var keywords []string
		if resolved.Manifest != nil {
			localVersion = resolved.Manifest.Version
			keywords = resolved.Manifest.Keywords
		}

		plugins = append(plugins, MarketplacePlugin{
			Name:         resolved.PluginID.PluginName,
			LocalVersion: localVersion,
			Source:       resolved.Source,
			Policy:       resolved.Policy,
			Interface:    resolved.Interface,
			Keywords:     keywords,
		})
	}

	return Marketplace{
		Name:      raw.Name,
		Path:      path,
		Interface: resolveMarketplaceInterface(raw.Interface),
		Plugins:   plugins,
	}, nil
}

// FindMarketplacePlugin mirrors the Rust `find_marketplace_plugin`.
func FindMarketplacePlugin(marketplacePath abspath.AbsolutePathBuf, pluginName string) (ResolvedMarketplacePlugin, error) {
	raw, err := loadRawMarketplaceManifest(marketplacePath)
	if err != nil {
		return ResolvedMarketplacePlugin{}, err
	}
	for _, rawPlugin := range raw.Plugins {
		if rawPlugin.Name != pluginName {
			continue
		}
		resolved, ok, err := resolveMarketplacePluginEntry(marketplacePath, raw.Name, rawPlugin)
		if err != nil {
			return ResolvedMarketplacePlugin{}, err
		}
		if ok {
			return resolved, nil
		}
	}
	return ResolvedMarketplacePlugin{}, &MarketplaceError{
		Kind: MarketplaceErrorPluginNotFound,
		Message: fmt.Sprintf("plugin `%s` was not found in marketplace `%s`",
			pluginName, raw.Name),
	}
}

// FindInstallableMarketplacePlugin mirrors the Rust
// `find_installable_marketplace_plugin`: it additionally enforces install policy
// and product gating.
func FindInstallableMarketplacePlugin(
	marketplacePath abspath.AbsolutePathBuf,
	pluginName string,
	restrictionProduct *Product,
) (ResolvedMarketplacePlugin, error) {
	resolved, err := FindMarketplacePlugin(marketplacePath, pluginName)
	if err != nil {
		return ResolvedMarketplacePlugin{}, err
	}

	productAllowed := true
	if resolved.Policy.Products != nil {
		products := *resolved.Policy.Products
		switch {
		case len(products) == 0:
			productAllowed = false
		default:
			productAllowed = restrictionProduct != nil &&
				restrictionProduct.MatchesProductRestriction(products)
		}
	}
	if resolved.Policy.Installation == InstallPolicyNotAvailable || !productAllowed {
		return ResolvedMarketplacePlugin{}, &MarketplaceError{
			Kind: MarketplaceErrorPluginNotAvailable,
			Message: fmt.Sprintf("plugin `%s` is not available for install in marketplace `%s`",
				resolved.PluginID.PluginName, resolved.PluginID.MarketplaceName),
		}
	}
	return resolved, nil
}

// ListMarketplaces mirrors the Rust `list_marketplaces`.
func ListMarketplaces(additionalRoots []abspath.AbsolutePathBuf) (MarketplaceListOutcome, error) {
	home, ok := HomeDir()
	var homePtr *string
	if ok {
		homePtr = &home
	}
	return ListMarketplacesWithHome(additionalRoots, homePtr)
}

// ListMarketplacesWithHome mirrors the Rust `list_marketplaces_with_home`.
func ListMarketplacesWithHome(additionalRoots []abspath.AbsolutePathBuf, homeDir *string) (MarketplaceListOutcome, error) {
	outcome := MarketplaceListOutcome{}
	for _, marketplacePath := range discoverMarketplacePathsFromRoots(additionalRoots, homeDir) {
		marketplace, err := LoadMarketplace(marketplacePath)
		if err != nil {
			outcome.Errors = append(outcome.Errors, MarketplaceListError{
				Path:    marketplacePath,
				Message: err.Error(),
			})
			continue
		}
		outcome.Marketplaces = append(outcome.Marketplaces, marketplace)
	}
	return outcome, nil
}

// ValidateMarketplaceRoot mirrors the Rust `validate_marketplace_root`: it
// returns the marketplace name for a root containing a supported manifest.
func ValidateMarketplaceRoot(root string) (string, error) {
	path, ok := FindMarketplaceManifestPath(root)
	if !ok {
		return "", &MarketplaceError{
			Kind:    MarketplaceErrorInvalidFile,
			Message: fmt.Sprintf("invalid marketplace file `%s`: marketplace root does not contain a supported manifest", root),
		}
	}
	marketplace, err := LoadMarketplace(path)
	if err != nil {
		return "", err
	}
	return marketplace.Name, nil
}

// discoverMarketplacePathsFromRoots mirrors the Rust
// `discover_marketplace_paths_from_roots`.
func discoverMarketplacePathsFromRoots(additionalRoots []abspath.AbsolutePathBuf, homeDir *string) []abspath.AbsolutePathBuf {
	var paths []abspath.AbsolutePathBuf
	contains := func(p abspath.AbsolutePathBuf) bool {
		for _, existing := range paths {
			if existing == p {
				return true
			}
		}
		return false
	}

	if homeDir != nil {
		if path, ok := FindMarketplaceManifestPath(*homeDir); ok {
			paths = append(paths, path)
		}
	}

	for _, root := range additionalRoots {
		if path, ok := FindMarketplaceManifestPath(root.Path()); ok {
			if !contains(path) {
				paths = append(paths, path)
			}
			continue
		}
		if repoRoot, ok := gitutils.GetGitRepoRoot(root.Path()); ok {
			if resolvedRoot, err := abspath.FromAbsolutePathChecked(repoRoot); err == nil {
				if path, ok := FindMarketplaceManifestPath(resolvedRoot.Path()); ok && !contains(path) {
					paths = append(paths, path)
				}
			}
		}
	}

	return paths
}

// loadRawMarketplaceManifest mirrors the Rust `load_raw_marketplace_manifest`.
func loadRawMarketplaceManifest(path abspath.AbsolutePathBuf) (rawMarketplaceManifest, error) {
	contents, err := readFileString(path.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return rawMarketplaceManifest{}, &MarketplaceError{
				Kind:    MarketplaceErrorNotFound,
				Message: fmt.Sprintf("marketplace file `%s` does not exist", path.String()),
			}
		}
		return rawMarketplaceManifest{}, marketplaceIOError("failed to read marketplace file", err)
	}
	var manifest rawMarketplaceManifest
	if err := json.Unmarshal([]byte(contents), &manifest); err != nil {
		return rawMarketplaceManifest{}, &MarketplaceError{
			Kind:    MarketplaceErrorInvalidFile,
			Message: fmt.Sprintf("invalid marketplace file `%s`: %s", path.String(), err.Error()),
		}
	}
	return manifest, nil
}

// resolveMarketplacePluginEntry mirrors the Rust
// `resolve_marketplace_plugin_entry`. It returns ok=false for unsupported
// sources, and an InvalidPlugin error for invalid plugin ids.
func resolveMarketplacePluginEntry(
	marketplacePath abspath.AbsolutePathBuf,
	marketplaceName string,
	plugin rawMarketplaceManifestPlugin,
) (ResolvedMarketplacePlugin, bool, error) {
	source, ok := resolveSupportedPluginSource(marketplacePath, plugin.Name, plugin.Source)
	if !ok {
		return ResolvedMarketplacePlugin{}, false, nil
	}

	var manifest *PluginManifest
	if source.Kind == MarketplaceSourceLocal {
		if loaded, ok := LoadPluginManifest(source.Path.Path()); ok {
			manifest = &loaded
		}
	}

	var manifestInterface *PluginManifestInterface
	if manifest != nil {
		manifestInterface = manifest.Interface
	}
	iface := PluginInterfaceWithMarketplaceCategory(manifestInterface, plugin.Category)

	pluginID, err := NewPluginID(plugin.Name, marketplaceName)
	if err != nil {
		return ResolvedMarketplacePlugin{}, false, &MarketplaceError{
			Kind:    MarketplaceErrorInvalidPlugin,
			Message: err.Error(),
		}
	}

	return ResolvedMarketplacePlugin{
		PluginID: pluginID,
		Source:   source,
		Policy: MarketplacePluginPolicy{
			Installation:   parseInstallPolicy(plugin.Policy.Installation),
			Authentication: parseAuthPolicy(plugin.Policy.Authentication),
			Products:       parsePolicyProducts(plugin.Policy.Products),
		},
		Interface: iface,
		Manifest:  manifest,
	}, true, nil
}
