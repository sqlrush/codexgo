package hooks

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// Engine holds the resolved set of command hook handlers plus the shell used to
// run them, and dispatches lifecycle events to those handlers. It mirrors the
// run/preview surface of ClaudeHooksEngine.
//
// Output spilling and handler discovery (config layer stacks, plugins, managed
// requirements) live outside this package's allowed dependency set and are
// therefore not part of the Engine; callers construct an Engine from a
// pre-resolved handler list (see NewEngine).
type Engine struct {
	handlers []ConfiguredHandler
	shell    CommandShell
}

// NewEngine builds an Engine from resolved handlers and a shell. The handler
// slice is copied so later mutation by the caller cannot affect the engine.
func NewEngine(handlers []ConfiguredHandler, shell CommandShell) *Engine {
	copied := make([]ConfiguredHandler, len(handlers))
	copy(copied, handlers)
	return &Engine{handlers: copied, shell: shell}
}

// Handlers returns a copy of the engine's configured handlers.
func (e *Engine) Handlers() []ConfiguredHandler {
	out := make([]ConfiguredHandler, len(e.handlers))
	copy(out, e.handlers)
	return out
}

// PreviewSessionStart lists the handlers that would run for a SessionStart.
func (e *Engine) PreviewSessionStart(request *SessionStartRequest) []protocol.HookRunSummary {
	return previewSessionStart(e.handlers, request)
}

// PreviewPreToolUse lists the handlers that would run for a PreToolUse.
func (e *Engine) PreviewPreToolUse(request *PreToolUseRequest) []protocol.HookRunSummary {
	return previewPreToolUse(e.handlers, request)
}

// PreviewPermissionRequest lists the handlers that would run for a
// PermissionRequest.
func (e *Engine) PreviewPermissionRequest(request *PermissionRequestRequest) []protocol.HookRunSummary {
	return previewPermissionRequest(e.handlers, request)
}

// PreviewPostToolUse lists the handlers that would run for a PostToolUse.
func (e *Engine) PreviewPostToolUse(request *PostToolUseRequest) []protocol.HookRunSummary {
	return previewPostToolUse(e.handlers, request)
}

// PreviewPreCompact lists the handlers that would run for a PreCompact.
func (e *Engine) PreviewPreCompact(request *PreCompactRequest) []protocol.HookRunSummary {
	return previewPreCompact(e.handlers, request)
}

// PreviewPostCompact lists the handlers that would run for a PostCompact.
func (e *Engine) PreviewPostCompact(request *PostCompactRequest) []protocol.HookRunSummary {
	return previewPostCompact(e.handlers, request)
}

// PreviewUserPromptSubmit lists the handlers that would run for a
// UserPromptSubmit.
func (e *Engine) PreviewUserPromptSubmit(request *UserPromptSubmitRequest) []protocol.HookRunSummary {
	return previewUserPromptSubmit(e.handlers, request)
}

// PreviewStop lists the handlers that would run for a Stop/SubagentStop.
func (e *Engine) PreviewStop(request *StopRequest) []protocol.HookRunSummary {
	return previewStop(e.handlers, request)
}

// RunSessionStart dispatches a SessionStart/SubagentStart event.
func (e *Engine) RunSessionStart(ctx context.Context, request SessionStartRequest, turnID *string) SessionStartOutcome {
	return runSessionStart(ctx, e.handlers, e.shell, request, turnID)
}

// RunPreToolUse dispatches a PreToolUse event.
func (e *Engine) RunPreToolUse(ctx context.Context, request PreToolUseRequest) PreToolUseOutcome {
	return runPreToolUse(ctx, e.handlers, e.shell, request)
}

// RunPermissionRequest dispatches a PermissionRequest event.
func (e *Engine) RunPermissionRequest(ctx context.Context, request PermissionRequestRequest) PermissionRequestOutcome {
	return runPermissionRequest(ctx, e.handlers, e.shell, request)
}

// RunPostToolUse dispatches a PostToolUse event.
func (e *Engine) RunPostToolUse(ctx context.Context, request PostToolUseRequest) PostToolUseOutcome {
	return runPostToolUse(ctx, e.handlers, e.shell, request)
}

// RunPreCompact dispatches a PreCompact event.
func (e *Engine) RunPreCompact(ctx context.Context, request PreCompactRequest) PreCompactOutcome {
	return runPreCompact(ctx, e.handlers, e.shell, request)
}

// RunPostCompact dispatches a PostCompact event.
func (e *Engine) RunPostCompact(ctx context.Context, request PostCompactRequest) StatelessHookOutcome {
	return runPostCompact(ctx, e.handlers, e.shell, request)
}

// RunUserPromptSubmit dispatches a UserPromptSubmit event.
func (e *Engine) RunUserPromptSubmit(ctx context.Context, request UserPromptSubmitRequest) UserPromptSubmitOutcome {
	return runUserPromptSubmit(ctx, e.handlers, e.shell, request)
}

// RunStop dispatches a Stop/SubagentStop event.
func (e *Engine) RunStop(ctx context.Context, request StopRequest) StopOutcome {
	return runStop(ctx, e.handlers, e.shell, request)
}
