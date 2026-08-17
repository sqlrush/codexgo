package realtimeconv

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ConversationEnd classifies why a realtime conversation ended. It mirrors the
// Rust RealtimeConversationEnd enum and selects the close-reason string.
type ConversationEnd int

const (
	// EndRequested means the client explicitly closed the conversation.
	EndRequested ConversationEnd = iota
	// EndTransportClosed means the transport ended without an explicit close.
	EndTransportClosed
	// EndError means the conversation ended due to an error event.
	EndError
)

// reason renders the close-reason string for a ConversationEnd, matching the
// Rust send_realtime_conversation_closed mapping.
func (e ConversationEnd) reason() string {
	switch e {
	case EndRequested:
		return "requested"
	case EndTransportClosed:
		return "transport_closed"
	case EndError:
		return "error"
	default:
		return "transport_closed"
	}
}

// RealtimeEventMsg wraps a loop output Event as the protocol
// RealtimeConversationRealtime EventMsg, serializing the event into the raw
// payload. It mirrors the Rust EventMsg::RealtimeConversationRealtime(
// RealtimeConversationRealtimeEvent { payload }) construction in the fanout.
func RealtimeEventMsg(event Event) (protocol.EventMsg, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return protocol.EventMsg{}, fmt.Errorf("realtimeconv: encode realtime event: %w", err)
	}
	return protocol.EventMsg{
		Type: protocol.EventMsgKindRealtimeConversationRealtime,
		RealtimeConversationRealtime: &protocol.RealtimeConversationRealtimeEvent{
			Payload: json.RawMessage(payload),
		},
	}, nil
}

// StartedEventMsg builds the RealtimeConversationStarted EventMsg announced once
// the conversation has started. It mirrors the Rust
// EventMsg::RealtimeConversationStarted construction in handle_start_inner.
func StartedEventMsg(realtimeSessionID *string, version protocol.RealtimeConversationVersion) protocol.EventMsg {
	return protocol.EventMsg{
		Type: protocol.EventMsgKindRealtimeConversationStarted,
		RealtimeConversationStarted: &protocol.RealtimeConversationStartedEvent{
			RealtimeSessionID: cloneStringPtr(realtimeSessionID),
			Version:           version,
		},
	}
}

// SdpEventMsg builds the RealtimeConversationSdp EventMsg carrying an SDP answer
// for the WebRTC transport. It mirrors the Rust EventMsg::RealtimeConversationSdp
// construction in handle_start_inner.
func SdpEventMsg(sdp string) protocol.EventMsg {
	return protocol.EventMsg{
		Type:                    protocol.EventMsgKindRealtimeConversationSdp,
		RealtimeConversationSdp: &protocol.RealtimeConversationSdpEvent{SDP: sdp},
	}
}

// ClosedEventMsg builds the RealtimeConversationClosed EventMsg with the reason
// string for end. It mirrors the Rust send_realtime_conversation_closed.
func ClosedEventMsg(end ConversationEnd) protocol.EventMsg {
	reason := end.reason()
	return protocol.EventMsg{
		Type: protocol.EventMsgKindRealtimeConversationClosed,
		RealtimeConversationClosed: &protocol.RealtimeConversationClosedEvent{
			Reason: &reason,
		},
	}
}

// ErrorEventMsg builds the generic Error EventMsg used when realtime input
// fails while the session is not running. It mirrors the Rust
// send_conversation_error with a BadRequest CodexErrorInfo.
func ErrorEventMsg(message string) protocol.EventMsg {
	info := &protocol.CodexErrorInfo{Kind: protocol.CodexErrorInfoBadRequest}
	return protocol.EventMsg{
		Type: protocol.EventMsgKindError,
		Error: &protocol.ErrorEvent{
			Message:        message,
			CodexErrorInfo: info,
		},
	}
}
