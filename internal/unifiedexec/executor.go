package unifiedexec

import "context"

// Executor is the tool-executor facade over a [ProcessManager]: it exposes the
// four operations a unified-exec tool call needs (open a session, write stdin /
// poll, resize, kill) plus session-id allocation and shutdown. It is the
// analogue of the surface the core layer drives on UnifiedExecProcessManager,
// minus the approval/event orchestration which lives outside this package.
//
// Executor is safe for concurrent use; it delegates to the manager, which holds
// its own lock.
type Executor struct {
	manager *ProcessManager
}

// NewExecutor builds an Executor over manager. When manager is nil a default
// manager (sandbox spawner, default background timeout) is used.
func NewExecutor(manager *ProcessManager) *Executor {
	if manager == nil {
		manager = NewDefaultProcessManager()
	}
	return &Executor{manager: manager}
}

// Manager returns the underlying process manager.
func (e *Executor) Manager() *ProcessManager { return e.manager }

// ExecCommand opens a new session for req. When req.ProcessID is zero a fresh
// logical id is allocated; otherwise the caller-reserved id is used. The
// returned Output reports whether the session persisted (non-nil ProcessID) or
// exited. The context cancels the spawn.
func (e *Executor) ExecCommand(ctx context.Context, req *ExecCommandRequest) (*Output, error) {
	if req == nil {
		return nil, newMissingCommandLineError()
	}
	r := *req
	if r.ProcessID == 0 {
		r.ProcessID = e.manager.AllocateProcessID()
	}
	return e.manager.ExecCommand(ctx, &r)
}

// WriteStdin writes input to a live session (empty input polls for output).
// The context cancels the write.
func (e *Executor) WriteStdin(ctx context.Context, req *WriteStdinRequest) (*Output, error) {
	if req == nil {
		return nil, newUnknownProcessIDError(0)
	}
	return e.manager.WriteStdin(ctx, req)
}

// Resize resizes the PTY of a live interactive session.
func (e *Executor) Resize(req *ResizeRequest) error {
	if req == nil {
		return newUnknownProcessIDError(0)
	}
	return e.manager.Resize(req)
}

// Kill terminates and removes a live session.
func (e *Executor) Kill(processID int) error {
	return e.manager.Kill(processID)
}

// Shutdown tears down every live session.
func (e *Executor) Shutdown() {
	e.manager.TerminateAllProcesses()
}
