package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sqlrush/codexgo/internal/installcontext"
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
	_, npm := os.LookupEnv("CODEX_MANAGED_BY_NPM")
	_, bun := os.LookupEnv("CODEX_MANAGED_BY_BUN")
	b.detail(fmt.Sprintf("current executable: %s", exe))
	b.detail(fmt.Sprintf("install context: %s", installContextLabel(ctx)))
	b.detail(fmt.Sprintf("managed by npm: %t", npm))
	b.detail(fmt.Sprintf("managed by bun: %t", bun))
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
	b.detail(fmt.Sprintf("install method: %s", installContextLabel(ctx)))
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

// updatesStatusCheck reports the locally-cached update configuration, mirroring
// updates.status in doctor.rs. It does not contact the network; the latest
// version is read from the on-disk version cache when present.
func updatesStatusCheck(dctx doctorContext) doctorCheck {
	b := newCheck("updates.status", "updates")

	checkOnStartup := true
	if dctx.Loaded && dctx.Cfg.CheckForUpdateOnStartup != nil {
		checkOnStartup = *dctx.Cfg.CheckForUpdateOnStartup
	}
	b.detail(fmt.Sprintf("check for update on startup: %t", checkOnStartup))

	if dctx.Loaded {
		versionFile := filepath.Join(dctx.CodexHome, "version.json")
		b.detail(fmt.Sprintf("version cache: %s", versionFile))
		if _, err := os.Stat(versionFile); err == nil {
			b.detail("version cache: present")
		} else {
			b.detail("version cache: not yet created")
		}
	}

	b.ok("update configuration is locally consistent")
	return b.build()
}

// firstLine returns the first line of s with surrounding whitespace trimmed.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
