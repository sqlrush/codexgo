// Package cli implements the top-level `codex` command surface: a faithful,
// drop-in-compatible Go port of the codex 0.136.0 CLI multitool dispatcher.
//
// The package owns argument parsing (top-level flags plus busybox-style arg0
// dispatch), subcommand routing, and process exit-code mapping. Each subcommand
// is wired to an existing internal backend (exec, login, mcp, mcpserver,
// appserver, applypatch/gitutils, threadstore, features, sandbox) rather than
// reimplementing behavior here.
//
// Design notes mirroring the Rust crate:
//
//   - Top-level flags (-c key=value, -p/--profile, --enable/--disable, --remote,
//     --remote-auth-token-env, --strict-config) are parsed before the subcommand
//     and folded into the per-subcommand config-override stream, matching
//     prepend_config_flags / FeatureToggles::to_overrides in main.rs.
//   - --remote / --remote-auth-token-env are rejected for non-interactive
//     subcommands, matching reject_remote_mode_for_subcommand.
//   - Exit codes use signals 128+n on unix and the raw code on Windows, matching
//     exit_status.rs handle_exit_status.
//
// The interactive TUI is a later roadmap phase; with no subcommand the dispatcher
// prints a clear notice and exits non-zero.
package cli
