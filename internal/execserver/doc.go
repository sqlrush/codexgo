// Package execserver is a faithful, drop-in-compatible Go port of the core of
// codex's Rust crate codex-rs/exec-server (Codex 0.136.0).
//
// It provides the process-execution service and the filesystem helper used by
// codex's executor:
//
//   - The wire protocol types (process/* and fs/* JSON-RPC params, responses,
//     and notifications), reproduced byte-for-byte after key-order
//     canonicalization. See protocol.go.
//   - [ProcessId], the connection-scoped logical process handle.
//   - The [ExecBackend] / [ExecProcess] abstractions plus the pushed-event log
//     ([ExecProcessEvent], with bounded replay history). See process.go.
//   - [LocalProcess], the in-process backend that spawns commands through
//     internal/pty (PTY or pipe), streams stdout/stderr/pty output with bounded
//     retention and truncation, supports stdin writes, applies the
//     ShellEnvironmentPolicy filtering, and emits exited/closed lifecycle
//     notifications. See local_process.go.
//   - The fs helper request/response envelopes and the direct dispatch over
//     internal/filesystem. See fs_helper.go and fs_helper_main.go.
//
// The Rust crate is built on tokio + serde; this port uses goroutines,
// channels, context.Context, and encoding/json. JSON shapes match the Rust serde
// derives exactly (camelCase fields, transparent base64 byte chunks, internally
// and adjacently tagged enums where applicable).
//
// Components of the upstream crate that are not part of the core
// process-execution + fs-helper service (the websocket/HTTP server transports,
// the relay, the remote environment client, the environment TOML provider, and
// the HTTP-request proxy) are out of scope for this package.
package execserver
