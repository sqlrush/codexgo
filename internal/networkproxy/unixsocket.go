package networkproxy

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// validatedUnixSocketPathKind distinguishes a natively-absolute path from a
// unix-style absolute path that is not absolute on the host platform (e.g. a
// "/tmp/x.sock" entry validated on Windows).
type validatedUnixSocketPathKind int

const (
	unixPathNative validatedUnixSocketPathKind = iota
	unixPathUnixStyleAbsolute
)

type validatedUnixSocketPath struct {
	kind   validatedUnixSocketPathKind
	native abspath.AbsolutePathBuf
	raw    string
}

// parseValidatedUnixSocketPath validates that a socket path is absolute (either
// natively or in unix-style), mirroring Rust's `ValidatedUnixSocketPath::parse`.
func parseValidatedUnixSocketPath(socketPath string) (validatedUnixSocketPath, error) {
	if filepath.IsAbs(socketPath) {
		native, err := abspath.FromAbsolutePath(socketPath)
		if err != nil {
			return validatedUnixSocketPath{}, fmt.Errorf("failed to normalize unix socket path %q: %w", socketPath, err)
		}
		return validatedUnixSocketPath{kind: unixPathNative, native: native, raw: socketPath}, nil
	}
	if strings.HasPrefix(socketPath, "/") {
		return validatedUnixSocketPath{kind: unixPathUnixStyleAbsolute, raw: socketPath}, nil
	}
	return validatedUnixSocketPath{}, fmt.Errorf("expected an absolute path, got %q", socketPath)
}

func validateUnixSocketAllowlistPaths(cfg NetworkProxyConfig) error {
	for index, socketPath := range cfg.Network.AllowUnixSockets() {
		if _, err := parseValidatedUnixSocketPath(socketPath); err != nil {
			return fmt.Errorf("invalid network.allow_unix_sockets[%d]: %w", index, err)
		}
	}
	return nil
}

// unixSocketPermissionsSupported reports whether the platform supports the
// `x-unix-socket` escape hatch (macOS only), mirroring Rust.
func unixSocketPermissionsSupported() bool {
	return runtime.GOOS == "darwin"
}
