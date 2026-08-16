//go:build !unix

package pty

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/ptycap"
)

// ConPTYSupported reports whether a console PTY is available; see ptycap.
func ConPTYSupported() bool { return ptycap.ConPTYSupported() }

// SpawnPTY is unsupported on non-Unix platforms in this build. Mirrors the
// platform split in the Rust crate (which uses ConPTY on Windows); codexgo only
// targets the Unix PTY backend here.
func SpawnPTY(_ context.Context, _ string, _ []string, _ string, _ map[string]string, _ *string, _ TerminalSize) (*SpawnedProcess, error) {
	return nil, fmt.Errorf("pty: PTY spawn is not supported on this platform")
}
