package pty

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

// stdinMode selects how the child's stdin is wired in pipe mode.
type stdinMode int

const (
	stdinPiped stdinMode = iota // stdin connected to the writer channel
	stdinNull                   // stdin closed immediately
)

// SpawnPipe spawns a process using ordinary pipes (no PTY), returning a handle
// for stdin, split stdout/stderr, and exit. Mirrors pipe::spawn_process.
//
// program is the executable; args are its arguments (not including arg0); cwd is
// the working directory; env is the full environment (the child's environment is
// cleared and replaced, matching env_clear in codex); arg0, when non-nil,
// overrides argv[0]. The context cancels the spawn and is propagated to the
// child via exec.CommandContext.
func SpawnPipe(ctx context.Context, program string, args []string, cwd string, env map[string]string, arg0 *string) (*SpawnedProcess, error) {
	return spawnPipe(ctx, program, args, cwd, env, arg0, stdinPiped)
}

// SpawnPipeNoStdin spawns a process using ordinary pipes but closes stdin
// immediately. Mirrors pipe::spawn_process_no_stdin.
func SpawnPipeNoStdin(ctx context.Context, program string, args []string, cwd string, env map[string]string, arg0 *string) (*SpawnedProcess, error) {
	return spawnPipe(ctx, program, args, cwd, env, arg0, stdinNull)
}

func spawnPipe(ctx context.Context, program string, args []string, cwd string, env map[string]string, arg0 *string, mode stdinMode) (*SpawnedProcess, error) {
	if program == "" {
		return nil, fmt.Errorf("pty: missing program for pipe spawn")
	}

	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = cwd
	cmd.Env = envSlice(env)
	cmd.SysProcAttr = sysProcAttrDetached()
	applyArg0(cmd, arg0)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pty: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("pty: stderr pipe: %w", err)
	}

	var stdinWriteCloser interface {
		Write([]byte) (int, error)
		Close() error
	}
	if mode == stdinPiped {
		sp, perr := cmd.StdinPipe()
		if perr != nil {
			return nil, fmt.Errorf("pty: stdin pipe: %w", perr)
		}
		stdinWriteCloser = sp
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pty: start %q: %w", program, err)
	}

	pid := cmd.Process.Pid

	stdoutCh := make(chan []byte, 128)
	stderrCh := make(chan []byte, 128)
	exitCh := make(chan int, 1)

	// done is closed once the child has exited (or on Terminate) so the stdin
	// writer goroutine stops blocking on the stdin channel and does not leak.
	// closeDone is idempotent because both the exit goroutine and the cleanup
	// hook may signal it.
	done := make(chan struct{})
	var doneOnce sync.Once
	closeDone := func() { doneOnce.Do(func() { close(done) }) }

	var stdinCh chan []byte
	var readers sync.WaitGroup
	readers.Add(2)
	go readStream(stdoutPipe, stdoutCh, &readers)
	go readStream(stderrPipe, stderrCh, &readers)

	if mode == stdinPiped {
		stdinCh = make(chan []byte, 128)
		go func() {
			defer func() { _ = stdinWriteCloser.Close() }()
			for {
				select {
				case chunk, ok := <-stdinCh:
					if !ok {
						return
					}
					if _, werr := stdinWriteCloser.Write(chunk); werr != nil {
						return
					}
				case <-done:
					return
				}
			}
		}()
	}

	p := newProcess(stdinCh, pgidTerminator{pgid: pid}, nil, closeDone)

	go func() {
		// Wait for output to drain before reporting exit, matching the Rust
		// reader-join-before-exit ordering.
		readers.Wait()
		err := cmd.Wait()
		closeDone()
		code := exitCodeFromErr(err)
		p.setExit(code)
		exitCh <- code
		close(exitCh)
	}()

	return &SpawnedProcess{
		Process: p,
		Stdout:  stdoutCh,
		Stderr:  stderrCh,
		Exit:    exitCh,
	}, nil
}
