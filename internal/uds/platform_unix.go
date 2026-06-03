//go:build unix

package uds

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
)

// socketDirMode is the owner-only permission mode applied to the socket
// directory. Owner-only access keeps the control socket directory private while
// preserving owner traversal and socket path creation. It mirrors the Rust
// constant SOCKET_DIR_MODE.
const socketDirMode fs.FileMode = 0o700

// socketDirPermissionBits is the mask of permission bits compared against
// socketDirMode when validating an existing directory. It mirrors the Rust
// constant SOCKET_DIR_PERMISSION_BITS (0o777).
const socketDirPermissionBits fs.FileMode = 0o777

// preparePrivateSocketDirectory mirrors the Rust unix
// prepare_private_socket_directory: create the directory with mode 0700, and if
// it already exists ensure it is a directory and force its permission bits to
// exactly 0700.
func preparePrivateSocketDirectory(socketDir string) error {
	switch err := mkdirExclusive(socketDir, socketDirMode); {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		// Fall through to validate and fix the existing directory.
	default:
		return err
	}

	// Use Lstat so a symlink at the path is not silently followed; this matches
	// the Rust use of symlink_metadata.
	info, err := os.Lstat(socketDir)
	if err != nil {
		return fmt.Errorf("stat existing socket directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf(
			"socket directory path exists and is not a directory: %s: %w",
			socketDir, fs.ErrExist,
		)
	}

	// The SSH-over-UDS control socket is reachable by path, so the rendezvous
	// directory must be owner-traversable while denying group/other access;
	// forcing exactly 0700 fixes insecure modes and unusable owner-only modes
	// like 0600.
	if info.Mode().Perm()&socketDirPermissionBits != socketDirMode {
		if err := os.Chmod(socketDir, socketDirMode); err != nil {
			return fmt.Errorf("set socket directory permissions: %w", err)
		}
	}
	return nil
}

// mkdirExclusive creates dir with the given mode, applying it exactly (without
// the influence of the process umask) so the resulting directory matches the
// Rust DirBuilder.mode behavior. It returns an error satisfying fs.ErrExist when
// the path already exists.
func mkdirExclusive(dir string, mode fs.FileMode) error {
	if err := os.Mkdir(dir, mode); err != nil {
		return err
	}
	// os.Mkdir applies the process umask, so re-apply the mode explicitly to
	// guarantee owner-only permissions regardless of umask.
	if err := os.Chmod(dir, mode); err != nil {
		return fmt.Errorf("set socket directory permissions: %w", err)
	}
	return nil
}

// bindListener mirrors the Rust unix bind_listener: bind a tokio UnixListener.
func bindListener(socketPath string) (*net.UnixListener, error) {
	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, err
	}
	return listener, nil
}

// connectStream mirrors the Rust unix connect_stream: connect a tokio
// UnixStream.
func connectStream(socketPath string) (*net.UnixConn, error) {
	addr := &net.UnixAddr{Name: socketPath, Net: "unix"}
	conn, err := net.DialUnix("unix", nil, addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// isStaleSocketPath mirrors the Rust unix is_stale_socket_path: inspect the file
// type via symlink metadata and report whether it is a socket.
func isStaleSocketPath(socketPath string) (bool, error) {
	info, err := os.Lstat(socketPath)
	if err != nil {
		return false, err
	}
	return info.Mode()&fs.ModeSocket != 0, nil
}
