//go:build darwin

package sandbox

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/pty"
)

// seatbeltBackend runs commands under macOS Seatbelt via /usr/bin/sandbox-exec.
// Mirrors the SandboxType::MacosSeatbelt arm of the transform/spawn path.
type seatbeltBackend struct{}

// newSeatbeltBackend constructs the macOS Seatbelt backend. It verifies the
// pinned sandbox-exec binary exists so callers fail fast on a misconfigured host.
func newSeatbeltBackend() (Backend, error) {
	if _, err := os.Stat(MacosPathToSeatbeltExecutable); err != nil {
		return nil, fmt.Errorf("sandbox: seatbelt executable %q unavailable: %w", MacosPathToSeatbeltExecutable, err)
	}
	return seatbeltBackend{}, nil
}

// Type reports SandboxTypeMacosSeatbelt.
func (seatbeltBackend) Type() SandboxType { return SandboxTypeMacosSeatbelt }

// Spawn generates the Seatbelt policy for req, writes it to a temp policy file,
// and runs the command via "/usr/bin/sandbox-exec -f <policyfile> -D... -- cmd".
// The temp policy file is removed once the child has exited.
func (seatbeltBackend) Spawn(ctx context.Context, req SpawnRequest) (*pty.SpawnedProcess, error) {
	if len(req.Command) == 0 {
		return nil, fmt.Errorf("sandbox: empty command")
	}

	policy, dirParams := buildSeatbeltPolicy(CreateSeatbeltCommandArgsParams{
		Command:                 req.Command,
		FileSystemSandboxPolicy: req.FileSystemSandboxPolicy,
		NetworkSandboxPolicy:    req.NetworkSandboxPolicy,
		SandboxPolicyCwd:        req.policyCwd(),
		EnforceManagedNetwork:   req.EnforceManagedNetwork,
		Network:                 req.Network,
		ExtraAllowUnixSockets:   req.ExtraAllowUnixSockets,
	})

	policyFile, cleanup, err := writePolicyFile(policy)
	if err != nil {
		return nil, err
	}

	// Build: sandbox-exec -f <policyfile> -D<key>=<path>... -- <command...>
	seatbeltArgs := make([]string, 0, 3+len(dirParams)+len(req.Command))
	seatbeltArgs = append(seatbeltArgs, "-f", policyFile)
	for _, param := range dirParams {
		seatbeltArgs = append(seatbeltArgs, "-D"+param.key+"="+param.path)
	}
	seatbeltArgs = append(seatbeltArgs, "--")
	seatbeltArgs = append(seatbeltArgs, req.Command...)

	fullCommand := make([]string, 0, 1+len(seatbeltArgs))
	fullCommand = append(fullCommand, MacosPathToSeatbeltExecutable)
	fullCommand = append(fullCommand, seatbeltArgs...)

	proc, err := spawnWithOutputMode(ctx, fullCommand, req.Cwd, req.Env, nil, req.Output, req.TerminalSize)
	if err != nil {
		cleanup()
		return nil, err
	}

	// Remove the policy file once the child exits; sandbox-exec reads it at
	// startup so it is safe to delete after launch, but waiting for exit keeps
	// behavior robust if the kernel re-reads it.
	go func() {
		<-proc.Exit
		cleanup()
	}()

	return proc, nil
}

// writePolicyFile writes the .sbpl policy text to a temp file and returns its
// path plus a cleanup func that removes it. The cleanup func is safe to call
// more than once.
func writePolicyFile(policy string) (string, func(), error) {
	file, err := os.CreateTemp("", "codex-seatbelt-*.sbpl")
	if err != nil {
		return "", nil, fmt.Errorf("sandbox: create seatbelt policy file: %w", err)
	}
	name := file.Name()
	cleanup := func() { _ = os.Remove(name) }

	if _, err := file.WriteString(policy); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("sandbox: write seatbelt policy file: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("sandbox: close seatbelt policy file: %w", err)
	}
	return name, cleanup, nil
}
