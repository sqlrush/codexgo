package appserver

import (
	"context"
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// handleTurnStart submits the user input to the addressed thread as a UserInput
// op, starting a model turn. The submission id is returned as the turn id,
// mirroring the Rust TurnRequestProcessor::turn_start which returns the
// submission id as the started turn's id.
func (p *Processor) handleTurnStart(_ context.Context, params *appserverproto.TurnStartParams) (any, *RPCError) {
	thread, rpcErr := p.loadThread(params.ThreadID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	op := protocol.Op{
		Type:  protocol.OpUserInput,
		Items: userInputsToCore(params.Input),
	}
	if len(params.OutputSchema) > 0 {
		op.FinalOutputJSONSchema = append(json.RawMessage(nil), params.OutputSchema...)
	}
	if len(params.ResponsesapiClientMeta) > 0 {
		op.ResponsesAPIClientMetadata = params.ResponsesapiClientMeta
	}

	turnID, err := thread.Submit(op)
	if err != nil {
		return nil, internalError("failed to start turn: %v", err)
	}

	turn := turnAggregate(turnID, appserverproto.TurnStatusInProgress)
	return appserverproto.TurnStartResponse{Turn: turn}, nil
}

// handleTurnInterrupt submits an Interrupt op to the addressed thread. For a turn
// interrupt (non-empty turn id) the JSON-RPC response is deferred: it is sent by
// the event forwarder when the matching TurnAborted event arrives, mirroring the
// Rust pattern where pending_interrupts are answered on abort. A startup
// interrupt (empty turn id) is acknowledged immediately.
func (p *Processor) handleTurnInterrupt(_ context.Context, requestID appserverproto.RequestId, params *appserverproto.TurnInterruptParams) (any, *RPCError) {
	thread, rpcErr := p.loadThread(params.ThreadID)
	if rpcErr != nil {
		return nil, rpcErr
	}

	isStartup := params.TurnID == ""
	if !isStartup {
		if f, ok := p.forwarderFor(params.ThreadID); ok {
			f.registerInterrupt(params.TurnID, requestID)
		}
	}

	if _, err := thread.Submit(protocol.Op{Type: protocol.OpInterrupt}); err != nil {
		// Unregister the pending interrupt so we do not leak a waiter.
		if !isStartup {
			if f, ok := p.forwarderFor(params.ThreadID); ok {
				f.unregisterInterrupt(params.TurnID, requestID)
			}
		}
		return nil, internalError("failed to interrupt turn: %v", err)
	}

	if isStartup {
		return appserverproto.TurnInterruptResponse{}, nil
	}
	// Defer: the forwarder answers when TurnAborted is observed.
	return nil, nil
}

// loadThread resolves a thread id string to its live [core.CodexThread],
// returning an invalid-request error when the thread is unknown.
func (p *Processor) loadThread(id string) (*core.CodexThread, *RPCError) {
	thread, err := p.assembly.ThreadManager.GetThread(protocol.NewThreadID(id))
	if err != nil {
		return nil, invalidRequest("thread not found: %s", id)
	}
	return thread, nil
}

// turnAggregate builds the `turn` aggregate (raw JSON) carried in the turn/start
// response. The full v2 Turn type is sibling-owned; here we emit the minimal,
// wire-shaped object clients rely on when a turn starts: the turn id, its status,
// and an empty items list.
func turnAggregate(turnID string, status appserverproto.TurnStatus) json.RawMessage {
	agg := map[string]any{
		"id":     turnID,
		"status": string(status),
		"items":  []any{},
	}
	raw, _ := json.Marshal(agg)
	return raw
}
