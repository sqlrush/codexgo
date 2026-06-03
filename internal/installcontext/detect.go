package installcontext

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// installMethodFromExe determines the install method from the executable path,
// optional Codex home, optional package layout, and platform. It mirrors codex's
// `install_method_from_exe`.
func installMethodFromExe(exePath string, codexHome *string, packageLayout *CodexPackageLayout, isMacOS bool) InstallMethod {
	if standalone, ok := standaloneInstallMethod(exePath, codexHome, packageLayout); ok {
		return standalone
	}

	if isMacOS && (pathStartsWith(exePath, "/opt/homebrew") || pathStartsWith(exePath, "/usr/local")) {
		return newSimpleMethod(MethodBrew)
	}
	return newSimpleMethod(MethodOther)
}

// standaloneInstallMethod detects a managed standalone install. It mirrors
// codex's `standalone_install_method`: the release directory (either the package
// root, or the canonical parent of the executable) must live under
// `<codex_home>/packages/standalone/releases`.
func standaloneInstallMethod(exePath string, codexHome *string, packageLayout *CodexPackageLayout) (InstallMethod, bool) {
	if codexHome == nil {
		return InstallMethod{}, false
	}
	canonicalCodexHome, ok := canonicalAbsolutePath(*codexHome)
	if !ok {
		return InstallMethod{}, false
	}

	var releaseDir abspath.AbsolutePathBuf
	if packageLayout != nil {
		releaseDir = packageLayout.PackageDir
	} else {
		canonicalExe, ok := canonicalAbsolutePath(exePath)
		if !ok {
			return InstallMethod{}, false
		}
		parent, ok := canonicalExe.Parent()
		if !ok {
			return InstallMethod{}, false
		}
		releaseDir = parent
	}

	releasesRoot := canonicalCodexHome.
		Join("packages").
		Join(standalonePackagesDirname).
		Join(releasesDirname)
	if !pathStartsWith(releaseDir.Path(), releasesRoot.Path()) {
		return InstallMethod{}, false
	}

	resourcesDir := releaseDir.Join(resourcesDirname)
	var resourcesPtr *abspath.AbsolutePathBuf
	if isDir(resourcesDir.Path()) {
		rd := resourcesDir
		resourcesPtr = &rd
	}
	return newStandalone(releaseDir, resourcesPtr, standalonePlatform()), true
}

// packageLayoutFromExe detects a managed package layout surrounding the
// executable. It mirrors codex's `CodexPackageLayout::from_exe`: the canonical
// executable must live in a `bin/` directory whose parent contains the package
// metadata file.
func packageLayoutFromExe(exePath string) *CodexPackageLayout {
	canonicalExe, ok := canonicalAbsolutePath(exePath)
	if !ok {
		return nil
	}
	binDir, ok := canonicalExe.Parent()
	if !ok {
		return nil
	}
	if fileName(binDir.Path()) != binDirname {
		return nil
	}
	packageDir, ok := binDir.Parent()
	if !ok {
		return nil
	}
	if !isFile(packageDir.Join(packageMetadataFilename).Path()) {
		return nil
	}

	return &CodexPackageLayout{
		ResourcesDir: existingDir(packageDir.Join(resourcesDirname)),
		PathDir:      existingDir(packageDir.Join(pathDirname)),
		PackageDir:   packageDir,
		BinDir:       binDir,
	}
}

// canonicalAbsolutePath canonicalizes path into an [abspath.AbsolutePathBuf],
// returning ok=false when the path cannot be canonicalized. It mirrors codex's
// `canonical_absolute_path`.
func canonicalAbsolutePath(path string) (abspath.AbsolutePathBuf, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return abspath.AbsolutePathBuf{}, false
	}
	abs, err := abspath.FromAbsolutePathChecked(resolved)
	if err != nil {
		// EvalSymlinks may return a relative path for a relative input; fall
		// back to resolving it against the working directory.
		abs, err = abspath.FromAbsolutePath(resolved)
		if err != nil {
			return abspath.AbsolutePathBuf{}, false
		}
	}
	return abs, true
}

// existingDir returns a pointer to path when it is a directory; otherwise nil.
// It mirrors codex's `existing_dir`.
func existingDir(path abspath.AbsolutePathBuf) *abspath.AbsolutePathBuf {
	if isDir(path.Path()) {
		p := path
		return &p
	}
	return nil
}

// standalonePlatform returns the platform for a standalone release based on the
// build target. It mirrors codex's `standalone_platform`.
func standalonePlatform() StandalonePlatform {
	if isWindows() {
		return PlatformWindows
	}
	return PlatformUnix
}

// pathStartsWith reports whether path has prefix as an ancestor, comparing whole
// path components. It mirrors Rust's `Path::starts_with`, which is
// component-wise (so "/a/bc" does not start with "/a/b").
func pathStartsWith(path, prefix string) bool {
	pathParts := splitComponents(path)
	prefixParts := splitComponents(prefix)
	if len(prefixParts) > len(pathParts) {
		return false
	}
	for i := range prefixParts {
		if pathParts[i] != prefixParts[i] {
			return false
		}
	}
	return true
}

// splitComponents splits a path into its components, preserving a leading root
// marker so absolute paths compare correctly. Trailing separators are ignored.
func splitComponents(path string) []string {
	cleaned := filepath.Clean(path)
	if cleaned == string(filepath.Separator) {
		return []string{string(filepath.Separator)}
	}
	var parts []string
	if filepath.IsAbs(cleaned) {
		parts = append(parts, "")
	}
	for _, p := range strings.Split(cleaned, string(filepath.Separator)) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// fileName returns the final path component of p, mirroring Rust's
// `Path::file_name`.
func fileName(p string) string {
	base := filepath.Base(p)
	if base == string(filepath.Separator) || base == "." {
		return ""
	}
	return base
}

// isDir reports whether path is an existing directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isFile reports whether path is an existing regular file (or anything that is
// not a directory), mirroring Rust's `Path::is_file` which is true for regular
// files.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
