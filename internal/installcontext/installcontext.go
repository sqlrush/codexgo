// Package installcontext is a faithful Go port of codex's `install-context`
// crate. It detects how the running Codex binary was installed (standalone,
// npm, bun, Homebrew, or other) by inspecting the executable path, environment
// variables, and the optional managed package layout that surrounds the binary.
//
// The detection rules, directory names, and precedence match codex so behavior
// is drop-in compatible.
package installcontext

import (
	"os"
	"runtime"
	"sync"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/homedir"
)

// Directory and file names used to recognize a managed package layout. They
// mirror the corresponding constants in codex.
const (
	binDirname                = "bin"
	packageMetadataFilename   = "codex-package.json"
	pathDirname               = "codex-path"
	releasesDirname           = "releases"
	resourcesDirname          = "codex-resources"
	standalonePackagesDirname = "standalone"
	zshDirname                = "zsh"
)

// StandalonePlatform identifies the platform of a standalone release.
type StandalonePlatform int

const (
	// PlatformUnix is a Unix standalone release.
	PlatformUnix StandalonePlatform = iota
	// PlatformWindows is a Windows standalone release.
	PlatformWindows
)

// MethodKind identifies which [InstallMethod] variant is in use.
type MethodKind int

const (
	// MethodStandalone is a managed standalone release install.
	MethodStandalone MethodKind = iota
	// MethodNpm is an npm-managed install (via the codex.js shim).
	MethodNpm
	// MethodBun is a bun-managed install (via the codex.js shim).
	MethodBun
	// MethodBrew is a Homebrew install.
	MethodBrew
	// MethodOther is any other execution environment.
	MethodOther
)

// InstallMethod describes how Codex was installed. It mirrors codex's
// `InstallMethod` enum. Only the [MethodStandalone] variant carries payload
// fields (release directory, optional resources directory, and platform).
//
// The zero value is the [MethodStandalone] kind with empty fields; construct
// values through the package constructors or via [DetectFromExe].
type InstallMethod struct {
	kind         MethodKind
	releaseDir   abspath.AbsolutePathBuf
	resourcesDir *abspath.AbsolutePathBuf
	platform     StandalonePlatform
}

// Kind reports which install-method variant this value represents.
func (m InstallMethod) Kind() MethodKind {
	return m.kind
}

// Standalone returns the standalone payload (release directory, optional
// resources directory, and platform) and true when the method is
// [MethodStandalone]; otherwise it returns zero values and false.
func (m InstallMethod) Standalone() (releaseDir abspath.AbsolutePathBuf, resourcesDir *abspath.AbsolutePathBuf, platform StandalonePlatform, ok bool) {
	if m.kind != MethodStandalone {
		return abspath.AbsolutePathBuf{}, nil, PlatformUnix, false
	}
	return m.releaseDir, m.resourcesDir, m.platform, true
}

// newStandalone constructs a [MethodStandalone] install method.
func newStandalone(releaseDir abspath.AbsolutePathBuf, resourcesDir *abspath.AbsolutePathBuf, platform StandalonePlatform) InstallMethod {
	return InstallMethod{
		kind:         MethodStandalone,
		releaseDir:   releaseDir,
		resourcesDir: resourcesDir,
		platform:     platform,
	}
}

// newSimpleMethod constructs an [InstallMethod] with no payload for the npm,
// bun, brew, and other kinds.
func newSimpleMethod(kind MethodKind) InstallMethod {
	return InstallMethod{kind: kind}
}

// CodexPackageLayout describes a managed package directory layout surrounding the
// Codex executable. It mirrors codex's `CodexPackageLayout`.
type CodexPackageLayout struct {
	// PackageDir is the package root that contains the metadata file and layout
	// directories.
	PackageDir abspath.AbsolutePathBuf
	// BinDir is the directory containing the Codex entrypoint executable.
	BinDir abspath.AbsolutePathBuf
	// ResourcesDir is the directory of managed helper binaries and data files,
	// when present.
	ResourcesDir *abspath.AbsolutePathBuf
	// PathDir is the folder to prepend to PATH, when present.
	PathDir *abspath.AbsolutePathBuf
}

// InstallContext bundles the detected install method with the optional package
// layout. It mirrors codex's `InstallContext`.
type InstallContext struct {
	Method        InstallMethod
	PackageLayout *CodexPackageLayout
}

// currentContext caches the process-wide install context computed by [Current].
var (
	currentOnce sync.Once
	currentCtx  *InstallContext
)

// DetectFromExe computes the [InstallContext] for the given executable path and
// environment flags, consulting the Codex home directory via homedir. It mirrors
// codex's `InstallContext::from_exe`.
//
// currentExe is the path to the running executable, or "" when unknown.
func DetectFromExe(isMacOS bool, currentExe string, managedByNpm, managedByBun bool) InstallContext {
	var codexHome *string
	if home, err := homedir.FindCodexHome(); err == nil {
		codexHome = &home
	}
	return detectFromExeWithCodexHome(isMacOS, currentExe, managedByNpm, managedByBun, codexHome)
}

// detectFromExeWithCodexHome is the testable core of [DetectFromExe]. It mirrors
// codex's `InstallContext::from_exe_with_codex_home`. A nil codexHome represents
// an absent/unresolvable Codex home directory.
func detectFromExeWithCodexHome(isMacOS bool, currentExe string, managedByNpm, managedByBun bool, codexHome *string) InstallContext {
	var packageLayout *CodexPackageLayout
	if currentExe != "" {
		packageLayout = packageLayoutFromExe(currentExe)
	}

	var method InstallMethod
	switch {
	case managedByNpm:
		method = newSimpleMethod(MethodNpm)
	case managedByBun:
		method = newSimpleMethod(MethodBun)
	case currentExe != "":
		method = installMethodFromExe(currentExe, codexHome, packageLayout, isMacOS)
	default:
		method = newSimpleMethod(MethodOther)
	}

	return InstallContext{Method: method, PackageLayout: packageLayout}
}

// Current returns the process-wide [InstallContext], computed once on first call
// from the current executable and CODEX_MANAGED_BY_NPM / CODEX_MANAGED_BY_BUN
// environment variables. It mirrors codex's `InstallContext::current`.
func Current() *InstallContext {
	currentOnce.Do(func() {
		exe := ""
		if path, err := os.Executable(); err == nil {
			exe = path
		}
		_, npm := os.LookupEnv("CODEX_MANAGED_BY_NPM")
		_, bun := os.LookupEnv("CODEX_MANAGED_BY_BUN")
		ctx := DetectFromExe(runtime.GOOS == "darwin", exe, npm, bun)
		currentCtx = &ctx
	})
	return currentCtx
}
