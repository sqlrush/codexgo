package cli

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/execserver"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// localExecService is the binary's [core.ExecService]: it runs an already-argv-
// resolved command to completion through the in-process [execserver.LocalProcess]
// backend (which spawns via internal/pty) and returns the captured stdout,
// stderr, and exit code.
//
// STUB: streaming output deltas, unified-exec session reuse, PTY interaction,
// timeouts, and sandbox-policy escalation are deferred; the turn-running subset
// the binary needs is a synchronous "spawn, capture, exit" call. The command is
// executed without an OS sandbox, matching the `danger-full-access` /
// `approval_policy = "never"` configuration the non-interactive exec path uses.
type localExecService struct {
	backend *execserver.LocalProcess
	// seq mints a unique process id per Run so concurrent calls never collide in
	// the backend's process registry.
	seq atomic.Uint64
}

var _ core.ExecService = (*localExecService)(nil)

// newLocalExecService builds an exec service over a fresh LocalProcess backend.
// Notifications are discarded (nil sender) because the synchronous Run path reads
// retained output directly rather than following pushed notifications.
func newLocalExecService() *localExecService {
	return &localExecService{backend: execserver.NewLocalProcess(nil, nil)}
}

// Run spawns req.Command in req.Cwd, drains its output to completion, and returns
// the captured streams and exit code. It is the [core.ExecService] entry point.
//
// The command argv is taken verbatim (the caller has already wrapped a shell
// command string into the user's shell, e.g. ["/bin/zsh","-lc","..."]). Errors
// from spawning or reading the process are wrapped with %w.
func (s *localExecService) Run(ctx context.Context, req core.ExecRequest) (core.ExecResult, error) {
	if len(req.Command) == 0 {
		return core.ExecResult{}, fmt.Errorf("cli: exec requires a non-empty command")
	}

	id := execserver.NewProcessId(fmt.Sprintf("codexgo-exec-%d", s.seq.Add(1)))
	started, err := s.backend.Start(ctx, execserver.ExecParams{
		ProcessID: id,
		Argv:      req.Command,
		Cwd:       req.Cwd,
	})
	if err != nil {
		return core.ExecResult{}, fmt.Errorf("cli: start command %q: %w", req.Command[0], err)
	}
	defer func() { _ = started.Process.Terminate(context.Background()) }()

	return drainProcess(ctx, started.Process)
}

// drainProcess reads a started process to completion, accumulating stdout and
// stderr (PTY output is folded into stdout) and capturing the exit code. It polls
// retained output with a bounded wait until the process reports Closed, which
// avoids the dropped-event recovery path of the live subscriber for large bursts.
func drainProcess(ctx context.Context, proc execserver.ExecProcess) (core.ExecResult, error) {
	var (
		stdout   strings.Builder
		stderr   strings.Builder
		afterSeq uint64
		exitCode int32
	)

	waitMs := uint64(1000)
	for {
		after := afterSeq
		resp, err := proc.Read(ctx, &after, nil, &waitMs)
		if err != nil {
			return core.ExecResult{}, fmt.Errorf("cli: read command output: %w", err)
		}
		for _, chunk := range resp.Chunks {
			switch chunk.Stream {
			case execserver.ExecOutputStreamStderr:
				stderr.Write(chunk.Chunk)
			default:
				// Stdout and PTY output both go to stdout (PTY merges the streams).
				stdout.Write(chunk.Chunk)
			}
		}
		afterSeq = resp.NextSeq
		if resp.Failure != nil {
			return core.ExecResult{}, fmt.Errorf("cli: command failed: %s", *resp.Failure)
		}
		if resp.Exited && resp.ExitCode != nil {
			exitCode = int32(*resp.ExitCode)
		}
		if resp.Closed {
			break
		}
		if ctx.Err() != nil {
			return core.ExecResult{}, fmt.Errorf("cli: command interrupted: %w", ctx.Err())
		}
	}

	return core.ExecResult{
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}, nil
}

// headlessUserInputRequester is the [core.UserInputRequester] for the headless
// exec path: request_user_input is advertised (codex enables it by default) but
// there is no interactive client to answer, so every call resolves as
// cancelled. The TUI / app-server clients supply a real requester when wired.
type headlessUserInputRequester struct{}

// RequestUserInput reports the request as cancelled (no interactive client).
func (headlessUserInputRequester) RequestUserInput(context.Context, string, protocol.RequestUserInputArgs) (protocol.RequestUserInputResponse, bool) {
	return protocol.RequestUserInputResponse{}, false
}
