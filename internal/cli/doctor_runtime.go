package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sqlrush/codexgo/internal/installcontext"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// installMethodName returns the human-readable install-method label for the given
// context, mirroring the Rust install_method_name mapping.
func installMethodName(ctx *installcontext.InstallContext) string {
	switch ctx.Method.Kind() {
	case installcontext.MethodStandalone:
		return "standalone"
	case installcontext.MethodNpm:
		return "npm"
	case installcontext.MethodBun:
		return "bun"
	case installcontext.MethodBrew:
		return "brew"
	default:
		return "local build"
	}
}

// installContextLabel returns the short install-context label used in detail
// lines (the lower-cased method name without the "local build" expansion).
func installContextLabel(ctx *installcontext.InstallContext) string {
	switch ctx.Method.Kind() {
	case installcontext.MethodStandalone:
		return "standalone"
	case installcontext.MethodNpm:
		return "npm"
	case installcontext.MethodBun:
		return "bun"
	case installcontext.MethodBrew:
		return "brew"
	default:
		return "other"
	}
}

// describeInstallContext renders the descriptive install-context string used by
// the installation and runtime.provenance checks, mirroring
// describe_install_context in doctor.rs. For standalone installs it includes the
// release/package layout directories; for the npm/bun/brew/other methods it
// appends the package layout when present, otherwise it is just the method name.
func describeInstallContext(ctx *installcontext.InstallContext) string {
	method := installContextLabel(ctx)
	if releaseDir, resourcesDir, platform, ok := ctx.Method.Standalone(); ok {
		platformName := "unix"
		if platform == installcontext.PlatformWindows {
			platformName = "windows"
		}
		if ctx.PackageLayout != nil {
			return fmt.Sprintf(
				"standalone (%s, package %s, bin %s, resources %s, path %s)",
				platformName,
				ctx.PackageLayout.PackageDir.String(),
				ctx.PackageLayout.BinDir.String(),
				displayOptionalPath(ctx.PackageLayout.ResourcesDir),
				displayOptionalPath(ctx.PackageLayout.PathDir),
			)
		}
		return fmt.Sprintf(
			"standalone (%s, release %s, resources %s)",
			platformName,
			releaseDir.String(),
			displayOptionalAbsPath(resourcesDir),
		)
	}
	return describeMethodWithPackageLayout(method, ctx.PackageLayout)
}

// describeMethodWithPackageLayout renders the package-layout suffix for the
// npm/bun/brew/other install methods, mirroring describe_method_with_package_layout.
func describeMethodWithPackageLayout(method string, layout *installcontext.CodexPackageLayout) string {
	if layout == nil {
		return method
	}
	return fmt.Sprintf(
		"%s (package %s, bin %s, resources %s, path %s)",
		method,
		layout.PackageDir.String(),
		layout.BinDir.String(),
		displayOptionalPath(layout.ResourcesDir),
		displayOptionalPath(layout.PathDir),
	)
}

// displayOptionalPath renders an optional package-layout directory, or "none"
// when unset, mirroring display_optional_path in doctor.rs.
func displayOptionalPath(path *abspath.AbsolutePathBuf) string {
	return displayOptionalAbsPath(path)
}

// displayOptionalAbsPath renders an optional absolute path, or "none" when nil.
func displayOptionalAbsPath(path *abspath.AbsolutePathBuf) string {
	if path == nil {
		return "none"
	}
	return path.String()
}

// installationCheck reports the resolved executable path and install provenance,
// mirroring installation in doctor.rs. It is read-only and does not consult npm.
func installationCheck() doctorCheck {
	b := newCheck("installation", "install")
	ctx := installcontext.Current()
	exe, err := os.Executable()
	if err != nil {
		b.warn("could not resolve the Codex executable path").
			detail(fmt.Errorf("executable lookup error: %w", err).Error())
		return b.build()
	}
	_, npm := os.LookupEnv("CODEXGO_MANAGED_BY_NPM")
	_, bun := os.LookupEnv("CODEXGO_MANAGED_BY_BUN")
	// Detail emission order mirrors installation_check in doctor.rs.
	b.detail(fmt.Sprintf("current executable: %s", exe))
	b.detail(fmt.Sprintf("install context: %s", describeInstallContext(ctx)))
	b.detail(fmt.Sprintf("managed by npm: %t", npm))
	b.detail(fmt.Sprintf("managed by bun: %t", bun))
	b.detail(fmt.Sprintf("managed package root: %s", envPathOrNotSet("CODEXGO_MANAGED_PACKAGE_ROOT")))

	entries := codexPathEntries()
	if len(entries) > 1 {
		b.detail(fmt.Sprintf("PATH codex entries: %d", len(entries)))
	}
	for i, path := range entries {
		b.detail(fmt.Sprintf("PATH codex #%d: %s", i+1, path))
	}

	b.ok("installation looks consistent")
	return b.build()
}

// runtimeProvenanceCheck reports the running build's version, platform, install
// method, and executable path, mirroring runtime.provenance in doctor.rs.
func runtimeProvenanceCheck() doctorCheck {
	b := newCheck("runtime.provenance", "runtime")
	ctx := installcontext.Current()
	platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	exe := "unknown"
	if path, err := os.Executable(); err == nil {
		exe = path
	}
	b.detail(fmt.Sprintf("version: %s", Version))
	b.detail(fmt.Sprintf("platform: %s", platform))
	b.detail(fmt.Sprintf("install method: %s", describeInstallContext(ctx)))
	b.detail(fmt.Sprintf("commit: %s", BuildCommit))
	b.detail(fmt.Sprintf("current executable: %s", exe))
	b.ok(fmt.Sprintf("running %s on %s", installMethodName(ctx), platform))
	return b.build()
}

// runtimeSearchCheck verifies that the search command (ripgrep) selected by the
// install context is usable, mirroring runtime.search in doctor.rs. A missing or
// unverifiable command degrades to a warning because file search may not work.
func runtimeSearchCheck() doctorCheck {
	b := newCheck("runtime.search", "search")
	ctx := installcontext.Current()
	rgCommand := ctx.RgCommand()
	provider := "system"
	if filepath.IsAbs(rgCommand) || strings.ContainsRune(rgCommand, filepath.Separator) {
		provider = "bundled"
	}
	b.detail(fmt.Sprintf("search command: %s", rgCommand))
	b.detail(fmt.Sprintf("search provider: %s", provider))

	if provider == "bundled" {
		info, err := os.Stat(rgCommand)
		switch {
		case err == nil && info.Mode().IsRegular():
			b.detail("search command readiness: file exists")
			b.ok(fmt.Sprintf("search is OK (%s)", provider))
		case err == nil:
			b.detail("search command readiness: path is not a file")
			b.warn("search command could not be verified").
				remedy("Install ripgrep or repair the bundled Codex package.")
		default:
			b.detail(fmt.Sprintf("search command readiness: %v", err))
			b.warn("search command could not be verified").
				remedy("Install ripgrep or repair the bundled Codex package.")
		}
		return b.build()
	}

	out, err := exec.Command(rgCommand, "--version").Output()
	if err != nil {
		b.detail(fmt.Sprintf("search command readiness: %v", err))
		b.warn("search command could not be verified").
			remedy("Install ripgrep or repair the bundled Codex package.")
		return b.build()
	}
	version := "rg version unknown"
	if line := firstLine(string(out)); line != "" {
		version = line
	}
	b.detail(fmt.Sprintf("search command readiness: %s", version))
	b.ok(fmt.Sprintf("search is OK (%s)", provider))
	return b.build()
}

// updatesStatusCheck reports the locally-cached update configuration and a bounded
// latest-version probe, mirroring updates.status in doctor.rs. The probe honors
// CODEXGO_DOCTOR_SKIP_NETWORK so offline/deterministic runs stay local.
//
// Detail emission order mirrors updates_check: check-for-update flag, update
// action, version cache path/parse rows, then the latest-version rows.
func updatesStatusCheck(dctx doctorContext) doctorCheck {
	b := newCheck("updates.status", "updates")
	ctx := installcontext.Current()

	checkOnStartup := true
	if dctx.Loaded && dctx.Cfg.CheckForUpdateOnStartup != nil {
		checkOnStartup = *dctx.Cfg.CheckForUpdateOnStartup
	}
	b.detail(fmt.Sprintf("check for update on startup: %t", checkOnStartup))
	b.detail(fmt.Sprintf("update action: %s", updateActionLabel(ctx)))

	if dctx.Loaded {
		versionCacheDetails(b, filepath.Join(dctx.CodexHome, "version.json"))
	}

	if latest, err := fetchLatestVersion(); err == nil && latest != "" {
		b.detail(fmt.Sprintf("latest version: %s", latest))
		if versionIsNewer(latest, Version) {
			b.detail("latest version status: newer version is available")
		} else {
			b.detail("latest version status: current version is not older")
		}
	} else if err != nil {
		b.detail(fmt.Sprintf("latest version probe: %v", err))
	}

	b.ok("update configuration is locally consistent")
	return b.build()
}

// updateActionLabel maps the install method to the user-facing update command,
// mirroring update_action_label in updates.rs.
func updateActionLabel(ctx *installcontext.InstallContext) string {
	switch ctx.Method.Kind() {
	case installcontext.MethodNpm:
		return "npm install -g @openai/codex"
	case installcontext.MethodBun:
		return "bun install -g @openai/codex"
	case installcontext.MethodBrew:
		return "brew upgrade --cask codex"
	case installcontext.MethodStandalone:
		return "standalone installer"
	default:
		return "manual or unknown"
	}
}

// versionCacheDetails emits the version-cache path detail and a follow-up status
// row, mirroring push_cached_version_details in updates.rs. A present cache emits
// the cached latest/last-checked/dismissed rows; a missing cache emits a second
// "version cache: missing" row, which collapses into the [path, "missing"] array
// codex emits.
func versionCacheDetails(b *checkBuilder, versionFile string) {
	b.detail(fmt.Sprintf("version cache: %s", versionFile))
	contents, err := os.ReadFile(versionFile)
	switch {
	case err == nil:
		info, parseErr := parseVersionCache(contents)
		if parseErr != nil {
			b.detail(fmt.Sprintf("version cache parse: %v", parseErr))
			return
		}
		b.detail(fmt.Sprintf("cached latest version: %s", info.LatestVersion))
		if info.LastCheckedAt != "" {
			b.detail(fmt.Sprintf("last checked at: %s", info.LastCheckedAt))
		}
		if info.DismissedVersion != "" {
			b.detail(fmt.Sprintf("dismissed version: %s", info.DismissedVersion))
		}
	case os.IsNotExist(err):
		b.detail("version cache: missing")
	default:
		b.detail(fmt.Sprintf("version cache read: %v", err))
	}
}

// envPathOrNotSet returns the value of the named env var, or "not set" when it is
// absent, mirroring push_env_path_detail in doctor.rs.
func envPathOrNotSet(name string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return "not set"
}

// codexPathEntries returns the codex executables resolvable on PATH, mirroring
// codex_path_entries in doctor.rs (`which -a codex` / `where codex`). Errors and
// blank lines are dropped so the detail rows stay clean.
func codexPathEntries() []string {
	program, args := "which", []string{"-a", "codex"}
	if runtime.GOOS == "windows" {
		program, args = "where", []string{"codex"}
	}
	out, err := exec.Command(program, args...).Output()
	if err != nil {
		return nil
	}
	var entries []string
	for _, line := range strings.Split(string(out), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}

// firstLine returns the first line of s with surrounding whitespace trimmed.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
