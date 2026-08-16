// Package ptycap reports platform PTY capabilities without pulling in the PTY
// backend itself. Callers that only need to know whether a console PTY exists
// (tool-config selection between unified_exec and shell_command) import this
// leaf package; the spawning backend lives in internal/pty. Mirrors the Rust
// `codex_utils_pty::conpty_supported` used by the tools crate.
package ptycap
