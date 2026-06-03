package realtimewebrtc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
)

// eventBuffer is the capacity of the buffered events channel handed back to the
// caller. The Rust port uses an unbounded std::sync::mpsc; a generous buffer
// keeps the worker non-blocking while bounding memory.
const eventBuffer = 64

// audioLevelInterval matches the Rust 200ms metering cadence.
const audioLevelInterval = 200 * time.Millisecond

// offerTimeout bounds how long Start waits for ICE gathering to complete before
// giving up on building the offer SDP.
const offerTimeout = 10 * time.Second

// Started is the Go analogue of Rust StartedRealtimeWebrtcSession. It bundles
// the generated offer SDP, the controlling handle, and the event stream.
type Started struct {
	// OfferSDP is the local SDP offer to forward to the realtime service. It
	// already contains gathered ICE candidates (non-trickle).
	OfferSDP string
	// Handle controls the running session.
	Handle *SessionHandle
	// Events streams session lifecycle events until the session closes.
	Events <-chan Event
}

// command is an internal control message sent to the worker goroutine.
type command struct {
	kind       commandKind
	answerSDP  string
	replyError chan error
}

type commandKind int

const (
	commandApplyAnswer commandKind = iota
	commandClose
)

// SessionHandle is the Go analogue of Rust RealtimeWebrtcSessionHandle. It is
// safe for concurrent use.
type SessionHandle struct {
	commands chan command

	// done is closed by the worker when it exits, so handle methods can detect
	// a stopped worker without blocking.
	done chan struct{}

	closeOnce sync.Once

	// localAudioPeak mirrors the Rust Arc<AtomicU16>: a shared, lock-free view
	// of the most recently reported microphone peak.
	localAudioPeak *atomic.Uint32
}

// ApplyAnswerSDP applies the remote answer SDP, completing negotiation. It
// blocks until the worker has processed the answer and returns the worker's
// result, mirroring the Rust apply_answer_sdp behaviour.
func (h *SessionHandle) ApplyAnswerSDP(answerSDP string) error {
	reply := make(chan error, 1)
	cmd := command{kind: commandApplyAnswer, answerSDP: answerSDP, replyError: reply}
	select {
	case h.commands <- cmd:
	case <-h.done:
		return fmt.Errorf("apply answer SDP: %w", ErrWorkerStopped)
	}
	select {
	case err, ok := <-reply:
		if !ok {
			return fmt.Errorf("apply answer SDP: %w", ErrWorkerStopped)
		}
		return err
	case <-h.done:
		return fmt.Errorf("apply answer SDP: %w", ErrWorkerStopped)
	}
}

// Close requests an orderly shutdown of the session. It is idempotent and
// non-blocking, matching the Rust close() which best-effort sends a Close
// command.
func (h *SessionHandle) Close() {
	h.closeOnce.Do(func() {
		select {
		case h.commands <- command{kind: commandClose}:
		case <-h.done:
		}
	})
}

// LocalAudioPeak returns the most recently reported local microphone peak in
// the range [0, 32767]. It mirrors the Rust local_audio_peak accessor, exposing
// the shared atomic value rather than a copy.
func (h *SessionHandle) LocalAudioPeak() uint16 {
	return uint16(h.localAudioPeak.Load())
}

// Option configures a Start call via functional options.
type Option func(*config)

type config struct {
	backend AudioBackend
	factory peerConnFactory
}

// WithAudioBackend supplies a pluggable audio backend for capture/playback and
// local level metering. When omitted the session uses a silent placeholder
// track and reports no LocalAudioLevel events.
func WithAudioBackend(backend AudioBackend) Option {
	return func(c *config) { c.backend = backend }
}

// withPeerConnFactory overrides the peer-connection factory; used in tests.
func withPeerConnFactory(factory peerConnFactory) Option {
	return func(c *config) { c.factory = factory }
}

// Start creates a WebRTC peer connection, generates an SDP offer (with gathered
// ICE candidates), and spawns a background worker that owns the connection for
// the remainder of the session. It is the Go analogue of
// RealtimeWebrtcSession::start.
//
// The supplied context bounds offer creation only; the worker runs until Close
// is called or the connection fails.
func Start(ctx context.Context, opts ...Option) (*Started, error) {
	cfg := config{factory: newPionPeerConn}
	for _, opt := range opts {
		opt(&cfg)
	}

	pc, gatherComplete, err := cfg.factory()
	if err != nil {
		return nil, fmt.Errorf("start realtime WebRTC session: %w", err)
	}

	offerSDP, err := negotiateOffer(ctx, pc, gatherComplete, cfg.backend)
	if err != nil {
		_ = pc.Close()
		if cfg.backend != nil {
			cfg.backend.Close()
		}
		return nil, fmt.Errorf("start realtime WebRTC session: %w", err)
	}

	handle := &SessionHandle{
		commands:       make(chan command, 1),
		done:           make(chan struct{}),
		localAudioPeak: new(atomic.Uint32),
	}
	events := make(chan Event, eventBuffer)

	w := &worker{
		pc:             pc,
		backend:        cfg.backend,
		commands:       handle.commands,
		events:         events,
		done:           handle.done,
		localAudioPeak: handle.localAudioPeak,
	}
	go w.run()

	return &Started{OfferSDP: offerSDP, Handle: handle, Events: events}, nil
}

// negotiateOffer wires up the audio transceiver, creates the offer, sets it as
// the local description, waits for ICE gathering, and returns the final SDP.
func negotiateOffer(
	ctx context.Context,
	pc peerConn,
	gatherComplete func(context.Context) error,
	backend AudioBackend,
) (string, error) {
	if err := attachAudio(pc, backend); err != nil {
		return "", err
	}

	offer, err := pc.CreateOffer(&webrtc.OfferOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create WebRTC offer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("failed to set local WebRTC description: %w", err)
	}

	gctx, cancel := context.WithTimeout(ctx, offerTimeout)
	defer cancel()
	if err := gatherComplete(gctx); err != nil {
		return "", fmt.Errorf("failed to gather ICE candidates: %w", err)
	}

	local := pc.LocalDescription()
	if local == nil {
		return "", errors.New("local WebRTC description unavailable after gathering")
	}
	return local.SDP, nil
}

// attachAudio registers the inbound-track handler and attaches the local audio
// track (from the backend or a silent placeholder) via a sendrecv transceiver.
func attachAudio(pc peerConn, backend AudioBackend) error {
	if backend != nil {
		pc.OnTrack(backend.OnRemoteTrack)
	}

	track, err := localTrack(backend)
	if err != nil {
		return err
	}

	if _, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendrecv,
	}); err != nil {
		return fmt.Errorf("failed to add audio transceiver: %w", err)
	}
	return nil
}

// localTrack resolves the local audio track from the backend, falling back to a
// silent placeholder when none is supplied.
func localTrack(backend AudioBackend) (webrtc.TrackLocal, error) {
	if backend != nil {
		track, err := backend.LocalTrack()
		if err != nil {
			return nil, fmt.Errorf("failed to obtain local audio track: %w", err)
		}
		if track != nil {
			return track, nil
		}
	}
	return silentLocalTrack()
}
