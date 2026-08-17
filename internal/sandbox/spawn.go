package sandbox

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/pty"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// SelectParams describes the policy inputs that decide which sandbox backend a
// command runs under, per the AskForApproval x SandboxPolicy matrix expressed by
// SandboxablePreference. Mirrors the inputs of SandboxManager::select_initial.
type SelectParams struct {
	// FileSystemSandboxPolicy is the resolved filesystem policy.
	FileSystemSandboxPolicy protocol.FileSystemSandboxPolicy
	// NetworkSandboxPolicy is the resolved network policy.
	NetworkSandboxPolicy protocol.NetworkSandboxPolicy
	// Preference expresses how strongly the command wants a platform sandbox.
	Preference SandboxablePreference
	// WindowsSandboxLevel selects the Windows sandbox enforcement level (only
	// consulted on Windows).
	WindowsSandboxLevel protocol.WindowsSandboxLevel
	// HasManagedNetworkRequirements forces a platform sandbox when true.
	HasManagedNetworkRequirements bool
}

// SelectBackendType selects the SandboxType for the given policy inputs using the
// existing AskForApproval x SandboxPolicy matrix. Mirrors
// SandboxManager::select_initial.
func SelectBackendType(params SelectParams) SandboxType {
	return selectInitial(
		params.FileSystemSandboxPolicy,
		params.NetworkSandboxPolicy,
		params.Preference,
		params.WindowsSandboxLevel,
		params.HasManagedNetworkRequirements,
	)
}

// SelectBackend selects and constructs the Backend for the given policy inputs.
// It returns the chosen SandboxType alongside the backend so callers can record
// the metric tag. When the selected platform sandbox is unavailable (e.g. the
// Linux/Windows backends tracked by separate specs), the error wraps
// ErrSandboxUnavailable.
func SelectBackend(params SelectParams) (Backend, SandboxType, error) {
	sandboxType := SelectBackendType(params)
	backend, err := NewBackend(sandboxType)
	if err != nil {
		return nil, sandboxType, err
	}
	return backend, sandboxType, nil
}

// Spawn selects the appropriate backend for the policy inputs in params and runs
// the command described by req under it. When the matrix resolves to no platform
// sandbox (Forbid, or a DangerFullAccess/disabled policy under Auto), the command
// runs unsandboxed. The resolved SandboxType is returned for observability.
//
// req carries the resolved policies that the chosen backend enforces; the policy
// fields in params are used only to select the backend. Callers typically pass
// the same FileSystemSandboxPolicy / NetworkSandboxPolicy in both.
func Spawn(ctx context.Context, params SelectParams, req SpawnRequest) (*pty.SpawnedProcess, SandboxType, error) {
	backend, sandboxType, err := SelectBackend(params)
	if err != nil {
		return nil, sandboxType, err
	}
	proc, err := backend.Spawn(ctx, req)
	if err != nil {
		return nil, sandboxType, fmt.Errorf("sandbox: spawn under %s backend: %w", sandboxType.AsMetricTag(), err)
	}
	return proc, sandboxType, nil
}
