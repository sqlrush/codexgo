// Package pty ports codex's utils/pty crate: spawning child processes either
// attached to a pseudo-terminal (interactive) or wired to ordinary pipes
// (non-interactive), with a uniform handle for writing stdin, reading split
// stdout/stderr, resizing, and terminating.
//
// Faithful port of codex 0.136.0. The Rust crate is built on tokio + portable-pty;
// this port uses goroutines, channels, os/exec, and github.com/creack/pty. The
// observable contract matches codex: callers get a stdin writer channel, split
// stdout/stderr byte-chunk channels, and a one-shot exit-code channel.
package pty

import (
	"sync"
	"sync/atomic"
)

// DefaultOutputBytesCap mirrors DEFAULT_OUTPUT_BYTES_CAP in the Rust crate.
const DefaultOutputBytesCap = 1024 * 1024

// readBufferSize is the per-read chunk size used by the output readers,
// matching the 8 KiB buffer the Rust crate uses.
const readBufferSize = 8192

// TerminalSize is a terminal size in character cells. Mirrors the Rust
// TerminalSize struct, including its 24x80 default.
type TerminalSize struct {
	Rows uint16
	Cols uint16
}

// DefaultTerminalSize returns the default 24 rows by 80 columns size.
func DefaultTerminalSize() TerminalSize {
	return TerminalSize{Rows: 24, Cols: 80}
}

// terminator abstracts how a backend kills its child (process group for pipe and
// PTY backends). Mirrors the ChildTerminator trait.
type terminator interface {
	kill() error
}

// resizeFunc resizes the underlying PTY, or reports an error when the process is
// not attached to a resizable terminal.
type resizeFunc func(TerminalSize) error

// Process is a handle for driving an interactive or non-interactive child
// process. It is the analogue of the Rust ProcessHandle. All methods are safe
// for concurrent use.
type Process struct {
	// stdin carries bytes to write to the child. Closing it (via CloseStdin)
	// signals end-of-input to the writer goroutine.
	stdin chan []byte

	mu          sync.Mutex
	stdinClosed bool
	killer      terminator
	killed      bool
	resize      resizeFunc

	exited   atomic.Bool
	exitCode atomic.Int64
	hasCode  atomic.Bool

	// done is closed when all background goroutines have finished.
	wg sync.WaitGroup

	// cleanup runs on Terminate to release backend resources (close PTY fds,
	// stop reader/writer goroutines).
	cleanupOnce sync.Once
	cleanup     func()
}

// newProcess builds a Process from its backend pieces. stdin may be nil when the
// child's stdin is closed (no-stdin pipe mode).
func newProcess(stdin chan []byte, killer terminator, resize resizeFunc, cleanup func()) *Process {
	p := &Process{
		stdin:   stdin,
		killer:  killer,
		resize:  resize,
		cleanup: cleanup,
	}
	if stdin == nil {
		p.stdinClosed = true
	}
	return p
}

// Stdin returns the channel used to write raw bytes to the child's stdin. It
// returns nil when the child was spawned without a stdin pipe. Mirrors
// writer_sender on the Rust handle.
func (p *Process) Stdin() chan<- []byte {
	return p.stdin
}

// CloseStdin closes the child's stdin channel, signaling end-of-input. It is
// idempotent. Mirrors close_stdin.
func (p *Process) CloseStdin() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdinClosed {
		return
	}
	p.stdinClosed = true
	if p.stdin != nil {
		close(p.stdin)
	}
}

// HasExited reports whether the child process has exited. Mirrors has_exited.
func (p *Process) HasExited() bool {
	return p.exited.Load()
}

// ExitCode returns the child's exit code and whether it is known yet. Mirrors
// exit_code (which returns Option<i32>).
func (p *Process) ExitCode() (int, bool) {
	if !p.hasCode.Load() {
		return 0, false
	}
	return int(p.exitCode.Load()), true
}

// setExit records the final exit code and marks the process exited.
func (p *Process) setExit(code int) {
	p.exitCode.Store(int64(code))
	p.hasCode.Store(true)
	p.exited.Store(true)
}

// Resize resizes the PTY in character cells. It returns an error when the
// process is not attached to a resizable terminal (e.g. pipe mode). Mirrors
// resize.
func (p *Process) Resize(size TerminalSize) error {
	p.mu.Lock()
	resize := p.resize
	p.mu.Unlock()
	if resize == nil {
		return errNotPTY
	}
	return resize(size)
}

// RequestTerminate attempts to kill the child while leaving reader/writer
// goroutines alive so callers can drain remaining output until EOF. It is
// idempotent. Mirrors request_terminate.
func (p *Process) RequestTerminate() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.killed || p.killer == nil {
		return nil
	}
	p.killed = true
	return p.killer.kill()
}

// Terminate kills the child and releases backend resources (closing PTY fds and
// stopping helper goroutines). It is idempotent and is safe to call multiple
// times. Mirrors terminate.
func (p *Process) Terminate() {
	_ = p.RequestTerminate()
	p.cleanupOnce.Do(func() {
		if p.cleanup != nil {
			p.cleanup()
		}
	})
}
