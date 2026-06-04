package cli

import (
	"context"

	"github.com/sqlrush/codexgo/internal/config"
)

// doctorContext is the resolved configuration projection shared across doctor
// checks. It is produced by configLoadCheck so downstream checks can reuse the
// already-merged configuration without re-reading config.toml. A nil value (or a
// value with Loaded=false) signals that configuration did not load, in which
// case dependent checks degrade gracefully instead of failing hard.
type doctorContext struct {
	// Loaded reports whether configuration loaded successfully.
	Loaded bool
	// CodexHome is the resolved configuration directory.
	CodexHome string
	// StoreMode is the resolved auth credentials store mode.
	StoreMode config.AuthCredentialsStoreMode
	// ChatgptBaseURL is the configured ChatGPT base URL override, or nil.
	ChatgptBaseURL *string
	// Model is the configured model name, or "" when unset.
	Model string
	// ModelProvider is the configured model provider id, or "" when unset.
	ModelProvider string
	// LogDir is the configured log directory override, or "" for the default.
	LogDir string
	// SqliteHome is the configured sqlite home override, or "" for CodexHome.
	SqliteHome string
	// McpServers is the resolved MCP server map.
	McpServers map[string]config.McpServerConfig
	// Cfg is the full resolved configuration, retained for checks that need
	// less commonly used fields (sandbox mode, approval policy, update flag).
	Cfg config.ConfigToml
}

// doctorCheckIDs is the canonical, sorted list of the 18 check IDs codex 0.136.0
// emits from `codex doctor --json`. The doctor report contains exactly these IDs
// so support tooling can compare codexgo and codex check-for-check.
var doctorCheckIDs = []string{
	"app_server.status",
	"auth.credentials",
	"config.load",
	"git.environment",
	"installation",
	"mcp.config",
	"network.env",
	"network.provider_reachability",
	"network.websocket_reachability",
	"runtime.provenance",
	"runtime.search",
	"sandbox.helpers",
	"state.paths",
	"state.rollout_db_parity",
	"system.environment",
	"terminal.env",
	"terminal.title",
	"updates.status",
}

// buildDoctorChecks runs the full bounded diagnostic set and returns the
// collected checks in a stable, id-sorted order. Each check is read-only and
// never mutates user state, matching the doctor contract in doctor.rs. The
// emitted set is exactly the 18 IDs in [doctorCheckIDs].
//
// Network reachability checks honor CODEX_DOCTOR_SKIP_NETWORK: when it is set
// they report a skipped status so offline and deterministic runs stay fast.
func buildDoctorChecks(ctx context.Context, root RootOptions) []doctorCheck {
	dctx, configCheck := configLoadCheck(root)

	checks := []doctorCheck{
		appServerCheck(dctx),
		authCheck(ctx, dctx),
		configCheck,
		gitCheck(ctx),
		installationCheck(),
		mcpConfigCheck(dctx),
		networkEnvCheck(),
		providerReachabilityCheck(ctx, dctx),
		websocketReachabilityCheck(dctx),
		runtimeProvenanceCheck(),
		runtimeSearchCheck(),
		sandboxHelpersCheck(dctx),
		statePathsCheck(dctx),
		stateRolloutParityCheck(dctx),
		systemEnvironmentCheck(),
		terminalEnvCheck(),
		terminalTitleCheck(dctx),
		updatesStatusCheck(dctx),
	}
	return checks
}
