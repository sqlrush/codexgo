package engine

import "context"

// CodeModeSessionDelegate mirrors codex's `CodeModeSessionDelegate` trait. It is
// the host-side bridge a code-mode session calls while cells execute: nested tool
// invocations, notifications, and cell-close lifecycle callbacks.
//
// In codex this trait carries a CancellationToken on each call; the Go port uses
// context.Context for the same purpose. Implementations must be safe for
// concurrent use because a single cell may have several in-flight nested calls.
type CodeModeSessionDelegate interface {
	// InvokeTool dispatches a nested tool call and returns the decoded JSON
	// response (any) on success or an error whose message is surfaced to the
	// cell as a rejected promise. Returning ctx.Err() is treated as a normal
	// cancellation by the cell control loop.
	InvokeTool(ctx context.Context, call CodeModeNestedToolCall) (any, error)

	// Notify delivers a notify() message emitted by a running cell. Errors are
	// logged by the session but never surfaced to the cell.
	Notify(ctx context.Context, callID string, cellID CellID, text string) error

	// CellClosed releases delegate state associated with a cell after it reaches
	// a terminal state. It must not block.
	CellClosed(cellID CellID)
}

// NoopCodeModeSessionDelegate mirrors codex's `NoopCodeModeSessionDelegate`. It
// rejects every nested tool call and silently accepts notifications, matching
// the default behavior when no host bridge is wired up.
type NoopCodeModeSessionDelegate struct{}

// InvokeTool always reports that nested tools are unavailable, after honoring
// cancellation (mirroring codex, which awaits the token before erroring).
func (NoopCodeModeSessionDelegate) InvokeTool(ctx context.Context, _ CodeModeNestedToolCall) (any, error) {
	<-ctx.Done()
	return nil, errCodeModeNestedToolsUnavailable
}

// Notify accepts and discards the notification.
func (NoopCodeModeSessionDelegate) Notify(context.Context, string, CellID, string) error {
	return nil
}

// CellClosed is a no-op.
func (NoopCodeModeSessionDelegate) CellClosed(CellID) {}

// errCodeModeNestedToolsUnavailable is the sentinel error the noop delegate
// returns. It is a value (not wrapped) so callers can match codex's exact text.
var errCodeModeNestedToolsUnavailable = errString("code mode nested tools are unavailable")

// errString is a tiny string-backed error type so package-level error sentinels
// can carry codex's verbatim messages without fmt overhead.
type errString string

func (e errString) Error() string { return string(e) }
