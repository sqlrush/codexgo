package engine

import (
	"fmt"

	"github.com/dop251/goja"

	"github.com/sqlrush/codexgo/internal/codemode"
)

// runtimeHandle is the host-facing channel bundle returned by spawnRuntime,
// mirroring codex's (runtime_tx, runtime_control_tx, isolate_handle) triple. The
// terminate func is the goja analog of v8::IsolateHandle::terminate_execution:
// it interrupts a CPU-bound runtime goroutine.
type runtimeHandle struct {
	commandCh chan<- runtimeCommand
	controlCh chan<- runtimeControlCommand
	terminate func()
}

// runtimeConfig mirrors codex's `RuntimeConfig`.
type runtimeConfig struct {
	toolCallID   string
	enabledTools []codemode.EnabledToolMetadata
	source       string
	storedValues map[string]any
}

// spawnRuntime starts a dedicated goroutine running a fresh goja runtime for one
// cell, mirroring codex's `spawn_runtime`. Events flow out on eventCh; commands
// (tool responses, timeout fires, terminate) flow in on the returned command
// channel; pause/resume controls flow in on the control channel.
//
// Deviation: codex spawns an OS thread carrying a V8 isolate and returns the
// isolate handle synchronously. goja runtimes are not thread-safe, so the port
// confines the runtime to its own goroutine and exposes termination via
// rt.Interrupt rather than an isolate handle.
func spawnRuntime(
	storedValues map[string]any,
	request ExecuteRequest,
	eventCh chan runtimeEvent,
	pendingMode pendingRuntimeMode,
) (runtimeHandle, error) {
	commandCh := make(chan runtimeCommand, 1024)
	controlCh := make(chan runtimeControlCommand, 16)

	enabled := make([]codemode.EnabledToolMetadata, 0, len(request.EnabledTools))
	for _, tool := range request.EnabledTools {
		enabled = append(enabled, codemode.EnabledToolMetadataOf(tool))
	}
	config := runtimeConfig{
		toolCallID:   request.ToolCallID,
		enabledTools: enabled,
		source:       request.Source,
		storedValues: storedValues,
	}

	rt := goja.New()
	handle := runtimeHandle{
		commandCh: commandCh,
		controlCh: controlCh,
		terminate: func() { rt.Interrupt("code mode terminated") },
	}

	go runRuntime(rt, config, eventCh, commandCh, controlCh, pendingMode)
	return handle, nil
}

// runRuntime is the cell's runtime goroutine, mirroring codex's `run_runtime`.
// It installs globals, evaluates the async-wrapped source, then loops draining
// commands and re-checking completion until a terminal state is reached.
func runRuntime(
	rt *goja.Runtime,
	config runtimeConfig,
	eventCh chan runtimeEvent,
	commandCh chan runtimeCommand,
	controlCh chan runtimeControlCommand,
	pendingMode pendingRuntimeMode,
) {
	// Closing the event channel on exit is the goja analog of codex's runtime
	// thread dropping its event_tx; it signals runtime_closed to the control loop.
	defer close(eventCh)
	state := newRuntimeState(rt, eventCh, commandCh, config.storedValues, config.enabledTools, config.toolCallID)
	defer func() {
		// Cancel any still-pending timers so their goroutines exit, then wait so
		// the runtime goroutine does not outlive its timers.
		for id, scheduled := range state.pendingTimeouts {
			close(scheduled.cancel)
			delete(state.pendingTimeouts, id)
		}
		state.timers.wait()
	}()

	if err := state.installGlobals(); err != nil {
		sendResult(eventCh, map[string]any{}, strPtrOf(err.Error()))
		return
	}

	eventCh <- runtimeEvent{Kind: runtimeEventStarted}

	pendingPromise, errText, terminated := evaluateMainModule(rt, state, config.source)
	if terminated {
		return
	}
	if errText != nil {
		captureAndSendError(state, eventCh, errText)
		return
	}

	switch cs := evaluateCompletion(state, pendingPromise); cs.Kind {
	case completionCompleted:
		sendResult(eventCh, cs.StoredValueWrites, cs.ErrorText)
		return
	case completionPending:
	}

	for {
		command, ok := nextRuntimeCommand(eventCh, commandCh, controlCh, pendingMode)
		if !ok {
			return
		}

		switch command.Kind {
		case runtimeCmdTerminate:
			return
		case runtimeCmdToolResponse:
			if err := resolveToolResponse(rt, state, command.ID, command.Result, nil); err != nil {
				captureAndSendError(state, eventCh, strPtrOf(err.Error()))
				return
			}
		case runtimeCmdToolError:
			if err := resolveToolResponse(rt, state, command.ID, nil, strPtrOf(command.ErrorText)); err != nil {
				captureAndSendError(state, eventCh, strPtrOf(err.Error()))
				return
			}
		case runtimeCmdTimeoutFired:
			if err := state.invokeTimeoutCallback(command.TimeoutID); err != nil {
				captureAndSendError(state, eventCh, strPtrOf(err.Error()))
				return
			}
		}

		// goja drains the microtask job queue at the end of each function call
		// (rt.leave()), so settling a promise above already ran its reactions.
		switch cs := evaluateCompletion(state, pendingPromise); cs.Kind {
		case completionCompleted:
			sendResult(eventCh, cs.StoredValueWrites, cs.ErrorText)
			return
		case completionPending:
		}

		if pendingPromise != nil && pendingPromise.State() != goja.PromiseStatePending {
			pendingPromise = nil
		}
	}
}

// nextRuntimeCommand mirrors codex's `next_runtime_command`. It drains a ready
// command, otherwise emits Pending and either blocks for the next command
// (Continue mode) or waits for a Resume/Terminate control (PauseUntilResumed).
func nextRuntimeCommand(
	eventCh chan<- runtimeEvent,
	commandCh chan runtimeCommand,
	controlCh chan runtimeControlCommand,
	pendingMode pendingRuntimeMode,
) (runtimeCommand, bool) {
	for {
		select {
		case command, ok := <-commandCh:
			if !ok {
				return runtimeCommand{}, false
			}
			return command, true
		default:
		}

		eventCh <- runtimeEvent{Kind: runtimeEventPending}

		switch pendingMode {
		case pendingModeContinue:
			command, ok := <-commandCh
			return command, ok
		case pendingModePauseUntilResumed:
			control, ok := <-controlCh
			if !ok {
				return runtimeCommand{}, false
			}
			switch control.Kind {
			case runtimeControlResume:
				continue
			case runtimeControlTerminate:
				return runtimeCommand{Kind: runtimeCmdTerminate}, true
			}
		}
	}
}

// evaluateMainModule compiles and runs the cell source, mirroring codex's
// `evaluate_main_module`. Because goja has no ES-module support, the source is
// wrapped in an async IIFE so top-level await behaves as it does in V8's module
// evaluation. The returned promise (when non-nil) is the module's completion
// promise. A true `terminated` return reports an interrupt (host termination).
func evaluateMainModule(rt *goja.Runtime, state *runtimeState, source string) (*goja.Promise, *string, bool) {
	value, err := rt.RunString(wrapAsyncModule(source))
	if err != nil {
		if isInterrupt(err) {
			return nil, nil, true
		}
		if isExitError(rt, state, err) {
			return nil, nil, false
		}
		return nil, strPtrOf(valueToErrorText(rt, err)), false
	}

	promise, ok := asPromise(value)
	if !ok {
		return nil, nil, false
	}
	return promise, nil, false
}

// evaluateCompletion mirrors codex's `completion_state`. It inspects the module
// completion promise and classifies the cell as Completed (resolved/rejected) or
// Pending, attaching the cell's accumulated stored-value writes.
func evaluateCompletion(state *runtimeState, promise *goja.Promise) completionState {
	writes := cloneStringMap(state.storedValueWrites)

	if promise == nil {
		return completionState{Kind: completionCompleted, StoredValueWrites: writes}
	}

	switch promise.State() {
	case goja.PromiseStatePending:
		return completionState{Kind: completionPending}
	case goja.PromiseStateFulfilled:
		return completionState{Kind: completionCompleted, StoredValueWrites: writes}
	case goja.PromiseStateRejected:
		result := promise.Result()
		var errText *string
		if !isExitValue(state, result) {
			errText = strPtrOf(rejectedValueErrorText(result))
		}
		return completionState{Kind: completionCompleted, StoredValueWrites: writes, ErrorText: errText}
	default:
		return completionState{Kind: completionCompleted, StoredValueWrites: writes}
	}
}

// resolveToolResponse mirrors codex's `resolve_tool_response`. Exactly one of
// result/errorText is meaningful: result resolves the pending promise, errorText
// rejects it. A returned error reports a runtime exception while settling.
func resolveToolResponse(rt *goja.Runtime, state *runtimeState, id string, result any, errorText *string) error {
	resolver, ok := state.pendingToolCalls[id]
	if !ok {
		return fmt.Errorf("unknown tool call %q", id)
	}
	delete(state.pendingToolCalls, id)

	if errorText != nil {
		if err := resolver.reject(rt.ToValue(*errorText)); err != nil {
			return fmt.Errorf("%s", valueToErrorText(rt, err))
		}
		return nil
	}

	value, err := jsonToJSValue(rt, result)
	if err != nil {
		return fmt.Errorf("failed to serialize tool response: %w", err)
	}
	if err := resolver.resolve(value); err != nil {
		return fmt.Errorf("%s", valueToErrorText(rt, err))
	}
	return nil
}

// captureAndSendError emits a Result event carrying the cell's stored-value
// writes plus the error text, mirroring codex's `capture_scope_send_error`.
func captureAndSendError(state *runtimeState, eventCh chan<- runtimeEvent, errText *string) {
	sendResult(eventCh, cloneStringMap(state.storedValueWrites), errText)
}

// sendResult emits the terminal Result event, mirroring codex's `send_result`.
func sendResult(eventCh chan<- runtimeEvent, writes map[string]any, errText *string) {
	eventCh <- runtimeEvent{Kind: runtimeEventResult, StoredValueWrites: writes, ErrorText: errText}
}

// wrapAsyncModule wraps the cell source in an async IIFE so that top-level await
// works under goja, which lacks ES-module evaluation. The result of the IIFE is
// the completion promise the engine polls.
func wrapAsyncModule(source string) string {
	return "(async () => {\n" + source + "\n})();"
}

// isInterrupt reports whether an error is a goja interrupt (host termination).
func isInterrupt(err error) bool {
	_, ok := err.(*goja.InterruptedError)
	return ok
}

// isExitError reports whether an error is the exit() sentinel thrown during
// synchronous evaluation, mirroring codex's `is_exit_exception`.
func isExitError(rt *goja.Runtime, state *runtimeState, err error) bool {
	if !state.exitRequested {
		return false
	}
	exception, ok := err.(*goja.Exception)
	if !ok {
		return false
	}
	return isExitValue(state, exception.Value())
}

// isExitValue reports whether a JS value is the exit sentinel string.
func isExitValue(state *runtimeState, value goja.Value) bool {
	if !state.exitRequested || value == nil {
		return false
	}
	str, ok := value.Export().(string)
	return ok && str == exitSentinel
}

// asPromise recovers a *goja.Promise from a goja value, if it is one.
func asPromise(value goja.Value) (*goja.Promise, bool) {
	if value == nil {
		return nil, false
	}
	promise, ok := value.Export().(*goja.Promise)
	return promise, ok
}

// rejectedValueErrorText extracts the model-facing error text from a rejected
// promise result, mirroring codex's `value_to_error_text` (prefer .stack on error
// objects, otherwise the value's string form).
func rejectedValueErrorText(value goja.Value) string {
	if value == nil {
		return "unknown code mode exception"
	}
	if object, ok := value.(*goja.Object); ok {
		if stack := object.Get("stack"); stack != nil && isStringValue(stack) {
			return stack.String()
		}
	}
	return value.String()
}

// strPtrOf returns a pointer to s.
func strPtrOf(s string) *string { return &s }

// cloneStringMap returns a shallow copy of m, never nil.
func cloneStringMap(m map[string]any) map[string]any {
	clone := make(map[string]any, len(m))
	for k, v := range m {
		clone[k] = v
	}
	return clone
}
