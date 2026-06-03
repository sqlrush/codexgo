package plugins

// Marketplace manifest schema, plugin source resolution and policies. Ports the
// type definitions and source-resolution logic of
// `codex-rs/core-plugins/src/marketplace.rs`.
//
// A marketplace manifest lives at ".agents/plugins/marketplace.json" with a
// ".claude-plugin/marketplace.json" fallback. Local plugin sources resolve
// "./"-relative to the marketplace root; git sources are normalized but not
// fetched here.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// marketplaceManifestRelativePaths mirrors the Rust
// MARKETPLACE_MANIFEST_RELATIVE_PATHS, in priority order.
var marketplaceManifestRelativePaths = []string{
	filepath.Join(".agents", "plugins", "marketplace.json"),
	filepath.Join(".claude-plugin", "marketplace.json"),
}

// MarketplacePluginInstallPolicy mirrors the Rust enum of the same name. JSON is
// the SCREAMING_SNAKE_CASE rename values.
type MarketplacePluginInstallPolicy int

const (
	// InstallPolicyAvailable is the default install policy.
	InstallPolicyAvailable MarketplacePluginInstallPolicy = iota
	// InstallPolicyNotAvailable forbids installation.
	InstallPolicyNotAvailable
	// InstallPolicyInstalledByDefault installs the plugin by default.
	InstallPolicyInstalledByDefault
)

// MarketplacePluginAuthPolicy mirrors the Rust enum of the same name.
type MarketplacePluginAuthPolicy int

const (
	// AuthPolicyOnInstall is the default authentication policy.
	AuthPolicyOnInstall MarketplacePluginAuthPolicy = iota
	// AuthPolicyOnUse defers authentication until first use.
	AuthPolicyOnUse
)

// MarketplacePluginPolicy mirrors the Rust `MarketplacePluginPolicy`.
type MarketplacePluginPolicy struct {
	Installation   MarketplacePluginInstallPolicy
	Authentication MarketplacePluginAuthPolicy
	Products       *[]Product
}

// MarketplacePluginSourceKind selects the active variant of
// [MarketplacePluginSource].
type MarketplacePluginSourceKind int

const (
	// MarketplaceSourceLocal indicates a local path source.
	MarketplaceSourceLocal MarketplacePluginSourceKind = iota
	// MarketplaceSourceGit indicates a git source.
	MarketplaceSourceGit
)

// MarketplacePluginSource mirrors the Rust enum `MarketplacePluginSource`. For
// the Local variant Path is set; for the Git variant URL is set with optional
// SubPath, RefName and SHA.
type MarketplacePluginSource struct {
	Kind    MarketplacePluginSourceKind
	Path    abspath.AbsolutePathBuf
	URL     string
	SubPath *string
	RefName *string
	SHA     *string
}

// MarketplaceInterface mirrors the Rust `MarketplaceInterface`.
type MarketplaceInterface struct {
	DisplayName *string
}

// MarketplacePlugin mirrors the Rust `MarketplacePlugin`.
type MarketplacePlugin struct {
	Name         string
	LocalVersion *string
	Source       MarketplacePluginSource
	Policy       MarketplacePluginPolicy
	Interface    *PluginManifestInterface
	Keywords     []string
}

// Marketplace mirrors the Rust `Marketplace`.
type Marketplace struct {
	Name      string
	Path      abspath.AbsolutePathBuf
	Interface *MarketplaceInterface
	Plugins   []MarketplacePlugin
}

// ResolvedMarketplacePlugin mirrors the Rust `ResolvedMarketplacePlugin`.
type ResolvedMarketplacePlugin struct {
	PluginID  PluginID
	Source    MarketplacePluginSource
	Policy    MarketplacePluginPolicy
	Interface *PluginManifestInterface
	Manifest  *PluginManifest
}

// MarketplaceError mirrors the Rust `MarketplaceError` variants. Kind discerns
// which error occurred for callers that need to branch; Message matches the Rust
// display output.
type MarketplaceError struct {
	Kind    MarketplaceErrorKind
	Message string
	Err     error
}

// MarketplaceErrorKind enumerates marketplace error categories.
type MarketplaceErrorKind int

const (
	// MarketplaceErrorIO is an I/O failure.
	MarketplaceErrorIO MarketplaceErrorKind = iota
	// MarketplaceErrorNotFound indicates the manifest file does not exist.
	MarketplaceErrorNotFound
	// MarketplaceErrorInvalidFile indicates an unparseable/invalid manifest.
	MarketplaceErrorInvalidFile
	// MarketplaceErrorPluginNotFound indicates the plugin is absent.
	MarketplaceErrorPluginNotFound
	// MarketplaceErrorPluginNotAvailable indicates the plugin is not installable.
	MarketplaceErrorPluginNotAvailable
	// MarketplaceErrorPluginsDisabled indicates the plugins feature is off.
	MarketplaceErrorPluginsDisabled
	// MarketplaceErrorInvalidPlugin indicates an invalid plugin entry.
	MarketplaceErrorInvalidPlugin
)

func (e *MarketplaceError) Error() string { return e.Message }
func (e *MarketplaceError) Unwrap() error { return e.Err }

func marketplaceIOError(context string, err error) *MarketplaceError {
	return &MarketplaceError{
		Kind:    MarketplaceErrorIO,
		Message: fmt.Sprintf("%s: %s", context, err.Error()),
		Err:     err,
	}
}

// rawMarketplaceManifest mirrors the serde `RawMarketplaceManifest`.
type rawMarketplaceManifest struct {
	Name      string                           `json:"name"`
	Interface *rawMarketplaceManifestInterface `json:"interface"`
	Plugins   []rawMarketplaceManifestPlugin   `json:"plugins"`
}

type rawMarketplaceManifestInterface struct {
	DisplayName *string `json:"displayName"`
}

type rawMarketplaceManifestPlugin struct {
	Name     string                       `json:"name"`
	Source   json.RawMessage              `json:"source"`
	Policy   rawMarketplaceManifestPolicy `json:"policy"`
	Category *string                      `json:"category"`
}

type rawMarketplaceManifestPolicy struct {
	Installation   *string   `json:"installation"`
	Authentication *string   `json:"authentication"`
	Products       *[]string `json:"products"`
}

// parseInstallPolicy parses the SCREAMING_SNAKE_CASE install policy, defaulting
// to Available when absent or unrecognized (serde default behavior).
func parseInstallPolicy(value *string) MarketplacePluginInstallPolicy {
	if value == nil {
		return InstallPolicyAvailable
	}
	switch *value {
	case "NOT_AVAILABLE":
		return InstallPolicyNotAvailable
	case "AVAILABLE":
		return InstallPolicyAvailable
	case "INSTALLED_BY_DEFAULT":
		return InstallPolicyInstalledByDefault
	default:
		return InstallPolicyAvailable
	}
}

func parseAuthPolicy(value *string) MarketplacePluginAuthPolicy {
	if value == nil {
		return AuthPolicyOnInstall
	}
	switch *value {
	case "ON_INSTALL":
		return AuthPolicyOnInstall
	case "ON_USE":
		return AuthPolicyOnUse
	default:
		return AuthPolicyOnInstall
	}
}

func parsePolicyProducts(values *[]string) *[]Product {
	if values == nil {
		return nil
	}
	products := make([]Product, 0, len(*values))
	for _, value := range *values {
		if product, ok := parseProduct(value); ok {
			products = append(products, product)
		}
	}
	return &products
}

// PluginInterfaceWithMarketplaceCategory mirrors the Rust
// `plugin_interface_with_marketplace_category`: the marketplace category wins
// over the manifest's when both are present; it returns a new interface and does
// not mutate the input.
func PluginInterfaceWithMarketplaceCategory(iface *PluginManifestInterface, category *string) *PluginManifestInterface {
	if category == nil {
		return iface
	}
	var result PluginManifestInterface
	if iface != nil {
		result = *iface
	}
	result.Category = category
	return &result
}

func resolveMarketplaceInterface(raw *rawMarketplaceManifestInterface) *MarketplaceInterface {
	if raw == nil || raw.DisplayName == nil {
		return nil
	}
	return &MarketplaceInterface{DisplayName: raw.DisplayName}
}

// normalizeOptionalGitSelector mirrors the Rust `normalize_optional_git_selector`.
func normalizeOptionalGitSelector(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
