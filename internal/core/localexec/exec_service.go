package localexec

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
)

// ExecRequest describes a command to run through the exec service.
type ExecRequest struct {
	// Command is the argv to execute.
	Command []string
	// Cwd is the working directory for the command.
	Cwd string

	// SandboxType selects which platform sandbox backend the command spawns
	// under. The zero value (SandboxTypeNone) runs the command unsandboxed,
	// preserving the prior behavior for danger-full-access turns.
	SandboxType sandbox.SandboxType
	// FileSystemSandboxPolicy is the resolved filesystem policy the backend
	// enforces (consulted only when SandboxType is not none).
	FileSystemSandboxPolicy protocol.FileSystemSandboxPolicy
	// NetworkSandboxPolicy is the resolved network policy the backend enforces.
	NetworkSandboxPolicy protocol.NetworkSandboxPolicy
	// SandboxPolicyCwd anchors sandbox policy resolution (project_roots,
	// denied-read globs); defaults to Cwd when empty.
	SandboxPolicyCwd string
}

// ExecResult is the outcome of an exec request.
type ExecResult struct {
	// ExitCode is the process exit code.
	ExitCode int32
	// Stdout / Stderr capture the command's output streams.
	Stdout string
	Stderr string
}

// ExecService runs sandboxed commands on behalf of tool calls. It abstracts
// internal/execserver.
//
// STUB: streaming output deltas, unified-exec session reuse, PTY interaction,
// and approval escalation are deferred.
type ExecService interface {
	// Run executes a command and returns its result.
	Run(ctx context.Context, req ExecRequest) (ExecResult, error)
}
