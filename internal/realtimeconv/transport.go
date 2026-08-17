package realtimeconv

import (
	"context"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// Writer sends outbound messages into a realtime session. It is the locally
// defined analogue of the Rust codex_api RealtimeWebsocketWriter; the Go port's
// internal/api does not expose the realtime websocket types, so this package
// depends on this small interface at the point of use and lets callers supply
// any conforming transport (WebSocket, the WebRTC sideband, or a test fake).
//
// All methods take a context so callers can bound or cancel a send. Each method
// mirrors a Rust RealtimeWebsocketWriter method one-to-one.
type Writer interface {
	// SendAudioFrame forwards a captured microphone frame to the server. Mirrors
	// RealtimeWebsocketWriter::send_audio_frame.
	SendAudioFrame(ctx context.Context, frame protocol.RealtimeAudioFrame) error
	// SendConversationItemCreate appends a user/text conversation item. Mirrors
	// RealtimeWebsocketWriter::send_conversation_item_create.
	SendConversationItemCreate(ctx context.Context, text string) error
	// SendConversationFunctionCallOutput resolves a function call (handoff or
	// noop) with output. Mirrors
	// RealtimeWebsocketWriter::send_conversation_function_call_output.
	SendConversationFunctionCallOutput(ctx context.Context, callID, outputText string) error
	// SendResponseCreate asks the server to begin a model response. Mirrors
	// RealtimeWebsocketWriter::send_response_create.
	SendResponseCreate(ctx context.Context) error
	// SendPayload sends a raw JSON payload (used for conversation.item.truncate).
	// Mirrors RealtimeWebsocketWriter::send_payload.
	SendPayload(ctx context.Context, payload string) error
}

// Events streams inbound realtime server events. It is the locally defined
// analogue of the Rust codex_api RealtimeWebsocketEvents.
type Events interface {
	// NextEvent blocks for the next server event. A nil Event with a nil error
	// signals end-of-stream (the Rust Ok(None)); a non-nil error signals a
	// transport failure (the Rust Err(ApiError)). NextEvent must honor ctx
	// cancellation.
	NextEvent(ctx context.Context) (*Event, error)
}

// Connection bundles the outbound Writer and inbound Events of an established
// realtime transport, mirroring the Rust RealtimeWebsocketConnection accessors
// writer() and events().
type Connection interface {
	Writer() Writer
	Events() Events
}
