package unifiedexec

import (
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

// ExecCommandRequest describes a command to open as a unified-exec session.
// Mirrors the load-bearing fields of ExecCommandRequest in mod.rs. The
// policy/approval fields (sandbox permissions, justification, prefix rule) that
// the Rust struct carries for the core ToolOrchestrator are resolved by the
// caller before spawning, so this port keeps the fields that drive the spawn.
type ExecCommandRequest struct {
	// Command is the program followed by its arguments.
	Command []string
	// HookCommand is the original shell command string, echoed back in output.
	HookCommand string
	// CallID is the originating exec_command tool call id; it is stored on the
	// process entry so later write_stdin calls echo it (the Rust
	// ProcessEntry.call_id -> event_call_id flow behind TerminalInteraction).
	CallID string
	// TurnID is the originating turn's correlation id (the Rust
	// UnifiedExecContext.turn.sub_id). It is carried to the session-stored hook so
	// the late exec-end event the background watcher emits is correlated with the
	// turn that opened the session, not whatever turn happens to be active later.
	TurnID string
	// TruncationPolicy is the originating turn's model-output truncation policy
	// (the Rust turn.truncation_policy). It is carried to the session-stored hook
	// so the late exec-end event formats its output exactly like the in-call path
	// (format_exec_output_str(turn.truncation_policy)). The zero value means the
	// caller did not resolve a policy; the watcher then leaves the output verbatim.
	TruncationPolicy truncation.TruncationPolicy
	// ProcessID is the reserved logical process id for this session.
	ProcessID int
	// YieldTimeMS is the requested output-collection window.
	YieldTimeMS uint64
	// MaxOutputTokens optionally caps the response token count.
	MaxOutputTokens *int
	// Cwd is the working directory for the child.
	Cwd string
	// SandboxCwd anchors sandbox policy resolution; defaults to Cwd when empty.
	SandboxCwd string
	// Env is the base environment; the unified-exec overlay is applied on top.
	Env map[string]string
	// TTY requests a pseudo-terminal (keeping stdin open across calls).
	TTY bool
	// SandboxType selects which platform sandbox backend to spawn under.
	SandboxType sandbox.SandboxType
	// FileSystemSandboxPolicy is the resolved filesystem policy for the sandbox.
	FileSystemSandboxPolicy protocol.FileSystemSandboxPolicy
	// NetworkSandboxPolicy is the resolved network policy for the sandbox.
	NetworkSandboxPolicy protocol.NetworkSandboxPolicy
	// Arg0 optionally overrides argv[0] of the spawned process.
	Arg0 *string
}

// WriteStdinRequest describes a write-stdin / poll call against a live session.
// Mirrors WriteStdinRequest in mod.rs.
type WriteStdinRequest struct {
	// ProcessID identifies the live session.
	ProcessID int
	// Input is the bytes to write to stdin; empty means poll-only.
	Input string
	// YieldTimeMS is the requested output-collection window.
	YieldTimeMS uint64
	// MaxOutputTokens optionally caps the response token count.
	MaxOutputTokens *int
}

// ResizeRequest describes a terminal-resize call against a live session.
type ResizeRequest struct {
	// ProcessID identifies the live session.
	ProcessID int
	// Rows is the new terminal height in cells.
	Rows uint16
	// Cols is the new terminal width in cells.
	Cols uint16
}

// Output is the result of an exec/write-stdin call. Mirrors the load-bearing
// fields of ExecCommandToolOutput: the captured bytes plus the metadata a tool
// handler needs to render the response and decide whether the session persists.
type Output struct {
	// ChunkID is a fresh per-call identifier for this output chunk.
	ChunkID string
	// EventCallID is the originating exec_command call id for the session this
	// output belongs to (the Rust ExecCommandToolOutput.event_call_id); event
	// emitters tag TerminalInteraction with it.
	EventCallID string
	// WallTime is how long the call spent collecting output.
	WallTime int64 // nanoseconds, mirroring time.Duration
	// RawOutput is the collected bytes for this call.
	RawOutput []byte
	// MaxOutputTokens echoes the request's token cap, if any.
	MaxOutputTokens *int
	// ProcessID is the live session id when the process is still running, or nil
	// when it has exited (and been removed from the store).
	ProcessID *int
	// ExitCode is the process exit code when known.
	ExitCode *int
	// OriginalTokenCount is the approximate token count of the collected output.
	OriginalTokenCount int
	// HookCommand echoes back the original shell command string.
	HookCommand string
}
