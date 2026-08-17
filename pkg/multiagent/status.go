package multiagent

import "github.com/sqlrush/codexgo/internal/protocol"

// StatusFromEvent derives the next agent status from a single emitted event,
// returning (status, true) when the event affects status tracking and
// (zero, false) otherwise. It is a faithful port of the Rust
// `agent::status::agent_status_from_event`.
func StatusFromEvent(msg protocol.EventMsg) (protocol.AgentStatus, bool) {
	switch msg.Type {
	case protocol.EventMsgKindTurnStarted:
		return protocol.AgentStatus{Kind: protocol.AgentStatusRunning}, true
	case protocol.EventMsgKindTurnComplete:
		var last *string
		if msg.TurnComplete != nil {
			last = msg.TurnComplete.LastAgentMessage
		}
		return protocol.AgentStatus{Kind: protocol.AgentStatusCompleted, CompletedMessage: last}, true
	case protocol.EventMsgKindTurnAborted:
		reason := protocol.TurnAbortReasonInterrupted
		if msg.TurnAborted != nil {
			reason = msg.TurnAborted.Reason
		}
		switch reason {
		case protocol.TurnAbortReasonInterrupted, protocol.TurnAbortReasonBudgetLimited:
			return protocol.AgentStatus{Kind: protocol.AgentStatusInterrupted}, true
		default:
			return protocol.AgentStatus{Kind: protocol.AgentStatusErrored, ErroredMessage: string(reason)}, true
		}
	case protocol.EventMsgKindError:
		var m string
		if msg.Error != nil {
			m = msg.Error.Message
		}
		return protocol.AgentStatus{Kind: protocol.AgentStatusErrored, ErroredMessage: m}, true
	case protocol.EventMsgKindShutdownComplete:
		return protocol.AgentStatus{Kind: protocol.AgentStatusShutdown}, true
	default:
		return protocol.AgentStatus{}, false
	}
}

// IsFinal reports whether status is terminal (no further transitions expected).
// It is a faithful port of the Rust `agent::status::is_final`: every status
// except PendingInit, Running, and Interrupted is final.
func IsFinal(status protocol.AgentStatus) bool {
	switch status.Kind {
	case protocol.AgentStatusPendingInit, protocol.AgentStatusRunning, protocol.AgentStatusInterrupted:
		return false
	default:
		return true
	}
}
