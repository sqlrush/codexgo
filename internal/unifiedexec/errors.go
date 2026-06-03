package unifiedexec

import "fmt"

// ErrorKind classifies a [Error]. It mirrors the variants of the Rust
// UnifiedExecError enum.
type ErrorKind int

const (
	// ErrCreateProcess means the process could not be spawned.
	ErrCreateProcess ErrorKind = iota
	// ErrProcessFailed means a live process failed (e.g. network denial).
	ErrProcessFailed
	// ErrUnknownProcessID means no live session has the given process id.
	ErrUnknownProcessID
	// ErrWriteToStdin means a stdin write could not be delivered.
	ErrWriteToStdin
	// ErrStdinClosed means stdin is closed for a non-tty session.
	ErrStdinClosed
	// ErrMissingCommandLine means the request carried no command.
	ErrMissingCommandLine
	// ErrSandboxDenied means the command was denied by the sandbox.
	ErrSandboxDenied
)

// DeniedOutput carries the minimal exec output captured when a command is denied
// by the sandbox. The Rust error carries a full ExecToolCallOutput; this port
// keeps the load-bearing fields (exit code + aggregated text) so callers can
// surface a denial message without depending on core-private exec types.
type DeniedOutput struct {
	// ExitCode is the process exit code, or -1 when unknown.
	ExitCode int
	// AggregatedText is the captured combined output at the time of denial.
	AggregatedText string
}

// Error is the error type returned by the unified-exec subsystem. Mirrors
// UnifiedExecError; the Kind selects the variant and Message / ProcessID /
// Output carry variant-specific data.
type Error struct {
	Kind      ErrorKind
	Message   string
	ProcessID int
	Output    *DeniedOutput
}

// Error implements the error interface, mirroring the Rust Display impls.
func (e *Error) Error() string {
	switch e.Kind {
	case ErrCreateProcess:
		return fmt.Sprintf("Failed to create unified exec process: %s", e.Message)
	case ErrProcessFailed:
		return fmt.Sprintf("Unified exec process failed: %s", e.Message)
	case ErrUnknownProcessID:
		return fmt.Sprintf("Unknown process id %d", e.ProcessID)
	case ErrWriteToStdin:
		return "failed to write to stdin"
	case ErrStdinClosed:
		return "stdin is closed for this session; rerun exec_command with tty=true to keep stdin open"
	case ErrMissingCommandLine:
		return "missing command line for unified exec request"
	case ErrSandboxDenied:
		return fmt.Sprintf("Command denied by sandbox: %s", e.Message)
	default:
		return e.Message
	}
}

// newCreateProcessError builds an ErrCreateProcess error. Mirrors
// UnifiedExecError::create_process.
func newCreateProcessError(message string) *Error {
	return &Error{Kind: ErrCreateProcess, Message: message}
}

// newProcessFailedError builds an ErrProcessFailed error. Mirrors
// UnifiedExecError::process_failed.
func newProcessFailedError(message string) *Error {
	return &Error{Kind: ErrProcessFailed, Message: message}
}

// newUnknownProcessIDError builds an ErrUnknownProcessID error.
func newUnknownProcessIDError(processID int) *Error {
	return &Error{Kind: ErrUnknownProcessID, ProcessID: processID}
}

// newWriteToStdinError builds an ErrWriteToStdin error.
func newWriteToStdinError() *Error {
	return &Error{Kind: ErrWriteToStdin}
}

// newStdinClosedError builds an ErrStdinClosed error.
func newStdinClosedError() *Error {
	return &Error{Kind: ErrStdinClosed}
}

// newMissingCommandLineError builds an ErrMissingCommandLine error.
func newMissingCommandLineError() *Error {
	return &Error{Kind: ErrMissingCommandLine}
}

// newSandboxDeniedError builds an ErrSandboxDenied error. Mirrors
// UnifiedExecError::sandbox_denied.
func newSandboxDeniedError(message string, output DeniedOutput) *Error {
	return &Error{Kind: ErrSandboxDenied, Message: message, Output: &output}
}
