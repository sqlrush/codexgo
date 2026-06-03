// Package uds ports the OpenAI Codex Rust crate `codex-rs/uds` (the
// `codex-uds` crate) to Go as part of a faithful, drop-in-compatible
// reimplementation of Codex 0.136.0.
//
// It provides cross-platform Unix domain socket helpers: a listener, a stream,
// a helper to create a private (owner-only) socket directory, and a helper to
// detect stale socket rendezvous paths.
//
// # Mapping from Rust to Go
//
// The Rust crate is built on tokio's asynchronous I/O traits (AsyncRead and
// AsyncWrite). Go's standard library models socket I/O synchronously over the
// runtime network poller, so the asynchronous trait implementations collapse
// into the standard [net.Conn] interface:
//
//   - Rust's `UnixStream` (implementing AsyncRead + AsyncWrite) becomes
//     [UnixStream], which satisfies [net.Conn]. The Rust trait methods
//     poll_read/poll_write/poll_flush/poll_shutdown map onto Go's blocking
//     Read/Write and Close (UnixStream has no separate flush because Go's
//     [net.UnixConn] is unbuffered).
//   - Rust's `UnixListener` becomes [UnixListener]; `accept` becomes [UnixListener.Accept].
//
// The Rust API accepts any `AsRef<Path>`; the Go API takes a plain path string,
// which is the idiomatic equivalent.
//
// # Immutability
//
// These helpers operate on the filesystem and on sockets, which are inherently
// stateful. No function mutates a caller-supplied value: paths are passed by
// value and the returned [UnixListener] and [UnixStream] own their underlying
// resources.
package uds

import (
	"fmt"
	"net"
	"time"
)

// PreparePrivateSocketDirectory creates socketDir if needed and, where the
// platform exposes Unix permissions, restricts it to the current user
// (mode 0700).
//
// It mirrors the Rust `prepare_private_socket_directory`: on Unix it creates the
// directory with mode 0700, and if it already exists verifies that it is a
// directory and forces its permission bits to exactly 0700. On platforms without
// Unix permissions it merely ensures the directory exists.
func PreparePrivateSocketDirectory(socketDir string) error {
	if err := preparePrivateSocketDirectory(socketDir); err != nil {
		return fmt.Errorf("uds: prepare private socket directory %q: %w", socketDir, err)
	}
	return nil
}

// IsStaleSocketPath reports whether socketPath points at a Unix socket
// rendezvous path.
//
// It mirrors the Rust `is_stale_socket_path`: on Unix it inspects the file type
// (following the symlink metadata semantics of lstat) and returns true only when
// the path is a socket. On platforms where the rendezvous is a regular path,
// existence is the only available signal.
//
// As in the Rust implementation, a path that does not exist yields an error
// rather than false; callers that want to treat a missing path as "not stale"
// should check for [fs.ErrNotExist] (via [errors.Is]) on the returned error.
func IsStaleSocketPath(socketPath string) (bool, error) {
	stale, err := isStaleSocketPath(socketPath)
	if err != nil {
		return false, fmt.Errorf("uds: stale socket check %q: %w", socketPath, err)
	}
	return stale, nil
}

// UnixListener is an asynchronous Unix domain socket listener.
//
// It corresponds to the Rust `UnixListener`. The zero value is not usable;
// obtain one with [Bind].
type UnixListener struct {
	inner *net.UnixListener
}

// Bind binds a new listener at socketPath.
//
// It corresponds to the Rust `UnixListener::bind`.
func Bind(socketPath string) (*UnixListener, error) {
	inner, err := bindListener(socketPath)
	if err != nil {
		return nil, fmt.Errorf("uds: bind %q: %w", socketPath, err)
	}
	return &UnixListener{inner: inner}, nil
}

// Accept accepts the next incoming stream.
//
// It corresponds to the Rust `UnixListener::accept` (which discards the peer
// address, as does this method).
func (l *UnixListener) Accept() (*UnixStream, error) {
	conn, err := l.inner.AcceptUnix()
	if err != nil {
		return nil, fmt.Errorf("uds: accept: %w", err)
	}
	return &UnixStream{inner: conn}, nil
}

// Addr returns the listener's network address.
func (l *UnixListener) Addr() net.Addr {
	return l.inner.Addr()
}

// Close releases the listener's resources.
//
// Go has no equivalent of Rust's automatic Drop, so callers must close the
// listener explicitly (typically via defer) to remove the bound socket path.
func (l *UnixListener) Close() error {
	if err := l.inner.Close(); err != nil {
		return fmt.Errorf("uds: close listener: %w", err)
	}
	return nil
}

// UnixStream is an asynchronous Unix domain socket stream.
//
// It corresponds to the Rust `UnixStream` and satisfies the [net.Conn]
// interface, which subsumes the Rust AsyncRead and AsyncWrite trait
// implementations. The zero value is not usable; obtain one with [Connect] or
// [UnixListener.Accept].
type UnixStream struct {
	inner *net.UnixConn
}

// Connect connects to socketPath.
//
// It corresponds to the Rust `UnixStream::connect`.
func Connect(socketPath string) (*UnixStream, error) {
	conn, err := connectStream(socketPath)
	if err != nil {
		return nil, fmt.Errorf("uds: connect %q: %w", socketPath, err)
	}
	return &UnixStream{inner: conn}, nil
}

// Read reads data from the stream into p. It implements [io.Reader] and the
// read half of [net.Conn].
func (s *UnixStream) Read(p []byte) (int, error) {
	return s.inner.Read(p)
}

// Write writes p to the stream. It implements [io.Writer] and the write half of
// [net.Conn].
func (s *UnixStream) Write(p []byte) (int, error) {
	return s.inner.Write(p)
}

// Close closes the stream. It corresponds to the Rust `poll_shutdown` followed
// by the implicit Drop: the underlying connection is fully shut down.
func (s *UnixStream) Close() error {
	return s.inner.Close()
}

// CloseWrite shuts down the writing side of the stream while leaving the read
// side open. It mirrors the Rust `poll_shutdown`, which performs a write-half
// shutdown (Shutdown::Write).
func (s *UnixStream) CloseWrite() error {
	return s.inner.CloseWrite()
}

// LocalAddr returns the local network address. It implements [net.Conn].
func (s *UnixStream) LocalAddr() net.Addr {
	return s.inner.LocalAddr()
}

// RemoteAddr returns the remote network address. It implements [net.Conn].
func (s *UnixStream) RemoteAddr() net.Addr {
	return s.inner.RemoteAddr()
}

// SetDeadline sets the read and write deadlines. It implements [net.Conn].
func (s *UnixStream) SetDeadline(t time.Time) error {
	return s.inner.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls. It implements [net.Conn].
func (s *UnixStream) SetReadDeadline(t time.Time) error {
	return s.inner.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls. It implements [net.Conn].
func (s *UnixStream) SetWriteDeadline(t time.Time) error {
	return s.inner.SetWriteDeadline(t)
}

// Compile-time assertion that UnixStream satisfies net.Conn, subsuming the Rust
// AsyncRead and AsyncWrite trait implementations.
//
// UnixListener deliberately does not implement net.Listener: like the Rust
// `UnixListener::accept`, its Accept method returns the concrete *UnixStream
// (mirroring "accept interfaces, return structs") rather than net.Conn.
var _ net.Conn = (*UnixStream)(nil)
