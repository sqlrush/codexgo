package plugins

// Atomic filesystem operations backing the plugin store install/uninstall.
// Ports the helpers from `codex-rs/core-plugins/src/store.rs`.

import (
	"fmt"
	"os"
	"path/filepath"
)

// removeExistingTarget mirrors the Rust `remove_existing_target`: removes a file
// or directory if it exists, succeeding when it does not.
func removeExistingTarget(path string) error {
	if !pathExists(path) {
		return nil
	}
	if isDir(path) {
		if err := os.RemoveAll(path); err != nil {
			return storeIOError("failed to remove existing plugin cache entry", err)
		}
		return nil
	}
	if err := os.Remove(path); err != nil {
		return storeIOError("failed to remove existing plugin cache entry", err)
	}
	return nil
}

// replacePluginRootAtomically mirrors the Rust `replace_plugin_root_atomically`.
//
// It stages a copy of source into a temp dir, then activates it. When the target
// root already exists but the specific version is missing it renames just the
// version directory in and prunes superseded versions; otherwise it backs up the
// existing root and swaps the staged root in, rolling back on failure.
func replacePluginRootAtomically(source, targetRoot, pluginVersion string) error {
	parent := filepath.Dir(targetRoot)
	if parent == targetRoot {
		return &PluginStoreError{Message: fmt.Sprintf("plugin cache path has no parent: %s", targetRoot)}
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return storeIOError("failed to create plugin cache directory", err)
	}

	pluginDirName := filepath.Base(targetRoot)
	if pluginDirName == "" || pluginDirName == string(filepath.Separator) {
		return &PluginStoreError{Message: fmt.Sprintf("plugin cache path has no directory name: %s", targetRoot)}
	}

	stagedDir, err := os.MkdirTemp(parent, "plugin-install-")
	if err != nil {
		return storeIOError("failed to create temporary plugin cache directory", err)
	}
	defer os.RemoveAll(stagedDir)

	stagedRoot := filepath.Join(stagedDir, pluginDirName)
	stagedVersionRoot := filepath.Join(stagedRoot, pluginVersion)
	if err := copyDirRecursive(source, stagedVersionRoot); err != nil {
		return err
	}

	targetVersionRoot := filepath.Join(targetRoot, pluginVersion)
	if pathExists(targetRoot) && !pathExists(targetVersionRoot) {
		if err := os.Rename(stagedVersionRoot, targetVersionRoot); err != nil {
			return storeIOError("failed to activate updated plugin cache version", err)
		}
		return removeOldPluginVersions(targetRoot, pluginVersion)
	}

	if pathExists(targetRoot) {
		backupDir, err := os.MkdirTemp(parent, "plugin-backup-")
		if err != nil {
			return storeIOError("failed to create plugin cache backup directory", err)
		}
		backupRoot := filepath.Join(backupDir, pluginDirName)
		if err := os.Rename(targetRoot, backupRoot); err != nil {
			os.RemoveAll(backupDir)
			return storeIOError("failed to back up plugin cache entry", err)
		}

		if err := os.Rename(stagedRoot, targetRoot); err != nil {
			if rollbackErr := os.Rename(backupRoot, targetRoot); rollbackErr != nil {
				// Keep the backup so it can be recovered manually; do not delete.
				return &PluginStoreError{Message: fmt.Sprintf(
					"failed to activate updated plugin cache entry at %s: %s; failed to restore previous cache entry (left at %s): %s",
					targetRoot, err.Error(), backupRoot, rollbackErr.Error())}
			}
			os.RemoveAll(backupDir)
			return storeIOError("failed to activate updated plugin cache entry", err)
		}
		os.RemoveAll(backupDir)
		return nil
	}

	if err := os.Rename(stagedRoot, targetRoot); err != nil {
		return storeIOError("failed to activate plugin cache entry", err)
	}
	return nil
}

// removeOldPluginVersions mirrors the Rust `remove_old_plugin_versions`: prunes
// version directories other than pluginVersion. A removal failure is an error
// only when the stale version would otherwise remain active.
func removeOldPluginVersions(targetRoot, pluginVersion string) error {
	entries, err := os.ReadDir(targetRoot)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		version := entry.Name()
		if version == pluginVersion || validatePluginVersionSegment(version) != nil {
			continue
		}
		if rmErr := os.RemoveAll(filepath.Join(targetRoot, version)); rmErr != nil {
			if oldPluginVersionWouldStayActive(version, pluginVersion) {
				return &PluginStoreError{Message: fmt.Sprintf(
					"failed to activate updated plugin cache version `%s` while `%s` remains active",
					pluginVersion, version)}
			}
		}
	}
	return nil
}

// oldPluginVersionWouldStayActive mirrors the Rust helper of the same name.
func oldPluginVersionWouldStayActive(oldVersion, newVersion string) bool {
	return oldVersion == DefaultPluginVersion || comparePluginVersions(oldVersion, newVersion) > 0
}

// copyDirRecursive mirrors the Rust `copy_dir_recursive`: copies files and
// directories (regular files only) from source into target.
func copyDirRecursive(source, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return storeIOError("failed to create plugin target directory", err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return storeIOError("failed to read plugin source directory", err)
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(source, entry.Name())
		targetPath := filepath.Join(target, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return storeIOError("failed to inspect plugin source entry", err)
		}
		switch {
		case info.IsDir():
			if err := copyDirRecursive(sourcePath, targetPath); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(sourcePath, targetPath); err != nil {
				return storeIOError("failed to copy plugin file", err)
			}
		}
	}
	return nil
}

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, info.Mode().Perm())
}
