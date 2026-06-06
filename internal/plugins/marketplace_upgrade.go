package plugins

// Configured-marketplace upgrade orchestration, porting the Rust
// `core-plugins/src/marketplace_upgrade.rs` (+ activation.rs/git.rs). It
// refreshes the local clones of git-sourced marketplaces declared in the user
// config's `[marketplaces]` table:
//
//   - entry point: UpgradeConfiguredGitMarketplaces selects the configured git
//     marketplaces (optionally filtered to one by name) and upgrades each.
//   - per-marketplace update decision: the remote revision is resolved and, when
//     the local clone already carries a matching manifest, recorded revision,
//     and install metadata, the marketplace is skipped (no re-clone).
//   - failure isolation: each marketplace is upgraded independently; one
//     marketplace's failure is collected into the outcome's errors and does NOT
//     abort the rest.
//   - atomic activation: a fresh clone is staged in a temp dir, validated, and
//     swapped into place with the previous root restored on failure.
//
// Git access is injected through [MarketplaceGitClient] so tests can drive the
// orchestration with temp-git fixtures or a fake; UpgradeConfiguredGitMarketplaces
// uses the real git binary.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sqlrush/codexgo/internal/brand"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

const (
	marketplaceUpgradeGitTimeout    = 30 * time.Second
	marketplaceInstallMetadataFile  = brand.MarketplaceInstallMetadataFile
	marketplaceUpgradeStagingSubdir = ".staging"
)

// ConfiguredMarketplaceUpgradeError records one marketplace's upgrade failure,
// mirroring Rust's `ConfiguredMarketplaceUpgradeError`.
type ConfiguredMarketplaceUpgradeError struct {
	MarketplaceName string
	Message         string
}

// ConfiguredMarketplaceUpgradeOutcome is the aggregate result of an upgrade run,
// mirroring Rust's `ConfiguredMarketplaceUpgradeOutcome`.
type ConfiguredMarketplaceUpgradeOutcome struct {
	SelectedMarketplaces []string
	UpgradedRoots        []abspath.AbsolutePathBuf
	Errors               []ConfiguredMarketplaceUpgradeError
}

// AllSucceeded reports whether the run completed without per-marketplace errors,
// mirroring Rust's `ConfiguredMarketplaceUpgradeOutcome::all_succeeded`.
func (o ConfiguredMarketplaceUpgradeOutcome) AllSucceeded() bool {
	return len(o.Errors) == 0
}

// configuredGitMarketplace is the resolved view of one git-sourced marketplace
// entry, mirroring Rust's `ConfiguredGitMarketplace`.
type configuredGitMarketplace struct {
	name         string
	source       string
	refName      *string
	sparsePaths  []string
	lastRevision *string
}

// MarketplaceGitClient abstracts the git operations the upgrade needs, so tests
// can avoid real network/git. It mirrors the Rust `git_remote_revision` and
// `clone_git_source` free functions.
type MarketplaceGitClient interface {
	// RemoteRevision resolves the revision the source+ref currently points to.
	RemoteRevision(source string, refName *string, timeout time.Duration) (string, error)
	// CloneSource clones source (optionally at refName, with sparse paths) into
	// destination and returns the checked-out revision.
	CloneSource(source string, refName *string, sparsePaths []string, destination string, timeout time.Duration) (string, error)
}

// installedMarketplaceMetadata is the activation metadata recorded alongside a
// cloned marketplace, mirroring Rust's `InstalledMarketplaceMetadata`. The JSON
// shape is snake_case to match serde.
type installedMarketplaceMetadata struct {
	SourceType  config.MarketplaceSourceType `json:"source_type"`
	Source      string                       `json:"source"`
	RefName     *string                      `json:"ref_name"`
	SparsePaths []string                     `json:"sparse_paths"`
	Revision    string                       `json:"revision"`
}

// UpgradeConfiguredGitMarketplaces upgrades configured git marketplaces using the
// real git binary, mirroring Rust's `upgrade_configured_git_marketplaces`. When
// marketplaceName is non-empty only that marketplace is upgraded.
func UpgradeConfiguredGitMarketplaces(codexHome string, marketplaces map[string]config.MarketplaceConfig, marketplaceName string) ConfiguredMarketplaceUpgradeOutcome {
	return UpgradeConfiguredGitMarketplacesWithClient(codexHome, marketplaces, marketplaceName, realMarketplaceGitClient{})
}

// UpgradeConfiguredGitMarketplacesWithClient runs the upgrade over the given git
// client, isolating per-marketplace failures. The selected marketplaces are
// sorted by name for deterministic output, mirroring the Rust ordering.
func UpgradeConfiguredGitMarketplacesWithClient(codexHome string, marketplaces map[string]config.MarketplaceConfig, marketplaceName string, client MarketplaceGitClient) ConfiguredMarketplaceUpgradeOutcome {
	selected := configuredGitMarketplaces(marketplaces)
	if marketplaceName != "" {
		filtered := selected[:0:0]
		for _, m := range selected {
			if m.name == marketplaceName {
				filtered = append(filtered, m)
			}
		}
		selected = filtered
	}
	if len(selected) == 0 {
		return ConfiguredMarketplaceUpgradeOutcome{}
	}

	installRoot := MarketplaceInstallRoot(codexHome)
	outcome := ConfiguredMarketplaceUpgradeOutcome{}
	for _, m := range selected {
		outcome.SelectedMarketplaces = append(outcome.SelectedMarketplaces, m.name)
	}
	for _, m := range selected {
		root, err := upgradeConfiguredGitMarketplace(installRoot, m, client)
		if err != nil {
			outcome.Errors = append(outcome.Errors, ConfiguredMarketplaceUpgradeError{
				MarketplaceName: m.name,
				Message:         err.Error(),
			})
			continue
		}
		if root != nil {
			outcome.UpgradedRoots = append(outcome.UpgradedRoots, *root)
		}
	}
	return outcome
}

// configuredGitMarketplaces projects the config map into the git-sourced subset,
// sorted by name, mirroring Rust's `configured_git_marketplaces`.
func configuredGitMarketplaces(marketplaces map[string]config.MarketplaceConfig) []configuredGitMarketplace {
	var out []configuredGitMarketplace
	for name, mc := range marketplaces {
		if mc.SourceType == nil || *mc.SourceType != config.MarketplaceSourceGit {
			continue
		}
		if mc.Source == nil || *mc.Source == "" {
			// Ignore git marketplaces without a source, like the Rust warn+skip.
			continue
		}
		var sparse []string
		if mc.SparsePaths != nil {
			sparse = append(sparse, *mc.SparsePaths...)
		}
		out = append(out, configuredGitMarketplace{
			name:         name,
			source:       *mc.Source,
			refName:      mc.RefName,
			sparsePaths:  sparse,
			lastRevision: mc.LastRevision,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// upgradeConfiguredGitMarketplace upgrades one marketplace, returning the
// activated root (or nil when already up to date), mirroring Rust's
// `upgrade_configured_git_marketplace`.
func upgradeConfiguredGitMarketplace(installRoot string, marketplace configuredGitMarketplace, client MarketplaceGitClient) (*abspath.AbsolutePathBuf, error) {
	if err := ValidatePluginSegment(marketplace.name, "marketplace name"); err != nil {
		return nil, err
	}
	remoteRevision, err := client.RemoteRevision(marketplace.source, marketplace.refName, marketplaceUpgradeGitTimeout)
	if err != nil {
		return nil, err
	}

	destination := filepath.Join(installRoot, marketplace.name)
	if _, ok := FindMarketplaceManifestPath(destination); ok &&
		marketplace.lastRevision != nil && *marketplace.lastRevision == remoteRevision &&
		installedMarketplaceMetadataMatches(destination, marketplace, remoteRevision) {
		// Already up to date: skip the re-clone (the update decision).
		return nil, nil
	}

	stagingParent := filepath.Join(installRoot, marketplaceUpgradeStagingSubdir)
	if err := os.MkdirAll(stagingParent, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create marketplace upgrade staging directory %s: %w", stagingParent, err)
	}
	stagedDir, err := os.MkdirTemp(stagingParent, "marketplace-upgrade-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary marketplace upgrade directory in %s: %w", stagingParent, err)
	}
	defer os.RemoveAll(stagedDir)

	activatedRevision, err := client.CloneSource(marketplace.source, marketplace.refName, marketplace.sparsePaths, stagedDir, marketplaceUpgradeGitTimeout)
	if err != nil {
		return nil, err
	}
	marketplaceName, err := ValidateMarketplaceRoot(stagedDir)
	if err != nil {
		return nil, fmt.Errorf("failed to validate upgraded marketplace root: %w", err)
	}
	if marketplaceName != marketplace.name {
		return nil, fmt.Errorf("upgraded marketplace name `%s` does not match configured marketplace `%s`", marketplaceName, marketplace.name)
	}
	if err := writeInstalledMarketplaceMetadata(stagedDir, marketplace, activatedRevision); err != nil {
		return nil, err
	}

	if err := activateMarketplaceRoot(destination, stagedDir); err != nil {
		return nil, err
	}

	root, err := abspath.FromAbsolutePathChecked(destination)
	if err != nil {
		return nil, fmt.Errorf("upgraded marketplace path is not absolute: %w", err)
	}
	return &root, nil
}

// installedMarketplaceMetadataPath returns the metadata file path under root.
func installedMarketplaceMetadataPath(root string) string {
	return filepath.Join(root, marketplaceInstallMetadataFile)
}

// installedMarketplaceMetadataFor builds the metadata for marketplace at revision.
func installedMarketplaceMetadataFor(marketplace configuredGitMarketplace, revision string) installedMarketplaceMetadata {
	sparse := marketplace.sparsePaths
	if sparse == nil {
		sparse = []string{}
	}
	return installedMarketplaceMetadata{
		SourceType:  config.MarketplaceSourceGit,
		Source:      marketplace.source,
		RefName:     marketplace.refName,
		SparsePaths: sparse,
		Revision:    revision,
	}
}

// installedMarketplaceMetadataMatches reports whether the activated metadata at
// root equals the expected metadata, mirroring Rust's
// `installed_marketplace_metadata_matches`.
func installedMarketplaceMetadataMatches(root string, marketplace configuredGitMarketplace, revision string) bool {
	data, err := os.ReadFile(installedMarketplaceMetadataPath(root))
	if err != nil {
		return false
	}
	var actual installedMarketplaceMetadata
	if err := json.Unmarshal(data, &actual); err != nil {
		return false
	}
	expected := installedMarketplaceMetadataFor(marketplace, revision)
	return metadataEqual(actual, expected)
}

// metadataEqual compares two metadata records by value, treating nil and empty
// sparse-path slices as equal (matching serde's Vec round-trip).
func metadataEqual(a, b installedMarketplaceMetadata) bool {
	if a.SourceType != b.SourceType || a.Source != b.Source || a.Revision != b.Revision {
		return false
	}
	if (a.RefName == nil) != (b.RefName == nil) {
		return false
	}
	if a.RefName != nil && *a.RefName != *b.RefName {
		return false
	}
	if len(a.SparsePaths) != len(b.SparsePaths) {
		return false
	}
	for i := range a.SparsePaths {
		if a.SparsePaths[i] != b.SparsePaths[i] {
			return false
		}
	}
	return true
}

// writeInstalledMarketplaceMetadata records the activation metadata at root,
// mirroring Rust's `write_installed_marketplace_metadata`.
func writeInstalledMarketplaceMetadata(root string, marketplace configuredGitMarketplace, revision string) error {
	metadata := installedMarketplaceMetadataFor(marketplace, revision)
	contents, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize activated marketplace metadata: %w", err)
	}
	if err := os.WriteFile(installedMarketplaceMetadataPath(root), contents, 0o600); err != nil {
		return fmt.Errorf("failed to write activated marketplace metadata: %w", err)
	}
	return nil
}

// activateMarketplaceRoot atomically swaps stagedDir into destination, mirroring
// Rust's `activate_marketplace_root`: the previous root is moved aside and
// restored on failure.
func activateMarketplaceRoot(destination, stagedDir string) error {
	parent := filepath.Dir(destination)
	if parent == "" || parent == destination {
		return fmt.Errorf("failed to determine marketplace install parent for %s", destination)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("failed to create marketplace install parent %s: %w", parent, err)
	}

	if _, err := os.Stat(destination); err == nil {
		backupDir, err := os.MkdirTemp(parent, "marketplace-backup-")
		if err != nil {
			return fmt.Errorf("failed to create marketplace backup directory in %s: %w", parent, err)
		}
		backupRoot := filepath.Join(backupDir, "root")
		if err := os.Rename(destination, backupRoot); err != nil {
			_ = os.RemoveAll(backupDir)
			return fmt.Errorf("failed to move previous marketplace root out of the way at %s: %w", destination, err)
		}
		if err := os.Rename(stagedDir, destination); err != nil {
			if rollbackErr := os.Rename(backupRoot, destination); rollbackErr != nil {
				return fmt.Errorf(
					"failed to activate upgraded marketplace at %s: %w; failed to restore previous root (left at %s): %v",
					destination, err, backupRoot, rollbackErr,
				)
			}
			_ = os.RemoveAll(backupDir)
			return fmt.Errorf("failed to activate upgraded marketplace at %s: %w", destination, err)
		}
		_ = os.RemoveAll(backupDir)
		return nil
	}

	if err := os.Rename(stagedDir, destination); err != nil {
		return fmt.Errorf("failed to activate upgraded marketplace at %s: %w", destination, err)
	}
	return nil
}
