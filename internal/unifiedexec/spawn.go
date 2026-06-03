package unifiedexec

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/execserver"
	"github.com/sqlrush/codexgo/internal/pty"
	"github.com/sqlrush/codexgo/internal/sandbox"
)

// Spawner opens a unified-exec [Process] for a prepared request. It abstracts
// the transport (local sandboxed PTY/pipe vs. a remote exec-server) so the
// manager stays transport-agnostic. Mirrors open_session_with_exec_env, which
// dispatches between the local PTY spawn helpers and the exec-server backend.
type Spawner interface {
	// Spawn opens a process for req under the given process id. The returned
	// Process owns the spawned child and its output buffering.
	Spawn(ctx context.Context, processID int, req *ExecCommandRequest) (*Process, error)
}

// SandboxSpawner spawns local processes through a sandbox [sandbox.Backend],
// wiring stdio to a PTY (when req.TTY) or to pipes otherwise. It is the default
// Spawner. Mirrors the local branch of open_session_with_exec_env.
type SandboxSpawner struct{}

// Spawn opens a local sandboxed process for req. Mirrors the non-remote branch
// of open_session_with_exec_env: PTY spawn for tty, no-stdin pipe spawn
// otherwise, wrapped via from_spawned.
func (SandboxSpawner) Spawn(ctx context.Context, processID int, req *ExecCommandRequest) (*Process, error) {
	if len(req.Command) == 0 {
		return nil, newMissingCommandLineError()
	}

	backend, err := sandbox.NewBackend(req.SandboxType)
	if err != nil {
		return nil, newCreateProcessError(err.Error())
	}

	output := sandbox.OutputModePipeNoStdin
	if req.TTY {
		output = sandbox.OutputModePTY
	}
	sandboxCwd := req.SandboxCwd
	if sandboxCwd == "" {
		sandboxCwd = req.Cwd
	}

	spawnReq := sandbox.SpawnRequest{
		Command:                 req.Command,
		Cwd:                     req.Cwd,
		Env:                     applyUnifiedExecEnv(req.Env),
		FileSystemSandboxPolicy: req.FileSystemSandboxPolicy,
		NetworkSandboxPolicy:    req.NetworkSandboxPolicy,
		SandboxPolicyCwd:        sandboxCwd,
		Output:                  output,
		TerminalSize:            pty.DefaultTerminalSize(),
		Arg0:                    req.Arg0,
	}

	spawned, err := backend.Spawn(ctx, spawnReq)
	if err != nil {
		return nil, newCreateProcessError(err.Error())
	}
	process, err := fromSpawned(spawned, req.SandboxType)
	if err != nil {
		return nil, err
	}
	return process, nil
}

// ExecServerSpawner spawns processes through a remote [execserver.ExecBackend].
// Mirrors the is_remote() branch of open_session_with_exec_env.
type ExecServerSpawner struct {
	// Backend is the exec-server backend that starts processes.
	Backend execserver.ExecBackend
}

// Spawn opens a remote exec-server process for req. Mirrors the remote branch of
// open_session_with_exec_env: it maps the request to ExecParams, starts the
// process, and wraps it via from_exec_server_started.
func (s ExecServerSpawner) Spawn(ctx context.Context, processID int, req *ExecCommandRequest) (*Process, error) {
	if s.Backend == nil {
		return nil, newCreateProcessError("exec-server backend is not configured")
	}
	if len(req.Command) == 0 {
		return nil, newMissingCommandLineError()
	}

	params := execserver.ExecParams{
		ProcessID: execserver.NewProcessId(fmt.Sprintf("%d", processID)),
		Argv:      req.Command,
		Cwd:       req.Cwd,
		Env:       applyUnifiedExecEnv(req.Env),
		Tty:       req.TTY,
		PipeStdin: false,
		Arg0:      req.Arg0,
	}

	started, err := s.Backend.Start(ctx, params)
	if err != nil {
		return nil, newCreateProcessError(err.Error())
	}
	process, err := fromExecServerStarted(started, req.SandboxType)
	if err != nil {
		return nil, err
	}
	return process, nil
}
