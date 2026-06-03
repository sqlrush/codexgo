package codemode

import (
	"context"
	"sync"
	"time"
)

// cellControlContext mirrors codex's `CellControlContext`.
type cellControlContext struct {
	cellID      CellID
	runtime     runtimeHandle
	pendingMode pendingRuntimeMode
}

// runCellControl is the per-cell control loop, mirroring codex's
// `run_cell_control`. It multiplexes runtime events, control commands, and the
// yield timer, accumulating content items and routing terminal/yield responses to
// whichever caller is currently waiting.
//
// Deviation from codex: codex uses tokio::select! over async channels with a
// JoinSet of notification tasks. The Go port uses a select loop over buffered
// channels plus a sync.WaitGroup for notification goroutines; the state machine
// (response routing, pending-result buffering, yield timer reset) is identical.
func (s *CodeModeService) runCellControl(
	ctx context.Context,
	cancel context.CancelFunc,
	cctx cellControlContext,
	eventCh <-chan runtimeEvent,
	controlCh <-chan cellControlCommand,
	initialResponse cellResponseSender,
	initialYieldMS *uint64,
	done chan struct{},
) {
	loop := &cellLoop{
		service:      s,
		ctx:          ctx,
		cancel:       cancel,
		cctx:         cctx,
		eventCh:      eventCh,
		controlCh:    controlCh,
		responseSink: initialResponse,
	}
	loop.run(initialYieldMS)

	// Teardown, mirroring codex's tail of run_cell_control.
	loop.send(cctx.runtime.commandCh, runtimeCommand{Kind: runtimeCmdTerminate})
	cancel()
	loop.notifications.Wait()
	if cctx.pendingMode == pendingModePauseUntilResumed {
		loop.sendControl(runtimeControlCommand{Kind: runtimeControlTerminate})
	}
	s.removeCell(cctx.cellID)
	s.delegate.CellClosed(cctx.cellID)
	_ = done // closed by removeCell
}

// cellLoop holds the mutable state of one cell's control loop. Keeping it in a
// struct avoids a 200-line function and lets helpers mutate shared state.
type cellLoop struct {
	service   *CodeModeService
	ctx       context.Context
	cancel    context.CancelFunc
	cctx      cellControlContext
	eventCh   <-chan runtimeEvent
	controlCh <-chan cellControlCommand

	contentItems       []FunctionCallOutputContentItem
	pendingResult      *pendingResult
	responseSink       cellResponseSender
	terminationReq     bool
	runtimeClosed      bool
	yieldTimer         *time.Timer
	yieldFired         <-chan time.Time
	notifications      sync.WaitGroup
	pendingToolCallIDs []string // reserved for execute-to-pending; unused in Execute/Wait
}

// pendingResult mirrors codex's `PendingResult`: a buffered terminal result held
// until a caller is waiting to receive it.
type pendingResult struct {
	contentItems []FunctionCallOutputContentItem
	errorText    *string
}

// run drives the select loop until a terminal response is delivered.
func (l *cellLoop) run(initialYieldMS *uint64) {
	for {
		yieldCh := l.yieldFired

		var eventSource <-chan runtimeEvent = l.eventCh
		if l.runtimeClosed {
			eventSource = nil
		}

		// Once termination has been requested the context is already cancelled;
		// disable the ctx.Done arm so the loop waits for the runtime to close its
		// event channel instead of busy-spinning on the closed Done channel.
		var ctxDone <-chan struct{} = l.ctx.Done()
		if l.terminationReq {
			ctxDone = nil
		}

		select {
		case event, ok := <-eventSource:
			if !ok {
				if l.handleRuntimeClosed() {
					return
				}
				continue
			}
			if l.handleEvent(event, initialYieldMS) {
				return
			}
		case command, ok := <-l.controlCh:
			if !ok {
				return
			}
			if l.handleControl(command) {
				return
			}
		case <-yieldCh:
			l.stopYieldTimer()
			l.sendYieldResponse()
		case <-ctxDone:
			// Shutdown/cancel: behave like a terminate command without a waiter.
			l.terminationReq = true
			l.cancel()
			l.stopYieldTimer()
			l.send(l.cctx.runtime.commandCh, runtimeCommand{Kind: runtimeCmdTerminate})
			if l.cctx.runtime.terminate != nil {
				l.cctx.runtime.terminate()
			}
			l.terminatePausedRuntime()
			if l.runtimeClosed {
				l.deliverTerminated()
				return
			}
		}
	}
}

// handleRuntimeClosed mirrors the maybe_event=None arm of codex's loop: the
// runtime's event channel closed.
func (l *cellLoop) handleRuntimeClosed() bool {
	l.runtimeClosed = true
	if l.terminationReq {
		if l.responseSink.isSet() {
			l.deliverTerminated()
		}
		return true
	}
	if l.pendingResult == nil {
		result := &pendingResult{
			contentItems: l.takeContentItems(),
			errorText:    strPtrOf("exec runtime ended unexpectedly"),
		}
		if l.sendOrBufferResult(result) {
			return true
		}
	}
	return false
}

// handleEvent processes one runtime event, returning true when the loop should
// exit (a terminal response was delivered).
func (l *cellLoop) handleEvent(event runtimeEvent, initialYieldMS *uint64) bool {
	switch event.Kind {
	case runtimeEventStarted:
		if initialYieldMS != nil {
			l.startYieldTimer(*initialYieldMS)
		}
	case runtimeEventPending:
		// In Execute/Wait (Continue mode) the runtime parks; nothing to route.
	case runtimeEventContentItem:
		l.contentItems = append(l.contentItems, event.ContentItem)
	case runtimeEventYieldRequested:
		l.stopYieldTimer()
		l.sendYieldResponse()
	case runtimeEventNotify:
		l.dispatchNotify(event.CallID, event.Text)
	case runtimeEventToolCall:
		l.dispatchToolCall(event)
	case runtimeEventResult:
		return l.handleResult(event)
	}
	return false
}

// handleResult processes a terminal Result event, mirroring codex's
// RuntimeEvent::Result arm.
func (l *cellLoop) handleResult(event runtimeEvent) bool {
	l.stopYieldTimer()
	if l.terminationReq {
		if l.responseSink.isSet() {
			l.deliverTerminated()
		}
		return true
	}
	l.notifications.Wait()
	l.service.extendStoredValues(event.StoredValueWrites)
	result := &pendingResult{contentItems: l.takeContentItems(), errorText: event.ErrorText}
	return l.sendOrBufferResult(result)
}

// handleControl processes one control command, returning true when the loop
// should exit.
func (l *cellLoop) handleControl(command cellControlCommand) bool {
	switch command.Kind {
	case cellControlPoll:
		if l.pendingResult != nil {
			command.responseCh <- l.pendingResultResponse(l.takePendingResult())
			return true
		}
		l.responseSink = cellResponseSender{runtimeCh: command.responseCh}
		l.startYieldTimer(command.YieldTimeMS)
		l.resumePausedRuntime()
	case cellControlTerminate:
		if l.pendingResult != nil {
			command.responseCh <- l.pendingResultResponse(l.takePendingResult())
			return true
		}
		l.responseSink = cellResponseSender{runtimeCh: command.responseCh}
		l.terminationReq = true
		l.cancel()
		l.stopYieldTimer()
		l.send(l.cctx.runtime.commandCh, runtimeCommand{Kind: runtimeCmdTerminate})
		l.terminatePausedRuntime()
		if l.cctx.runtime.terminate != nil {
			l.cctx.runtime.terminate()
		}
		if l.runtimeClosed {
			l.deliverTerminated()
			return true
		}
	}
	return false
}

// dispatchToolCall spawns a goroutine that invokes the host delegate for a nested
// tool call and posts the response/error back into the runtime, mirroring codex's
// RuntimeEvent::ToolCall arm.
func (l *cellLoop) dispatchToolCall(event runtimeEvent) {
	if l.cctx.pendingMode == pendingModePauseUntilResumed {
		l.pendingToolCallIDs = append(l.pendingToolCallIDs, event.ID)
	}
	call := CodeModeNestedToolCall{
		CellID:            l.cctx.cellID,
		RuntimeToolCallID: event.ID,
		ToolName:          event.ToolName,
		ToolKind:          event.ToolKind,
		Input:             event.Input,
	}
	id := event.ID
	commandCh := l.cctx.runtime.commandCh
	delegate := l.service.delegate
	ctx, cancel := context.WithCancel(l.ctx)
	go func() {
		defer cancel()
		result, err := delegate.InvokeTool(ctx, call)
		if ctx.Err() != nil {
			return
		}
		var command runtimeCommand
		if err != nil {
			command = runtimeCommand{Kind: runtimeCmdToolError, ID: id, ErrorText: err.Error()}
		} else {
			command = runtimeCommand{Kind: runtimeCmdToolResponse, ID: id, Result: result}
		}
		l.send(commandCh, command)
	}()
}

// dispatchNotify spawns a goroutine that delivers a notify() message via the host
// delegate, mirroring codex's RuntimeEvent::Notify arm.
func (l *cellLoop) dispatchNotify(callID, text string) {
	cellID := l.cctx.cellID
	delegate := l.service.delegate
	ctx, cancel := context.WithCancel(l.ctx)
	l.notifications.Add(1)
	go func() {
		defer l.notifications.Done()
		defer cancel()
		_ = delegate.Notify(ctx, callID, cellID, text)
	}()
}

// sendOrBufferResult delivers a terminal result to a waiting caller or buffers it
// for the next poll, mirroring codex's `send_or_buffer_result`. It returns true
// when the result was delivered (the loop should exit).
func (l *cellLoop) sendOrBufferResult(result *pendingResult) bool {
	if l.responseSink.isSet() {
		response := l.pendingResultResponse(result)
		sink := l.responseSink
		l.responseSink = cellResponseSender{}
		sink.send(response)
		return true
	}
	l.pendingResult = result
	return false
}

// sendYieldResponse delivers a Yielded response to a waiting caller, mirroring
// codex's `send_yield_response`. It clears the response sink and drains the
// accumulated content items.
func (l *cellLoop) sendYieldResponse() {
	if !l.responseSink.isSet() {
		return
	}
	sink := l.responseSink
	l.responseSink = cellResponseSender{}
	sink.send(newYieldedResponse(l.cctx.cellID, l.takeContentItems()))
}

// deliverTerminated sends a Terminated response to the current sink and clears it.
func (l *cellLoop) deliverTerminated() {
	if !l.responseSink.isSet() {
		return
	}
	sink := l.responseSink
	l.responseSink = cellResponseSender{}
	sink.send(newTerminatedResponse(l.cctx.cellID, l.takeContentItems()))
}

// pendingResultResponse mirrors codex's `pending_result_response`.
func (l *cellLoop) pendingResultResponse(result *pendingResult) RuntimeResponse {
	return newResultResponse(l.cctx.cellID, result.contentItems, result.errorText)
}

// takeContentItems drains and returns the accumulated content items.
func (l *cellLoop) takeContentItems() []FunctionCallOutputContentItem {
	items := l.contentItems
	l.contentItems = nil
	return items
}

// takePendingResult drains and returns the buffered pending result.
func (l *cellLoop) takePendingResult() *pendingResult {
	result := l.pendingResult
	l.pendingResult = nil
	return result
}

// startYieldTimer arms the yield timer for the given window, replacing any prior
// timer.
func (l *cellLoop) startYieldTimer(yieldTimeMS uint64) {
	l.stopYieldTimer()
	l.yieldTimer = time.NewTimer(time.Duration(yieldTimeMS) * time.Millisecond)
	l.yieldFired = l.yieldTimer.C
}

// stopYieldTimer disarms the yield timer.
func (l *cellLoop) stopYieldTimer() {
	if l.yieldTimer != nil {
		l.yieldTimer.Stop()
		l.yieldTimer = nil
		l.yieldFired = nil
	}
}

// resumePausedRuntime mirrors codex's `resume_paused_runtime`.
func (l *cellLoop) resumePausedRuntime() {
	if l.cctx.pendingMode == pendingModePauseUntilResumed {
		l.sendControl(runtimeControlCommand{Kind: runtimeControlResume})
	}
}

// terminatePausedRuntime mirrors codex's `terminate_paused_runtime`.
func (l *cellLoop) terminatePausedRuntime() {
	if l.cctx.pendingMode == pendingModePauseUntilResumed {
		l.sendControl(runtimeControlCommand{Kind: runtimeControlTerminate})
	}
}

// send forwards a runtime command, ignoring a closed channel (runtime already
// gone).
func (l *cellLoop) send(ch chan<- runtimeCommand, command runtimeCommand) {
	defer func() { _ = recover() }()
	ch <- command
}

// sendControl forwards a runtime control command, ignoring a closed channel.
func (l *cellLoop) sendControl(command runtimeControlCommand) {
	defer func() { _ = recover() }()
	l.cctx.runtime.controlCh <- command
}
