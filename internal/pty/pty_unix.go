//go:build unix

package pty

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	creackpty "github.com/creack/pty"
)

// ConPTYSupported reports whether a console PTY is available. On Unix this is
// always true. Mirrors conpty_supported.
func ConPTYSupported() bool { return true }

// SpawnPTY spawns a process attached to a pseudo-terminal, returning a handle
// for stdin, combined output (delivered on Stdout; Stderr is closed empty since
// the child sees one terminal), and exit. Mirrors pty::spawn_process.
//
// The child is started as a new session leader with the PTY slave as its
// controlling terminal (handled by creack/pty), so its PID equals its PGID and
// Terminate hard-kills the whole group. The context cancels the spawn and is
// propagated to the child.
func SpawnPTY(ctx context.Context, program string, args []string, cwd string, env map[string]string, arg0 *string, size TerminalSize) (*SpawnedProcess, error) {
	if program == "" {
		return nil, fmt.Errorf("pty: missing program for PTY spawn")
	}

	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = cwd
	cmd.Env = envSlice(env)
	applyArg0(cmd, arg0)

	ws := &creackpty.Winsize{Rows: size.Rows, Cols: size.Cols}
	master, err := creackpty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, fmt.Errorf("pty: start %q: %w", program, err)
	}

	pid := cmd.Process.Pid

	stdoutCh := make(chan []byte, 128)
	stderrCh := make(chan []byte, 1)
	exitCh := make(chan int, 1)
	stdinCh := make(chan []byte, 128)

	// The child sees a single terminal for stdout/stderr; close stderr empty to
	// match codex's PTY backend, which uses a 1-capacity stderr channel that is
	// never fed.
	close(stderrCh)

	// done is closed once the child has exited so the stdin writer goroutine
	// stops blocking on the stdin channel and does not leak.
	done := make(chan struct{})

	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go readStream(master, stdoutCh, &readerWG)

	go func() {
		for {
			select {
			case chunk, ok := <-stdinCh:
				if !ok {
					return
				}
				if _, werr := master.Write(chunk); werr != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	resize := func(s TerminalSize) error {
		if rerr := creackpty.Setsize(master, &creackpty.Winsize{Rows: s.Rows, Cols: s.Cols}); rerr != nil {
			return fmt.Errorf("pty: resize: %w", rerr)
		}
		return nil
	}

	cleanup := func() {
		// Closing the master fd makes the reader and writer goroutines unblock.
		_ = master.Close()
	}

	p := newProcess(stdinCh, pgidTerminator{pgid: pid}, resize, cleanup)

	go func() {
		err := cmd.Wait()
		code := exitCodeFromErr(err)
		p.setExit(code)
		// Signal the writer goroutine to stop, then close the master so the
		// reader reaches EOF. Wait for the reader so Stdout is fully drained
		// before reporting exit, matching codex's reader-join ordering.
		close(done)
		_ = master.Close()
		readerWG.Wait()
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
