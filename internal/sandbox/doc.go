// Package sandbox ports the OpenAI Codex Rust crate codex-sandboxing
// (codex-rs/sandboxing) to Go as part of a faithful, drop-in-compatible
// reimplementation of Codex 0.136.0.
//
// The package focuses on the policy model and the macOS Seatbelt backend:
//
//   - The SandboxPolicy / PermissionProfile model and the
//     AskForApproval x SandboxPolicy matrix: [SandboxablePreference],
//     [EffectivePermissionProfile], and [ShouldRequirePlatformSandbox].
//   - macOS Seatbelt: generation of a .sbpl policy from a permission profile
//     (deny-by-default plus per-path allow/deny rules), embedding the base,
//     network, and read-only platform-default policy assets verbatim, loopback
//     proxy port extraction from *_PROXY_URL environment variables, restriction
//     of Unix domain sockets to approved proxy paths, and execution of a command
//     via /usr/bin/sandbox-exec.
//   - The common command-spawn path (environment, working directory, optional
//     PTY, and context-based cancellation), implemented on top of internal/pty.
//
// The Linux (Landlock/seccomp/bubblewrap) and Windows (restricted-token)
// backends are separate specs; this package defines the [Backend] interface and
// provides explicit "not yet implemented" stubs so callers compile on every
// platform.
//
// # Permission resolution
//
// The Rust seatbelt backend depends on a large permission-resolution engine that
// lives in the codex-protocol crate (codex-rs/protocol/src/permissions.rs). The
// Go internal/protocol package only ports the serde types of that engine, not
// its ~3000 lines of cwd-sensitive resolution logic. To keep this package
// buildable and self-contained without modifying internal/protocol, the
// resolution methods that the seatbelt backend requires are reimplemented here
// (see policy_resolve.go) operating on the protocol serde types. See the package
// deviations note for the edge cases that are intentionally simplified.
package sandbox
