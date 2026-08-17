package sandbox

import (
	"context"
	"errors"

	"github.com/sqlrush/codexgo/internal/networkproxy"
	"github.com/sqlrush/codexgo/internal/pty"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// OutputMode selects how a sandboxed command's stdio is wired.
type OutputMode int

const (
	// OutputModePipe wires stdin/stdout/stderr to ordinary pipes (the default,
	// non-interactive mode).
	OutputModePipe OutputMode = iota
	// OutputModePipeNoStdin wires stdout/stderr to pipes and closes stdin.
	OutputModePipeNoStdin
	// OutputModePTY runs the command attached to a pseudo-terminal (interactive
	// mode); stderr is folded into stdout, matching the pty package.
	OutputModePTY
)

// SpawnRequest is the fully-resolved description of a command to run under a
// sandbox backend. It is immutable from the backend's point of view: a backend
// reads it and produces a running process, never mutating the request.
//
// The resolved FileSystemSandboxPolicy / NetworkSandboxPolicy drive the platform
// sandbox policy. Cwd, Env and the command are applied to the spawned child.
type SpawnRequest struct {
	// Command is the program followed by its arguments.
	Command []string
	// Cwd is the working directory for the child process. It must be absolute.
	Cwd string
	// Env is the full environment for the child; the inherited environment is
	// replaced, not merged (matching codex env_clear semantics).
	Env map[string]string
	// FileSystemSandboxPolicy is the resolved filesystem policy.
	FileSystemSandboxPolicy protocol.FileSystemSandboxPolicy
	// NetworkSandboxPolicy is the resolved network policy.
	NetworkSandboxPolicy protocol.NetworkSandboxPolicy
	// SandboxPolicyCwd is the directory the policy paths are resolved against.
	// When empty, Cwd is used.
	SandboxPolicyCwd string
	// EnforceManagedNetwork forces a proxy-routed network policy, failing closed
	// when no usable proxy endpoints exist.
	EnforceManagedNetwork bool
	// Network is the running network proxy, if any.
	Network *networkproxy.NetworkProxy
	// ExtraAllowUnixSockets lists additional absolute unix-socket paths to permit.
	ExtraAllowUnixSockets []string
	// Output selects the stdio wiring.
	Output OutputMode
	// TerminalSize is used when Output is OutputModePTY; the zero value falls back
	// to the pty package default (24x80).
	TerminalSize pty.TerminalSize
	// Arg0, when non-nil, overrides argv[0] of the spawned process.
	Arg0 *string
}

// policyCwd returns the directory the policy should be resolved against,
// defaulting to the command's working directory.
func (r SpawnRequest) policyCwd() string {
	if r.SandboxPolicyCwd != "" {
		return r.SandboxPolicyCwd
	}
	return r.Cwd
}

// Backend runs a command under a particular platform sandbox (or none). It is a
// small interface so callers can depend on the abstraction rather than a
// concrete backend. Implementations must honor ctx for cancellation.
type Backend interface {
	// Type reports which SandboxType this backend implements.
	Type() SandboxType
	// Spawn launches the command described by req and returns a running process.
	// The returned process exposes split stdout/stderr channels and a one-shot
	// exit-code channel. ctx cancels the spawn and is propagated to the child.
	Spawn(ctx context.Context, req SpawnRequest) (*pty.SpawnedProcess, error)
}

// ErrSandboxUnavailable indicates the requested platform sandbox is not available
// or not implemented on the current platform. Mirrors the
// SandboxTransformError::SeatbeltUnavailable family.
var ErrSandboxUnavailable = errors.New("sandbox: requested platform sandbox is not available on this platform")

// NewBackend returns the Backend implementing the given sandbox type. It returns
// ErrSandboxUnavailable wrapped with the requested type when the platform does
// not provide that backend (e.g. Seatbelt off macOS, or the Linux/Windows
// backends that are tracked by separate specs).
func NewBackend(sandboxType SandboxType) (Backend, error) {
	switch sandboxType {
	case SandboxTypeNone:
		return noneBackend{}, nil
	case SandboxTypeMacosSeatbelt:
		return newSeatbeltBackend()
	case SandboxTypeLinuxSeccomp:
		return newLandlockBackend()
	case SandboxTypeWindowsRestrictedToken:
		return newWindowsBackend()
	default:
		return nil, notImplementedBackendError(sandboxType, "unknown sandbox type")
	}
}

// notImplementedBackendError wraps ErrSandboxUnavailable with a clear,
// backend-specific message.
func notImplementedBackendError(sandboxType SandboxType, detail string) error {
	return &backendUnavailableError{sandboxType: sandboxType, detail: detail}
}

// backendUnavailableError reports that a backend is not available, while
// unwrapping to ErrSandboxUnavailable so callers can match with errors.Is.
type backendUnavailableError struct {
	sandboxType SandboxType
	detail      string
}

func (e *backendUnavailableError) Error() string {
	return "sandbox: " + e.detail + " is not implemented (" + e.sandboxType.AsMetricTag() + ")"
}

func (e *backendUnavailableError) Unwrap() error { return ErrSandboxUnavailable }
