// Package realtimewebrtc is a cross-platform Go port of codex-rs's
// realtime-webrtc crate. It drives the realtime/voice WebRTC session
// lifecycle: Start() produces an SDP offer, ApplyAnswerSDP applies the remote
// answer, and Close tears the session down. Throughout the session lifetime an
// event stream reports Connected, LocalAudioLevel, Closed and Failed.
//
// The reference crate targets macOS only and relies on libwebrtc's platform
// audio device module (AVAudioEngine) for microphone capture and speaker
// playback. This port uses github.com/pion/webrtc/v4, which is cross-platform
// but does NOT bundle a native audio backend. Microphone capture and speaker
// playback are therefore exposed as a pluggable AudioBackend interface; see the
// package-level documentation in session.go for details.
package realtimewebrtc

import "fmt"

// EventKind discriminates RealtimeWebrtcEvent values. It mirrors the Rust
// RealtimeWebrtcEvent enum variants.
type EventKind int

const (
	// EventKindConnected is emitted once the remote answer has been applied and
	// the peer connection negotiation has begun.
	EventKindConnected EventKind = iota
	// EventKindLocalAudioLevel reports the most recent local microphone peak,
	// scaled to the range [0, math.MaxInt16].
	EventKindLocalAudioLevel
	// EventKindClosed is emitted once the session has been torn down.
	EventKindClosed
	// EventKindFailed is emitted when the session fails; Message carries the
	// failure description.
	EventKindFailed
)

// String renders the EventKind for diagnostics.
func (k EventKind) String() string {
	switch k {
	case EventKindConnected:
		return "Connected"
	case EventKindLocalAudioLevel:
		return "LocalAudioLevel"
	case EventKindClosed:
		return "Closed"
	case EventKindFailed:
		return "Failed"
	default:
		return fmt.Sprintf("EventKind(%d)", int(k))
	}
}

// Event is the Go analogue of the Rust RealtimeWebrtcEvent enum. The active
// fields depend on Kind:
//
//   - EventKindConnected: no payload.
//   - EventKindLocalAudioLevel: AudioLevel holds the peak in [0, 32767].
//   - EventKindClosed: no payload.
//   - EventKindFailed: Message holds the failure description.
type Event struct {
	Kind       EventKind
	AudioLevel uint16
	Message    string
}

// Connected returns the Connected event.
func Connected() Event { return Event{Kind: EventKindConnected} }

// LocalAudioLevel returns a LocalAudioLevel event carrying peak.
func LocalAudioLevel(peak uint16) Event {
	return Event{Kind: EventKindLocalAudioLevel, AudioLevel: peak}
}

// Closed returns the Closed event.
func Closed() Event { return Event{Kind: EventKindClosed} }

// Failed returns a Failed event carrying message.
func Failed(message string) Event {
	return Event{Kind: EventKindFailed, Message: message}
}

// String renders an Event for diagnostics.
func (e Event) String() string {
	switch e.Kind {
	case EventKindLocalAudioLevel:
		return fmt.Sprintf("LocalAudioLevel(%d)", e.AudioLevel)
	case EventKindFailed:
		return fmt.Sprintf("Failed(%q)", e.Message)
	default:
		return e.Kind.String()
	}
}
