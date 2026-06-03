package realtimewebrtc

import "errors"

// ErrUnsupportedPlatform mirrors the Rust RealtimeWebrtcError::UnsupportedPlatform
// variant. The Go port itself is cross-platform via pion, but the sentinel is
// retained for API parity and is returned when a session is used on a backend
// that cannot satisfy the request (for example after the worker has stopped and
// no platform audio backend is available).
var ErrUnsupportedPlatform = errors.New("realtime WebRTC is not supported on this platform")

// ErrWorkerStopped mirrors the Rust "realtime WebRTC worker stopped" message
// that the SessionHandle returns when the background worker is no longer
// running.
var ErrWorkerStopped = errors.New("realtime WebRTC worker stopped")
