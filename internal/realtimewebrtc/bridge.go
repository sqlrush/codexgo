package realtimewebrtc

import (
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// OfferSdpEvent wraps a freshly generated offer SDP as the protocol
// RealtimeConversationSdp EventMsg. The realtime service expects the local offer
// to be surfaced over this variant so it can produce the matching answer.
func OfferSdpEvent(offerSDP string) protocol.EventMsg {
	return protocol.EventMsg{
		Type:                    protocol.EventMsgKindRealtimeConversationSdp,
		RealtimeConversationSdp: &protocol.RealtimeConversationSdpEvent{SDP: offerSDP},
	}
}

// EventToProtocol projects a session Event onto the corresponding protocol
// EventMsg variant. The boolean result is false for events that have no direct
// protocol projection (LocalAudioLevel, which is a UI-only signal in the Rust
// TUI and has no wire EventMsg), so callers can skip emitting them.
//
//   - EventKindConnected   -> RealtimeConversationStarted (version V2)
//   - EventKindClosed      -> RealtimeConversationClosed (no reason)
//   - EventKindFailed      -> RealtimeConversationClosed (reason = message)
//   - EventKindLocalAudioLevel -> (nil, false)
func EventToProtocol(ev Event) (protocol.EventMsg, bool) {
	switch ev.Kind {
	case EventKindConnected:
		return protocol.EventMsg{
			Type: protocol.EventMsgKindRealtimeConversationStarted,
			RealtimeConversationStarted: &protocol.RealtimeConversationStartedEvent{
				Version: protocol.RealtimeConversationVersionV2,
			},
		}, true
	case EventKindClosed:
		return protocol.EventMsg{
			Type:                       protocol.EventMsgKindRealtimeConversationClosed,
			RealtimeConversationClosed: &protocol.RealtimeConversationClosedEvent{},
		}, true
	case EventKindFailed:
		reason := ev.Message
		return protocol.EventMsg{
			Type: protocol.EventMsgKindRealtimeConversationClosed,
			RealtimeConversationClosed: &protocol.RealtimeConversationClosedEvent{
				Reason: &reason,
			},
		}, true
	default:
		return protocol.EventMsg{}, false
	}
}

// AnswerSDPFromOp extracts the remote answer SDP carried by a realtime Op. The
// answer is delivered via the webrtc transport on the start params. It returns
// an error when the op is not a realtime start carrying a webrtc transport.
func AnswerSDPFromOp(op protocol.Op) (string, error) {
	if op.Type != protocol.OpRealtimeConversationStart {
		return "", fmt.Errorf("op %q is not a realtime conversation start", op.Type)
	}
	params := op.RealtimeConversationStartParams
	if params == nil {
		return "", fmt.Errorf("realtime conversation start op missing params")
	}
	if params.Transport == nil {
		return "", fmt.Errorf("realtime conversation start op missing transport")
	}
	if params.Transport.Type != protocol.ConversationStartTransportWebrtc {
		return "", fmt.Errorf("realtime conversation transport %q is not webrtc", params.Transport.Type)
	}
	return params.Transport.SDP, nil
}
