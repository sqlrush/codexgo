package plugins

// Curated-plugins startup sync orchestration, porting the Rust
// `core-plugins/src/startup_sync.rs`. On startup codex refreshes the local
// snapshot of the openai/plugins curated marketplace repo so `codex plugin
// list`/install can resolve curated plugins offline. The sync tries three
// transports in order and stops at the first success:
//
//  1. git: ls-remote the HEAD sha, and if it differs from the local snapshot,
//     shallow-clone and atomically swap the repo into place.
//  2. GitHub HTTP API: resolve the default-branch HEAD sha and download the
//     zipball when it differs from the recorded sha.
//  3. export backup archive: a lagging public archive used only to BOOTSTRAP a
//     missing snapshot, never to refresh an existing one.
//
// Each transport's failure is isolated: a git failure falls through to HTTP, an
// HTTP failure falls through to the export archive (only when no snapshot
// exists), and the combined error reports every transport's message. The
// recorded sha (`.tmp/plugins.sha`) drives the per-sync update decision.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// curatedPluginsRelativeDir is the curated repo location under codex_home,
	// mirroring Rust's CURATED_PLUGINS_RELATIVE_DIR.
	curatedPluginsRelativeDir = ".tmp/plugins"
	// curatedPluginsSHAFile records the synced HEAD sha under codex_home,
	// mirroring Rust's CURATED_PLUGINS_SHA_FILE.
	curatedPluginsSHAFile = ".tmp/plugins.sha"
	// curatedPluginsMarketplaceRelative is the marketplace manifest path inside
	// the curated repo, mirroring the Rust `.agents/plugins/marketplace.json`.
	curatedPluginsMarketplaceRelative = ".agents/plugins/marketplace.json"
	// curatedPluginsBackupArchiveFallbackVersion is the synthetic sha recorded
	// when the export archive omits a git sha, mirroring
	// CURATED_PLUGINS_BACKUP_ARCHIVE_FALLBACK_VERSION.
	curatedPluginsBackupArchiveFallbackVersion = "export-backup"

	githubAPIBaseURL    = "https://api.github.com"
	openaiPluginsOwner  = "openai"
	openaiPluginsRepo   = "plugins"
	openaiPluginsGitURL = "https://github.com/openai/plugins.git"
	// curatedPluginsBackupArchiveAPIURL is the export archive metadata endpoint,
	// mirroring CURATED_PLUGINS_BACKUP_ARCHIVE_API_URL.
	curatedPluginsBackupArchiveAPIURL = "https://chatgpt.com/backend-api/plugins/export/curated"
)

// CuratedPluginsRepoPath returns the curated repo directory under codexHome,
// mirroring Rust's `curated_plugins_repo_path`.
func CuratedPluginsRepoPath(codexHome string) string {
	return filepath.Join(codexHome, curatedPluginsRelativeDir)
}

// curatedPluginsSHAPath returns the recorded-sha file path under codexHome,
// mirroring Rust's `curated_plugins_sha_path`.
func curatedPluginsSHAPath(codexHome string) string {
	return filepath.Join(codexHome, curatedPluginsSHAFile)
}

// ReadCuratedPluginsSHA reads the trimmed recorded sha, or ("", false) when the
// file is absent, mirroring Rust's `read_curated_plugins_sha`.
func ReadCuratedPluginsSHA(codexHome string) (string, bool) {
	return readSHAFile(curatedPluginsSHAPath(codexHome))
}

// HasLocalCuratedPluginsSnapshot reports whether a usable local snapshot exists
// (both the marketplace manifest and the recorded sha), mirroring Rust's
// `has_local_curated_plugins_snapshot`.
func HasLocalCuratedPluginsSnapshot(codexHome string) bool {
	manifest := filepath.Join(CuratedPluginsRepoPath(codexHome), curatedPluginsMarketplaceRelative)
	if !isFile(manifest) {
		return false
	}
	return isFile(curatedPluginsSHAPath(codexHome))
}

// CuratedSyncTransport abstracts the three curated-plugin transports so tests can
// inject fakes in place of git/network. SyncOpenAIPluginsRepo uses the real
// implementations; SyncOpenAIPluginsRepoWithTransport accepts an override.
type CuratedSyncTransport interface {
	// SyncViaGit refreshes the snapshot via git, returning the synced HEAD sha.
	SyncViaGit(codexHome string) (string, error)
	// SyncViaHTTP refreshes the snapshot via the GitHub HTTP API.
	SyncViaHTTP(codexHome string) (string, error)
	// SyncViaBackupArchive bootstraps the snapshot from the export archive.
	SyncViaBackupArchive(codexHome string) (string, error)
}

// SyncOpenAIPluginsRepo refreshes the curated snapshot using the real
// git/HTTP/export-archive transports, mirroring Rust's
// `sync_openai_plugins_repo`.
func SyncOpenAIPluginsRepo(codexHome string) (string, error) {
	return SyncOpenAIPluginsRepoWithTransport(codexHome, defaultCuratedSyncTransport{
		gitBinary:           "git",
		apiBaseURL:          githubAPIBaseURL,
		backupArchiveAPIURL: curatedPluginsBackupArchiveAPIURL,
	})
}

// SyncOpenAIPluginsRepoWithTransport runs the git → HTTP → export-archive
// fallback chain over the given transport, mirroring Rust's
// `sync_openai_plugins_repo_with_transport_overrides`. The export archive is
// only attempted when no local snapshot exists; otherwise an HTTP failure is
// terminal. The returned error aggregates every attempted transport's message.
func SyncOpenAIPluginsRepoWithTransport(codexHome string, transport CuratedSyncTransport) (string, error) {
	remoteSHA, gitErr := transport.SyncViaGit(codexHome)
	if gitErr == nil {
		return remoteSHA, nil
	}

	httpSHA, httpErr := transport.SyncViaHTTP(codexHome)
	if httpErr == nil {
		return httpSHA, nil
	}

	// The export archive is a lagging backup path. Only use it to bootstrap a
	// missing local snapshot, never to refresh an existing one.
	if HasLocalCuratedPluginsSnapshot(codexHome) {
		return "", fmt.Errorf(
			"git sync failed for curated plugin sync: %v; GitHub HTTP sync failed for curated plugin sync: %v; export archive fallback skipped because a local curated plugins snapshot already exists",
			gitErr, httpErr,
		)
	}

	exportSHA, exportErr := transport.SyncViaBackupArchive(codexHome)
	if exportErr != nil {
		return "", fmt.Errorf(
			"git sync failed for curated plugin sync: %v; GitHub HTTP sync failed for curated plugin sync: %v; export archive sync failed for curated plugin sync: %v",
			gitErr, httpErr, exportErr,
		)
	}
	return exportSHA, nil
}

// activateCuratedRepo atomically swaps stagedRepoDir into repoPath, mirroring
// Rust's `activate_curated_repo`. An existing repo is moved aside first; on
// failure the previous repo is restored. The caller owns stagedRepoDir's cleanup
// when this returns an error before the rename.
func activateCuratedRepo(repoPath, stagedRepoDir string) error {
	if _, err := os.Stat(repoPath); err == nil {
		parent := filepath.Dir(repoPath)
		backupDir, err := os.MkdirTemp(parent, "plugins-backup-")
		if err != nil {
			return fmt.Errorf("failed to create curated plugins backup directory in %s: %w", parent, err)
		}
		backupRepoPath := filepath.Join(backupDir, "repo")
		if err := os.Rename(repoPath, backupRepoPath); err != nil {
			_ = os.RemoveAll(backupDir)
			return fmt.Errorf("failed to move previous curated plugins repo out of the way at %s: %w", repoPath, err)
		}
		if err := os.Rename(stagedRepoDir, repoPath); err != nil {
			if rollbackErr := os.Rename(backupRepoPath, repoPath); rollbackErr != nil {
				return fmt.Errorf(
					"failed to activate new curated plugins repo at %s: %w; failed to restore previous repo (left at %s): %v",
					repoPath, err, backupRepoPath, rollbackErr,
				)
			}
			_ = os.RemoveAll(backupDir)
			return fmt.Errorf("failed to activate new curated plugins repo at %s: %w", repoPath, err)
		}
		_ = os.RemoveAll(backupDir)
		return nil
	}

	if err := os.Rename(stagedRepoDir, repoPath); err != nil {
		return fmt.Errorf("failed to activate curated plugins repo at %s: %w", repoPath, err)
	}
	return nil
}

// writeCuratedPluginsSHA records remoteSHA (with a trailing newline) at shaPath,
// creating the parent directory, mirroring Rust's `write_curated_plugins_sha`.
func writeCuratedPluginsSHA(shaPath, remoteSHA string) error {
	if parent := filepath.Dir(shaPath); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("failed to create curated plugins sha directory %s: %w", parent, err)
		}
	}
	if err := os.WriteFile(shaPath, []byte(remoteSHA+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write curated plugins sha file %s: %w", shaPath, err)
	}
	return nil
}

// ensureMarketplaceManifestExists verifies the curated repo carries the
// marketplace manifest, mirroring Rust's `ensure_marketplace_manifest_exists`.
func ensureMarketplaceManifestExists(repoPath string) error {
	manifest := filepath.Join(repoPath, curatedPluginsMarketplaceRelative)
	if isFile(manifest) {
		return nil
	}
	return fmt.Errorf("curated plugins archive missing marketplace manifest at %s", manifest)
}

// prepareCuratedRepoParentAndTempDir creates the curated repo's parent directory
// and a staging temp dir alongside it, mirroring Rust's
// `prepare_curated_repo_parent_and_temp_dir`. The caller owns the returned dir's
// cleanup until activation renames it into place.
func prepareCuratedRepoParentAndTempDir(repoPath string) (string, error) {
	parent := filepath.Dir(repoPath)
	if parent == "" || parent == repoPath {
		return "", fmt.Errorf("failed to determine curated plugins parent directory for %s", repoPath)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("failed to create curated plugins parent directory %s: %w", parent, err)
	}
	stagedDir, err := os.MkdirTemp(parent, "plugins-clone-")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary curated plugins directory in %s: %w", parent, err)
	}
	return stagedDir, nil
}

// readSHAFile reads and trims a recorded sha file, returning ("", false) when it
// is absent or empty, mirroring Rust's `read_sha_file`.
func readSHAFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	sha := strings.TrimSpace(string(data))
	if sha == "" {
		return "", false
	}
	return sha, true
}
