//go:build !unix

package pty

import (
	"context"
	"fmt"
)

// ConPTYSupported reports whether a console PTY is available. On platforms
// without PTY support in this build it returns false.
func ConPTYSupported() bool { return false }

// SpawnPTY is unsupported on non-Unix platforms in this build. Mirrors the
// platform split in the Rust crate (which uses ConPTY on Windows); codexgo only
// targets the Unix PTY backend here.
func SpawnPTY(_ context.Context, _ string, _ []string, _ string, _ map[string]string, _ *string, _ TerminalSize) (*SpawnedProcess, error) {
	return nil, fmt.Errorf("pty: PTY spawn is not supported on this platform")
}
