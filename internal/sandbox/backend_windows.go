//go:build windows

package sandbox

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/pty"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// windowsBackend runs commands under the native Windows sandbox: a restricted /
// low-integrity primary token plus deny-read ACLs on denied paths, with the
// command launched via CreateProcessAsUser. Mirrors the
// SandboxType::WindowsRestrictedToken arm of the transform/spawn path.
//
// Like the Linux backend, it wraps the command by re-executing the current
// binary with the NativeSandboxArgv0 sentinel; the helper (RunWindowsSandboxMain)
// applies the ACLs, builds the token, and launches the real command under it.
// The resolved policy is passed via NativeSandboxSpecEnv.
type windowsBackend struct {
	// selfExe is the absolute path of the current executable to re-exec as the
	// sandbox helper.
	selfExe string
	// level is the resolved Windows enforcement tier.
	level protocol.WindowsSandboxLevel
}

// newWindowsBackend constructs the Windows native sandbox backend. The
// enforcement tier defaults to restricted-token; callers that need a different
// tier set it on the SpawnRequest via the resolved policy (carried in the spec).
func newWindowsBackend() (Backend, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve current executable for windows sandbox helper: %w", err)
	}
	return windowsBackend{selfExe: exe, level: protocol.WindowsSandboxLevelRestrictedToken}, nil
}

// Type reports SandboxTypeWindowsRestrictedToken.
func (windowsBackend) Type() SandboxType { return SandboxTypeWindowsRestrictedToken }

// Spawn resolves req into a NativeSandboxSpec (with the Windows deny-read paths
// and enforcement tier), encodes it into the child environment, and runs the
// current binary as the sandbox helper.
func (b windowsBackend) Spawn(ctx context.Context, req SpawnRequest) (*pty.SpawnedProcess, error) {
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("sandbox: empty command")
	}

	spec := buildWindowsSandboxSpec(req, b.level)
	encoded, err := EncodeNativeSandboxSpec(spec)
	if err != nil {
		return nil, err
	}

	env := cloneEnv(req.Env)
	env[NativeSandboxSpecEnv] = encoded

	helperCommand := []string{b.selfExe, NativeSandboxArgv0}

	proc, err := spawnWithOutputMode(ctx, helperCommand, req.Cwd, env, nil, req.Output, req.TerminalSize)
	if err != nil {
		return nil, fmt.Errorf("sandbox: spawn windows sandbox helper: %w", err)
	}
	return proc, nil
}
