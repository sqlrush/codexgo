//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/pty"
)

// landlockBackend runs commands under the native Linux sandbox: user/mount/pid/
// (net) namespaces with a read-only root and writable binds, a Landlock
// filesystem ruleset, and a seccomp BPF network filter. Mirrors the
// SandboxType::LinuxSeccomp arm of the transform/spawn path.
//
// Like the macOS Seatbelt backend wraps the command with /usr/bin/sandbox-exec,
// this backend wraps the command by re-executing the current binary with the
// NativeSandboxArgv0 sentinel: that helper process (RunLinuxSandboxMain)
// establishes the sandbox natively and then exec's the real command. The fully
// resolved sandbox policy is passed to the helper via NativeSandboxSpecEnv, so
// the wrapped process needs no access to this package's policy model.
type landlockBackend struct {
	// selfExe is the absolute path of the current executable to re-exec as the
	// sandbox helper. Captured at construction so failures surface early.
	selfExe string
}

// newLandlockBackend constructs the Linux native sandbox backend. It resolves
// the current executable so the helper can be re-executed; a failure here means
// the host cannot support the self-wrapping sandbox and is reported clearly.
func newLandlockBackend() (Backend, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("sandbox: resolve current executable for linux sandbox helper: %w", err)
	}
	return landlockBackend{selfExe: exe}, nil
}

// Type reports SandboxTypeLinuxSeccomp.
func (landlockBackend) Type() SandboxType { return SandboxTypeLinuxSeccomp }

// Spawn resolves req into a NativeSandboxSpec, encodes it into the child
// environment, and runs the current binary as the sandbox helper (which applies
// the sandbox and exec's req.Command). The helper's working directory is req.Cwd
// and the helper itself chdir's there before exec, matching the unsandboxed path.
func (b landlockBackend) Spawn(ctx context.Context, req SpawnRequest) (*pty.SpawnedProcess, error) {
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("sandbox: empty command")
	}
	if procVersionIndicatesWSL1(readProcVersion()) {
		return nil, fmt.Errorf("sandbox: %w", errWSL1Unsupported)
	}

	spec := buildLinuxSandboxSpec(req)
	encoded, err := EncodeNativeSandboxSpec(spec)
	if err != nil {
		return nil, err
	}

	env := cloneEnv(req.Env)
	env[NativeSandboxSpecEnv] = encoded
	delete(env, nativeSandboxStageEnv)

	helperCommand := []string{b.selfExe, NativeSandboxArgv0}

	proc, err := spawnWithOutputMode(ctx, helperCommand, req.Cwd, env, nil, req.Output, req.TerminalSize)
	if err != nil {
		return nil, fmt.Errorf("sandbox: spawn linux sandbox helper: %w", err)
	}
	return proc, nil
}
