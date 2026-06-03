package mcpserver

import (
	"context"
	"fmt"
	"sync"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// runningRequests tracks the mapping from MCP request id (string form) to the
// thread id servicing it, so cancellation notifications can locate the thread to
// interrupt. It is the Go analogue of running_requests_id_to_codex_uuid.
type runningRequests struct {
	mu      sync.Mutex
	byReqID map[string]string
}

func newRunningRequests() *runningRequests {
	return &runningRequests{byReqID: make(map[string]string)}
}

func (r *runningRequests) set(reqID, threadID string) {
	r.mu.Lock()
	r.byReqID[reqID] = threadID
	r.mu.Unlock()
}

func (r *runningRequests) get(reqID string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byReqID[reqID]
	return id, ok
}

func (r *runningRequests) remove(reqID string) {
	r.mu.Lock()
	delete(r.byReqID, reqID)
	r.mu.Unlock()
}

// runCodexToolSession starts a new thread, submits the initial prompt, streams
// events back to the client as codex/event notifications, and returns the
// tools/call response on completion. It is the faithful port of
// run_codex_tool_session.
func runCodexToolSession(
	ctx context.Context,
	manager *core.ThreadManager,
	sender *outgoingSender,
	running *runningRequests,
	reqID RequestID,
	prompt string,
	cfg core.SessionConfiguration,
) {
	newThread, err := manager.StartThread(ctx, cfg)
	if err != nil {
		sender.sendResponse(reqID, errorCallToolResult(fmt.Sprintf("Failed to start Codex session: %v", err)))
		return
	}

	threadID := newThread.ThreadID.String()
	reqIDStr := reqID.String()

	// Re-emit the SessionConfigured ack as the first codex/event. The thread
	// manager consumes the ack at spawn, so the streaming loop never sees it.
	configured := newThread.SessionConfigured
	tid := threadID
	sender.sendEventAsNotification(protocol.Event{
		ID: core.InitialSubmitID,
		Msg: protocol.EventMsg{
			Type:              protocol.EventMsgKindSessionConfigured,
			SessionConfigured: &configured,
		},
	}, &outgoingMeta{RequestID: &reqID, ThreadID: &tid})

	running.set(reqIDStr, threadID)

	// Use the MCP request id as the submission id so emitted events correlate.
	sub := protocol.Submission{
		ID: reqIDStr,
		Op: protocol.Op{
			Type:  protocol.OpUserInput,
			Items: []protocol.UserInput{{Type: protocol.UserInputKindText, Text: prompt}},
		},
	}
	if err := newThread.Thread.SubmitWithID(sub); err != nil {
		sender.sendResponse(reqID, callToolResultWithThreadID(threadID, fmt.Sprintf("Failed to submit initial prompt: %v", err), boolTrue()))
		running.remove(reqIDStr)
		return
	}

	streamThreadSession(ctx, newThread.Thread, sender, running, reqID, threadID)
}

// runCodexToolSessionReply continues an existing thread with a follow-up prompt,
// streaming events and returning the tools/call response. Faithful port of
// run_codex_tool_session_reply.
func runCodexToolSessionReply(
	ctx context.Context,
	thread *core.CodexThread,
	sender *outgoingSender,
	running *runningRequests,
	reqID RequestID,
	threadID string,
	prompt string,
) {
	reqIDStr := reqID.String()
	running.set(reqIDStr, threadID)

	if _, err := thread.Submit(protocol.Op{
		Type:  protocol.OpUserInput,
		Items: []protocol.UserInput{{Type: protocol.UserInputKindText, Text: prompt}},
	}); err != nil {
		sender.sendResponse(reqID, callToolResultWithThreadID(threadID, fmt.Sprintf("Failed to submit user input: %v", err), boolTrue()))
		running.remove(reqIDStr)
		return
	}

	streamThreadSession(ctx, thread, sender, running, reqID, threadID)
}

// streamThreadSession pumps the thread's event stream to the client until the
// turn completes, errors, or the loop terminates, handling approval requests
// inline. It is the faithful port of run_codex_tool_session_inner.
func streamThreadSession(
	ctx context.Context,
	thread *core.CodexThread,
	sender *outgoingSender,
	running *runningRequests,
	reqID RequestID,
	threadID string,
) {
	reqIDStr := reqID.String()
	tid := threadID

	for {
		ev, err := thread.NextEvent(ctx)
		if err != nil {
			sender.sendResponse(reqID, callToolResultWithThreadID(threadID, fmt.Sprintf("Codex runtime error: %v", err), boolTrue()))
			return
		}

		sender.sendEventAsNotification(ev, &outgoingMeta{RequestID: &reqID, ThreadID: &tid})

		switch ev.Msg.Type {
		case protocol.EventMsgKindExecApprovalRequest:
			if ev.Msg.ExecApprovalRequest != nil {
				handleExecApprovalRequest(sender, thread, threadID, reqIDStr, *ev.Msg.ExecApprovalRequest, ev.ID)
			}
		case protocol.EventMsgKindApplyPatchApprovalRequest:
			if ev.Msg.ApplyPatchApprovalRequest != nil {
				handlePatchApprovalRequest(sender, thread, threadID, reqIDStr, *ev.Msg.ApplyPatchApprovalRequest, ev.ID)
			}
		case protocol.EventMsgKindError:
			msg := ""
			if ev.Msg.Error != nil {
				msg = ev.Msg.Error.Message
			}
			sender.sendResponse(reqID, callToolResultWithThreadID(threadID, msg, boolTrue()))
			return
		case protocol.EventMsgKindTurnComplete:
			text := ""
			if ev.Msg.TurnComplete != nil && ev.Msg.TurnComplete.LastAgentMessage != nil {
				text = *ev.Msg.TurnComplete.LastAgentMessage
			}
			sender.sendResponse(reqID, callToolResultWithThreadID(threadID, text, nil))
			running.remove(reqIDStr)
			return
		case protocol.EventMsgKindShutdownComplete:
			return
		default:
			// All other events are forwarded as notifications above; no extra
			// handling is required in the tool runner.
		}
	}
}

// boolTrue returns a pointer to true, used for the is_error flag.
func boolTrue() *bool {
	v := true
	return &v
}
