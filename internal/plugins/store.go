package plugins

// Plugin cache store: installs, uninstalls and upgrades plugin bundles on disk,
// and resolves the active version. Ports `codex-rs/core-plugins/src/store.rs`.
//
// On-disk layout under <codex_home>:
//
//	plugins/cache/<marketplace>/<plugin>/<version>/   installed plugin bundle
//	plugins/data/<plugin>-<marketplace>/              per-plugin data root

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/pluginutil"
)

// Cache layout constants mirror the Rust store.
const (
	// DefaultPluginVersion is the version segment used when a manifest omits a
	// version.
	DefaultPluginVersion = "local"
	// PluginsCacheDir is the cache directory relative to codex_home.
	PluginsCacheDir = "plugins/cache"
	// PluginsDataDir is the per-plugin data directory relative to codex_home.
	PluginsDataDir = "plugins/data"
)

// PluginInstallResult mirrors the Rust `PluginInstallResult`.
type PluginInstallResult struct {
	PluginID      PluginID
	PluginVersion string
	InstalledPath abspath.AbsolutePathBuf
}

// PluginStore mirrors the Rust `PluginStore`. It is an immutable value
// describing the cache roots; all operations derive new paths and never mutate
// the receiver.
type PluginStore struct {
	root     abspath.AbsolutePathBuf
	dataRoot abspath.AbsolutePathBuf
}

// NewPluginStore mirrors the Rust `PluginStore::new`: it builds a store rooted at
// codexHome, returning an error when the cache roots cannot be resolved to
// absolute paths (the Rust constructor panics; the Go API surfaces the error).
func NewPluginStore(codexHome string) (PluginStore, error) {
	root, err := abspath.FromAbsolutePathChecked(filepath.Join(codexHome, PluginsCacheDir))
	if err != nil {
		return PluginStore{}, fmt.Errorf("failed to resolve plugin cache root: %w", err)
	}
	dataRoot, err := abspath.FromAbsolutePathChecked(filepath.Join(codexHome, PluginsDataDir))
	if err != nil {
		return PluginStore{}, fmt.Errorf("failed to resolve plugin data root: %w", err)
	}
	return PluginStore{root: root, dataRoot: dataRoot}, nil
}

// Root mirrors the Rust `PluginStore::root`.
func (s PluginStore) Root() abspath.AbsolutePathBuf {
	return s.root
}

// PluginBaseRoot mirrors the Rust `plugin_base_root`:
// <cache>/<marketplace>/<plugin>.
func (s PluginStore) PluginBaseRoot(pluginID PluginID) abspath.AbsolutePathBuf {
	return s.root.Join(pluginID.MarketplaceName).Join(pluginID.PluginName)
}

// PluginRoot mirrors the Rust `plugin_root`:
// <cache>/<marketplace>/<plugin>/<version>.
func (s PluginStore) PluginRoot(pluginID PluginID, pluginVersion string) abspath.AbsolutePathBuf {
	return s.PluginBaseRoot(pluginID).Join(pluginVersion)
}

// PluginDataRoot mirrors the Rust `plugin_data_root`:
// <data>/<plugin>-<marketplace>.
func (s PluginStore) PluginDataRoot(pluginID PluginID) abspath.AbsolutePathBuf {
	return s.dataRoot.Join(fmt.Sprintf("%s-%s", pluginID.PluginName, pluginID.MarketplaceName))
}

// ActivePluginVersion mirrors the Rust `active_plugin_version`.
//
// It scans the plugin base root for valid version directories. When a directory
// named DefaultPluginVersion exists it always wins; otherwise the highest
// version (by semver, falling back to byte order) is chosen. It returns ok=false
// when no version directories exist.
func (s PluginStore) ActivePluginVersion(pluginID PluginID) (string, bool) {
	entries, err := os.ReadDir(s.PluginBaseRoot(pluginID).Path())
	if err != nil {
		return "", false
	}
	var versions []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if validatePluginVersionSegment(name) != nil {
			continue
		}
		versions = append(versions, name)
	}
	if len(versions) == 0 {
		return "", false
	}
	sort.SliceStable(versions, func(i, j int) bool {
		return comparePluginVersions(versions[i], versions[j]) < 0
	})
	for _, v := range versions {
		if v == DefaultPluginVersion {
			return DefaultPluginVersion, true
		}
	}
	return versions[len(versions)-1], true
}

// ActivePluginRoot mirrors the Rust `active_plugin_root`.
func (s PluginStore) ActivePluginRoot(pluginID PluginID) (abspath.AbsolutePathBuf, bool) {
	version, ok := s.ActivePluginVersion(pluginID)
	if !ok {
		return abspath.AbsolutePathBuf{}, false
	}
	return s.PluginRoot(pluginID, version), true
}

// IsInstalled mirrors the Rust `is_installed`.
func (s PluginStore) IsInstalled(pluginID PluginID) bool {
	_, ok := s.ActivePluginVersion(pluginID)
	return ok
}

// Install mirrors the Rust `PluginStore::install`: it derives the version from
// the source manifest, then installs.
func (s PluginStore) Install(sourcePath abspath.AbsolutePathBuf, pluginID PluginID) (PluginInstallResult, error) {
	version, err := PluginVersionForSource(sourcePath.Path())
	if err != nil {
		return PluginInstallResult{}, err
	}
	return s.InstallWithVersion(sourcePath, pluginID, version)
}

// InstallWithVersion mirrors the Rust `install_with_version`.
//
// It validates the source is a directory, that the manifest plugin name matches
// pluginID, validates the version segment, then atomically replaces the version
// directory in the cache. It returns the install result on success.
func (s PluginStore) InstallWithVersion(
	sourcePath abspath.AbsolutePathBuf,
	pluginID PluginID,
	pluginVersion string,
) (PluginInstallResult, error) {
	if !isDir(sourcePath.Path()) {
		return PluginInstallResult{}, &PluginStoreError{Message: fmt.Sprintf(
			"plugin source path is not a directory: %s", sourcePath.String())}
	}

	pluginName, err := pluginNameForSource(sourcePath.Path())
	if err != nil {
		return PluginInstallResult{}, err
	}
	if pluginName != pluginID.PluginName {
		return PluginInstallResult{}, &PluginStoreError{Message: fmt.Sprintf(
			"plugin.json name `%s` does not match marketplace plugin name `%s`",
			pluginName, pluginID.PluginName)}
	}
	if verr := validatePluginVersionSegment(pluginVersion); verr != nil {
		return PluginInstallResult{}, &PluginStoreError{Message: verr.Error()}
	}

	installedPath := s.PluginRoot(pluginID, pluginVersion)
	if err := replacePluginRootAtomically(
		sourcePath.Path(),
		s.PluginBaseRoot(pluginID).Path(),
		pluginVersion,
	); err != nil {
		return PluginInstallResult{}, err
	}

	return PluginInstallResult{
		PluginID:      pluginID,
		PluginVersion: pluginVersion,
		InstalledPath: installedPath,
	}, nil
}

// Uninstall mirrors the Rust `uninstall`: it removes the entire plugin base
// root.
func (s PluginStore) Uninstall(pluginID PluginID) error {
	return removeExistingTarget(s.PluginBaseRoot(pluginID).Path())
}

// PluginStoreError mirrors the Rust `PluginStoreError::Invalid` /
// `PluginStoreError::Io` variants. Message holds the rendered error, matching
// the Rust display output. When wrapping an io error, Err carries it for %w
// unwrapping.
type PluginStoreError struct {
	Message string
	Err     error
}

func (e *PluginStoreError) Error() string {
	return e.Message
}

func (e *PluginStoreError) Unwrap() error {
	return e.Err
}

func storeIOError(context string, err error) *PluginStoreError {
	return &PluginStoreError{Message: fmt.Sprintf("%s: %s", context, err.Error()), Err: err}
}

// PluginVersionForSource mirrors the Rust `plugin_version_for_source`.
func PluginVersionForSource(sourcePath string) (string, error) {
	version, err := pluginManifestVersionForSource(sourcePath)
	if err != nil {
		return "", err
	}
	if version == "" {
		version = DefaultPluginVersion
	}
	if verr := validatePluginVersionSegment(version); verr != nil {
		return "", &PluginStoreError{Message: verr.Error()}
	}
	return version, nil
}

// validatePluginVersionSegment mirrors the Rust `validate_plugin_version_segment`.
func validatePluginVersionSegment(pluginVersion string) error {
	if pluginVersion == "" {
		return fmt.Errorf("invalid plugin version: must not be empty")
	}
	if pluginVersion == "." || pluginVersion == ".." {
		return fmt.Errorf("invalid plugin version: path traversal is not allowed")
	}
	for _, ch := range pluginVersion {
		if !isPluginVersionChar(ch) {
			return fmt.Errorf(
				"invalid plugin version: only ASCII letters, digits, `.`, `+`, `_`, and `-` are allowed")
		}
	}
	return nil
}

func isPluginVersionChar(ch rune) bool {
	switch {
	case ch >= 'a' && ch <= 'z':
		return true
	case ch >= 'A' && ch <= 'Z':
		return true
	case ch >= '0' && ch <= '9':
		return true
	case ch == '-' || ch == '_' || ch == '.' || ch == '+':
		return true
	default:
		return false
	}
}

// rawPluginManifestVersion extracts just the version field, mirroring the Rust
// `RawPluginManifestVersion`. version is a raw value so non-string values can be
// reported precisely.
type rawPluginManifestVersion struct {
	Version json.RawMessage `json:"version"`
}

// pluginManifestVersionForSource mirrors the Rust
// `plugin_manifest_version_for_source`. It returns "" when the manifest omits a
// version, an error when present-but-invalid.
func pluginManifestVersionForSource(sourcePath string) (string, error) {
	manifestPath, ok := pluginutil.FindPluginManifestPath(sourcePath)
	if !ok {
		return "", &PluginStoreError{Message: "missing plugin.json"}
	}
	contents, err := readFileString(manifestPath)
	if err != nil {
		return "", storeIOError("failed to read plugin.json", err)
	}
	var manifest rawPluginManifestVersion
	if err := json.Unmarshal([]byte(contents), &manifest); err != nil {
		return "", &PluginStoreError{Message: fmt.Sprintf("failed to parse plugin.json: %s", err.Error())}
	}
	if len(manifest.Version) == 0 || isJSONNull(manifest.Version) {
		return "", nil
	}
	var version string
	if err := json.Unmarshal(manifest.Version, &version); err != nil {
		return "", &PluginStoreError{Message: "invalid plugin version in plugin.json: expected string"}
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "", &PluginStoreError{Message: "invalid plugin version in plugin.json: must not be blank"}
	}
	return version, nil
}

// pluginNameForSource mirrors the Rust `plugin_name_for_source`.
func pluginNameForSource(sourcePath string) (string, error) {
	manifest, ok := LoadPluginManifest(sourcePath)
	if !ok {
		return "", &PluginStoreError{Message: "missing or invalid plugin.json"}
	}
	if err := ValidatePluginSegment(manifest.Name, "plugin name"); err != nil {
		return "", &PluginStoreError{Message: err.Error()}
	}
	return manifest.Name, nil
}

// comparePluginVersions mirrors the Rust `compare_plugin_versions`: it compares
// by semver when both sides parse, falling back to byte comparison otherwise.
func comparePluginVersions(left, right string) int {
	l, lok := parseSemver(left)
	r, rok := parseSemver(right)
	if lok && rok {
		return compareSemver(l, r)
	}
	return strings.Compare(left, right)
}
