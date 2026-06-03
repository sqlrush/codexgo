package realtimewebrtc

import "github.com/pion/webrtc/v4"

// AudioBackend is a pluggable hook for platform audio capture and playback.
//
// The reference Rust crate relies on libwebrtc's platform audio device module
// (PeerConnectionFactory::with_platform_adm) to bind the system microphone and
// speaker automatically. pion/webrtc/v4 has no equivalent bundled audio device,
// so this port factors microphone capture, speaker playback and local audio
// level metering behind this interface.
//
// A nil backend is valid: the session negotiates a send/recv audio transceiver
// using a silent local track and reports no LocalAudioLevel events. This keeps
// the SDP offer/answer lifecycle fully functional for callers that wire their
// own audio (or that only exercise signalling), at the cost of not capturing or
// playing real audio out of the box.
//
// Implementations must be safe for concurrent use; the session calls
// LocalAudioPeak from a background metering goroutine while the caller may
// invoke other session methods.
type AudioBackend interface {
	// LocalTrack returns the local audio track to attach to the peer
	// connection's sender. Returning (nil, nil) is valid and instructs the
	// session to use its built-in silent placeholder track.
	LocalTrack() (webrtc.TrackLocal, error)

	// OnRemoteTrack is invoked for each inbound remote audio track so the
	// backend can route it to playback. It may be called more than once.
	OnRemoteTrack(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)

	// LocalAudioPeak returns the most recent microphone peak in the range
	// [0, 32767] together with ok=true when a fresh reading is available. When
	// ok is false the session emits no LocalAudioLevel event for that tick.
	LocalAudioPeak() (peak uint16, ok bool)

	// Close releases any audio resources held by the backend. It is invoked
	// exactly once when the session shuts down.
	Close()
}
