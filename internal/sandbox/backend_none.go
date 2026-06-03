package sandbox

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/pty"
)

// noneBackend runs a command with no outer platform sandbox. It is available on
// every platform and is selected for the SandboxTypeNone preference and for
// DangerFullAccess / disabled policies. Mirrors the SandboxType::None arm of the
// transform/spawn path.
type noneBackend struct{}

// Type reports SandboxTypeNone.
func (noneBackend) Type() SandboxType { return SandboxTypeNone }

// Spawn runs the command directly (no sandbox-exec wrapper), honoring the
// requested output mode and propagating ctx for cancellation.
func (noneBackend) Spawn(ctx context.Context, req SpawnRequest) (*pty.SpawnedProcess, error) {
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("sandbox: empty command")
	}
	return spawnWithOutputMode(ctx, req.Command, req.Cwd, req.Env, req.Arg0, req.Output, req.TerminalSize)
}

// spawnWithOutputMode dispatches to the pty package's spawn helper for the given
// output mode. It is shared by the backends so stdio wiring, env/cwd handling and
// cancellation behave identically regardless of whether a sandbox wrapper is used.
func spawnWithOutputMode(
	ctx context.Context,
	command []string,
	cwd string,
	env map[string]string,
	arg0 *string,
	output OutputMode,
	terminalSize pty.TerminalSize,
) (*pty.SpawnedProcess, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("sandbox: empty command")
	}
	program := command[0]
	args := command[1:]

	switch output {
	case OutputModePTY:
		size := terminalSize
		if size.Rows == 0 && size.Cols == 0 {
			size = pty.DefaultTerminalSize()
		}
		proc, err := pty.SpawnPTY(ctx, program, args, cwd, env, arg0, size)
		if err != nil {
			return nil, fmt.Errorf("sandbox: spawn pty: %w", err)
		}
		return proc, nil
	case OutputModePipeNoStdin:
		proc, err := pty.SpawnPipeNoStdin(ctx, program, args, cwd, env, arg0)
		if err != nil {
			return nil, fmt.Errorf("sandbox: spawn pipe (no stdin): %w", err)
		}
		return proc, nil
	default: // OutputModePipe
		proc, err := pty.SpawnPipe(ctx, program, args, cwd, env, arg0)
		if err != nil {
			return nil, fmt.Errorf("sandbox: spawn pipe: %w", err)
		}
		return proc, nil
	}
}
