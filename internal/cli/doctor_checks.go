package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/gitutils"
	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/internal/state"
	"github.com/sqlrush/codexgo/internal/termdetect"
)

// buildDoctorChecks runs the full bounded diagnostic set in the order the human
// report presents them, returning the collected checks. Each check is read-only
// and never mutates user state, matching the doctor contract in doctor.rs.
func buildDoctorChecks(ctx context.Context, root RootOptions) []doctorCheck {
	checks := []doctorCheck{
		systemCheck(),
		installMethodCheck(),
		terminalCheck(),
	}

	cfg, cfgCheck := configLoadCheck(root)
	checks = append(checks, cfgCheck)

	checks = append(checks,
		authCheck(ctx, cfg),
		stateCheck(cfg),
		gitCheck(ctx),
		providerHealthCheck(ctx),
	)
	return checks
}

// systemCheck reports the OS, architecture, and Go runtime.
func systemCheck() doctorCheck {
	b := newCheck("system", "system")
	b.ok(fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	b.detail(fmt.Sprintf("os: %s", runtime.GOOS))
	b.detail(fmt.Sprintf("arch: %s", runtime.GOARCH))
	b.detail(fmt.Sprintf("go: %s", runtime.Version()))
	return b.build()
}

// installMethodCheck reports the resolved Codex executable path, used as a proxy
// for the installation method (npm vs standalone vs source).
func installMethodCheck() doctorCheck {
	b := newCheck("install-method", "installation")
	exe, err := os.Executable()
	if err != nil {
		b.warn("could not resolve the Codex executable path").detail(err.Error())
		return b.build()
	}
	b.ok(exe)
	b.detail(fmt.Sprintf("executable: %s", exe))
	b.detail(fmt.Sprintf("version: %s", Version))
	return b.build()
}

// terminalCheck reports the detected terminal, warning when TERM=dumb (the TUI
// cannot run there), mirroring the dumb-terminal handling in main.rs.
func terminalCheck() doctorCheck {
	b := newCheck("terminal", "terminal")
	info := termdetect.Detect()
	if info.Term != "" {
		b.detail(fmt.Sprintf("TERM: %s", info.Term))
	}
	if info.TermProgram != "" {
		b.detail(fmt.Sprintf("TERM_PROGRAM: %s", info.TermProgram))
	}
	if info.Name == termdetect.TerminalDumb {
		b.warn("TERM is set to \"dumb\"; the interactive TUI may not work").
			remedy("Run in a supported terminal or unset TERM")
		return b.build()
	}
	b.ok("terminal detected")
	return b.build()
}

// configLoadCheck loads the merged configuration and reports unknown-field
// warnings. It returns the loaded projection (or nil on failure) so downstream
// checks can reuse it.
func configLoadCheck(root RootOptions) (*loadedConfig, doctorCheck) {
	b := newCheck("config-load", "config")
	overrides, err := root.Overrides.Parse()
	if err != nil {
		b.fail("could not parse -c overrides").detail(err.Error())
		return nil, b.build()
	}
	result, err := config.Load(config.LoadOptions{
		Profile:      root.Profile,
		CliOverrides: overrides,
		StrictConfig: root.StrictConfig,
	})
	if err != nil {
		b.fail("could not load configuration").detail(err.Error()).
			remedy("Fix the reported config.toml error")
		return nil, b.build()
	}
	for _, w := range result.Warnings {
		b.detail(w)
	}
	cfg := &loadedConfig{
		CodexHome:      result.CodexHome,
		StoreMode:      resolveStoreMode(result.Config.CliAuthCredentialsStore),
		ChatgptBaseURL: result.Config.ChatgptBaseURL,
	}
	b.detail(fmt.Sprintf("codex_home: %s", result.CodexHome))
	if len(result.Warnings) > 0 {
		b.warn(fmt.Sprintf("loaded with %d warning(s)", len(result.Warnings)))
	} else {
		b.ok("configuration loaded")
	}
	return cfg, b.build()
}

// authCheck reports the current login state without exposing secrets, mirroring
// the redacted auth check in doctor.rs.
func authCheck(ctx context.Context, cfg *loadedConfig) doctorCheck {
	b := newCheck("auth", "auth")
	if cfg == nil {
		b.warn("skipped: configuration did not load")
		return b.build()
	}
	stored, err := login.LoadAuthDotJson(cfg.CodexHome, cfg.StoreMode)
	if err != nil {
		b.warn("could not read stored credentials").detail(err.Error())
		return b.build()
	}
	if stored == nil {
		b.warn("not logged in").remedy("Run `codex login`")
		return b.build()
	}
	auth, err := login.FromAuthDotJson(ctx, http.DefaultClient, cfg.CodexHome, stored, cfg.StoreMode, cfg.ChatgptBaseURL)
	if err != nil {
		b.warn("stored credentials are present but could not be loaded").detail(err.Error())
		return b.build()
	}
	b.ok(fmt.Sprintf("logged in (%s)", auth.AuthMode()))
	return b.build()
}

// stateCheck reports the resolved local state database paths and whether they
// exist, without opening or migrating them.
func stateCheck(cfg *loadedConfig) doctorCheck {
	b := newCheck("state", "state")
	if cfg == nil {
		b.warn("skipped: configuration did not load")
		return b.build()
	}
	statePath := state.StateDBPath(cfg.CodexHome)
	b.detail(fmt.Sprintf("state_db: %s", statePath))
	if _, err := os.Stat(statePath); err == nil {
		b.detail("state_db: present")
	} else {
		b.detail("state_db: not yet created")
	}
	b.ok("state paths resolved")
	return b.build()
}

// gitCheck reports whether the working directory is inside a git repository.
func gitCheck(ctx context.Context) doctorCheck {
	_ = ctx
	b := newCheck("git", "git")
	cwd := resolveCwd()
	root, ok := gitutils.GetGitRepoRoot(cwd)
	if !ok {
		b.warn("not inside a git repository").
			remedy("Run Codex from within a git working tree for diff/apply support")
		return b.build()
	}
	b.ok("inside a git repository").detail(fmt.Sprintf("repo_root: %s", root))
	return b.build()
}

// providerHealthCheck performs a bounded reachability probe of the default model
// provider host, reporting reachability without leaking credentials. It uses a
// short timeout so the doctor stays fast and never blocks.
func providerHealthCheck(ctx context.Context) doctorCheck {
	b := newCheck("provider-health", "provider")
	const probeURL = "https://api.openai.com"

	// Allow offline/test runs to skip the live probe deterministically.
	if os.Getenv("CODEX_DOCTOR_SKIP_NETWORK") != "" {
		b.ok("provider reachability probe skipped").
			detail("CODEX_DOCTOR_SKIP_NETWORK is set")
		return b.build()
	}

	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodHead, probeURL, nil)
	if err != nil {
		b.warn("could not build provider probe request").detail(err.Error())
		return b.build()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.warn(fmt.Sprintf("provider %s is not reachable", probeURL)).
			detail(err.Error()).
			remedy("Check your network connection or proxy settings")
		return b.build()
	}
	defer resp.Body.Close()
	b.ok(fmt.Sprintf("provider %s reachable", probeURL)).
		detail(fmt.Sprintf("status: %s", resp.Status))
	return b.build()
}
