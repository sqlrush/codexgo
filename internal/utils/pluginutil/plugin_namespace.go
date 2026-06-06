package pluginutil

// Resolve plugin namespace from skill file paths by walking ancestors for a
// plugin manifest (`plugin.json`).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/brand"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// discoverablePluginManifestPaths lists, in priority order, the relative paths
// at which a plugin manifest may live. codexgo's own ".codexgo-plugin"
// convention comes first; the upstream codex ".codex-plugin" and Claude
// ".claude-plugin" conventions are kept as compatibility fallbacks so existing
// ecosystem plugins remain installable (mirrors the Rust
// DISCOVERABLE_PLUGIN_MANIFEST_PATHS constant plus the codexgo entry).
//
// It is an unexported package-level value; callers receive copies and cannot
// mutate it.
var discoverablePluginManifestPaths = [...]string{
	filepath.Join(brand.PluginManifestDir, "plugin.json"),
	filepath.Join(brand.PluginManifestDirCodexCompat, "plugin.json"),
	filepath.Join(".claude-plugin", "plugin.json"),
}

// rawPluginManifestName mirrors the Rust `RawPluginManifestName` struct used to
// extract just the `name` field from a plugin manifest. Unknown fields are
// ignored and a missing `name` defaults to the empty string.
type rawPluginManifestName struct {
	Name string `json:"name"`
}

// FindPluginManifestPath mirrors the Rust `find_plugin_manifest_path`.
//
// It returns the first discoverable manifest path under pluginRoot that is a
// regular file, or ok=false when none exists. pluginRoot is treated as an
// ordinary filesystem path; it is not modified.
func FindPluginManifestPath(pluginRoot string) (string, bool) {
	for _, relativePath := range discoverablePluginManifestPaths {
		manifestPath := filepath.Join(pluginRoot, relativePath)
		if info, err := os.Stat(manifestPath); err == nil && info.Mode().IsRegular() {
			return manifestPath, true
		}
	}
	return "", false
}

// pluginManifestName mirrors the Rust `plugin_manifest_name`.
//
// It locates the first discoverable manifest under pluginRoot that fs reports as
// a file, reads it, parses the `name` field, and resolves the effective plugin
// name:
//
//   - When the manifest `name` is blank (empty or whitespace-only), the plugin
//     root's final path component is used, falling back to the (blank) raw name
//     when the root has no final component.
//   - Otherwise the raw, untrimmed manifest `name` is used verbatim.
//
// It returns ok=false when no manifest file is found, the file cannot be read,
// or the contents are not valid JSON, matching the Rust use of `Option`.
func pluginManifestName(ctx context.Context, fs FileSystem, pluginRoot abspath.AbsolutePathBuf) (string, bool) {
	var manifestPath abspath.AbsolutePathBuf
	found := false
	for _, relativePath := range discoverablePluginManifestPaths {
		candidate := pluginRoot.Join(relativePath)
		metadata, err := fs.GetMetadata(ctx, candidate)
		if err == nil && metadata.IsFile {
			manifestPath = candidate
			found = true
			break
		}
	}
	if !found {
		return "", false
	}

	contents, err := fs.ReadFileText(ctx, manifestPath)
	if err != nil {
		return "", false
	}

	var raw rawPluginManifestName
	if err := json.Unmarshal([]byte(contents), &raw); err != nil {
		return "", false
	}

	if strings.TrimSpace(raw.Name) == "" {
		if base := finalPathComponent(pluginRoot); base != "" {
			return base, true
		}
		// No usable final component: fall back to the (blank) raw name, matching
		// Rust's `unwrap_or(raw_name.as_str())`.
		return raw.Name, true
	}
	return raw.Name, true
}

// finalPathComponent returns the final path component of an absolute path,
// mirroring Rust's `Path::file_name`. It returns "" when the path has no normal
// final component (for example a filesystem root), where Rust's `file_name`
// yields None.
func finalPathComponent(path abspath.AbsolutePathBuf) string {
	base := filepath.Base(path.Path())
	switch base {
	case ".", string(filepath.Separator), "":
		return ""
	}
	// filepath.Base never returns a bare volume root such as "/" or "C:\\" as a
	// normal component, but guard against a drive-letter root just in case.
	if filepath.VolumeName(base) == strings.TrimRight(base, `\/`) && strings.TrimRight(base, `\/`) != "" {
		return ""
	}
	return base
}

// PluginNamespaceForSkillPath mirrors the Rust `plugin_namespace_for_skill_path`.
//
// It returns the plugin manifest `name` for the nearest ancestor of path that
// contains a valid plugin manifest (using the same name rules as full manifest
// loading in codex-core), or ok=false when no ancestor has a manifest.
//
// path and its ancestors are inspected through fs; nothing is mutated.
func PluginNamespaceForSkillPath(ctx context.Context, fs FileSystem, path abspath.AbsolutePathBuf) (string, bool) {
	for _, ancestor := range path.Ancestors() {
		if name, ok := pluginManifestName(ctx, fs, ancestor); ok {
			return name, true
		}
	}
	return "", false
}
