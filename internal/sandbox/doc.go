// Package sandbox ports the OpenAI Codex Rust crate codex-sandboxing
// (codex-rs/sandboxing) to Go as part of a faithful, drop-in-compatible
// reimplementation of Codex 0.136.0.
//
// The package focuses on the policy model and three platform backends:
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
// # Native Linux and Windows backends
//
// The Linux and Windows backends are implemented natively (no shelling to
// bubblewrap or external helpers). Both wrap the command the same way the macOS
// backend wraps it with /usr/bin/sandbox-exec: by re-executing the current
// binary with the [NativeSandboxArgv0] sentinel and passing the fully-resolved
// [NativeSandboxSpec] via the [NativeSandboxSpecEnv] environment variable. A
// binary's main() detects the sentinel and dispatches to RunLinuxSandboxMain or
// RunWindowsSandboxMain (the platform-tagged helper entrypoints), which apply the
// sandbox and exec the real command.
//
//   - Linux: user/mount/pid/(net) namespaces (via unshare), a read-only root
//     with writable binds and read-only re-binds of protected subpaths, a fresh
//     /proc (skipped gracefully in restrictive containers), a Landlock
//     filesystem ruleset (raw landlock_* syscalls), and a hand-rolled seccomp
//     BPF network filter (classic BPF assembled in seccomp_bpf.go). WSL1 is
//     detected and reported as unsupported.
//   - Windows: a restricted / low-integrity primary token plus deny-read ACLs on
//     denied paths, with the command launched under that token via
//     CreateProcessAsUser. The enforcement tier follows [protocol.WindowsSandboxLevel].
//
// All native syscall access uses golang.org/x/sys (cgo-free).
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
