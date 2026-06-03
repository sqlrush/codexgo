//go:build !unix

package uds

import (
	"errors"
	"io/fs"
	"net"
	"os"
)

// preparePrivateSocketDirectory mirrors the Rust non-unix
// prepare_private_socket_directory, which calls create_dir_all: it ensures the
// directory exists but does not apply Unix-style owner-only permissions, because
// the platform does not expose them.
func preparePrivateSocketDirectory(socketDir string) error {
	return os.MkdirAll(socketDir, 0o700)
}

// bindListener binds a Unix domain socket listener. It mirrors the Rust
// non-unix bind_listener, which binds the platform's path-addressed Unix socket
// listener.
func bindListener(socketPath string) (*net.UnixListener, error) {
	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	return net.ListenUnix("unix", addr)
}

// connectStream connects to a Unix domain socket. It mirrors the Rust non-unix
// connect_stream.
func connectStream(socketPath string) (*net.UnixConn, error) {
	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	return net.DialUnix("unix", nil, addr)
}

// isStaleSocketPath mirrors the Rust non-unix is_stale_socket_path, which uses
// try_exists: on platforms where the rendezvous is a regular path, existence is
// the only available stale-path signal.
//
// Unlike try_exists, which reports a missing path as false (Ok(false)), the
// public IsStaleSocketPath documents that a missing path yields an error from
// the underlying stat. To preserve the Rust "existence is the signal" contract
// here, a not-found result is reported as false with no error; any other stat
// error is surfaced.
func isStaleSocketPath(socketPath string) (bool, error) {
	if _, err := os.Lstat(socketPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
