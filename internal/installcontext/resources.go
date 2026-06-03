package installcontext

import (
	"path/filepath"
	"runtime"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// isWindows reports whether the running platform is Windows. It is a package
// variable so tests can exercise platform-specific behavior, mirroring the
// reference crate's `cfg!(windows)` gating.
var isWindows = func() bool {
	return runtime.GOOS == "windows"
}

// RgCommand returns the path to the bundled ripgrep binary when one is present
// in the package layout's PATH directory or the standalone resources directory,
// falling back to the bare command name otherwise. It mirrors codex's
// `InstallContext::rg_command` and returns a filesystem path string.
func (c *InstallContext) RgCommand() string {
	if c.PackageLayout != nil && c.PackageLayout.PathDir != nil {
		bundled := c.PackageLayout.PathDir.Join(defaultRgCommand())
		if isFile(bundled.Path()) {
			return bundled.Path()
		}
	}

	if _, resourcesDir, _, ok := c.Method.Standalone(); ok && resourcesDir != nil {
		bundled := resourcesDir.Join(defaultRgCommand())
		if isFile(bundled.Path()) {
			return bundled.Path()
		}
	}

	return defaultRgCommand()
}

// BundledResource returns the absolute path to a bundled resource file when it
// exists in the package layout's resources directory or the standalone resources
// directory. It mirrors codex's `InstallContext::bundled_resource`.
func (c *InstallContext) BundledResource(fileName string) (abspath.AbsolutePathBuf, bool) {
	if c.PackageLayout != nil && c.PackageLayout.ResourcesDir != nil {
		resource := c.PackageLayout.ResourcesDir.Join(fileName)
		if isFile(resource.Path()) {
			return resource, true
		}
	}

	if _, resourcesDir, _, ok := c.Method.Standalone(); ok && resourcesDir != nil {
		resource := resourcesDir.Join(fileName)
		if isFile(resource.Path()) {
			return resource, true
		}
	}

	return abspath.AbsolutePathBuf{}, false
}

// BundledZshPath returns the absolute path to the bundled zsh binary, or false
// when none is available (always false on Windows). It mirrors codex's
// `InstallContext::bundled_zsh_path`.
func (c *InstallContext) BundledZshPath() (abspath.AbsolutePathBuf, bool) {
	if isWindows() {
		return abspath.AbsolutePathBuf{}, false
	}
	return c.BundledResource(zshResourcePath())
}

// BundledZshBinDir returns the directory containing the bundled zsh binary, or
// false when none is available. It mirrors codex's
// `InstallContext::bundled_zsh_bin_dir`.
func (c *InstallContext) BundledZshBinDir() (abspath.AbsolutePathBuf, bool) {
	zshPath, ok := c.BundledZshPath()
	if !ok {
		return abspath.AbsolutePathBuf{}, false
	}
	return zshPath.Parent()
}

// defaultRgCommand returns the platform-appropriate bare ripgrep command name.
// It mirrors codex's `default_rg_command`.
func defaultRgCommand() string {
	if isWindows() {
		return "rg.exe"
	}
	return "rg"
}

// zshResourcePath returns the relative path of the bundled zsh binary within the
// resources directory ("zsh/bin/zsh"). It mirrors codex's `zsh_resource_path`.
func zshResourcePath() string {
	return filepath.Join(zshDirname, binDirname, "zsh")
}
