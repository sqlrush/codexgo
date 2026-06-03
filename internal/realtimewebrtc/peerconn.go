package realtimewebrtc

import (
	"context"
	"fmt"

	"github.com/pion/webrtc/v4"
)

// peerConn is the minimal slice of *webrtc.PeerConnection the session worker
// depends on. Defining it here keeps the worker testable with a fake peer
// connection while still being satisfied by the concrete pion type.
type peerConn interface {
	OnTrack(func(*webrtc.TrackRemote, *webrtc.RTPReceiver))
	AddTransceiverFromTrack(track webrtc.TrackLocal, init ...webrtc.RTPTransceiverInit) (*webrtc.RTPTransceiver, error)
	AddTransceiverFromKind(kind webrtc.RTPCodecType, init ...webrtc.RTPTransceiverInit) (*webrtc.RTPTransceiver, error)
	CreateOffer(*webrtc.OfferOptions) (webrtc.SessionDescription, error)
	SetLocalDescription(webrtc.SessionDescription) error
	SetRemoteDescription(webrtc.SessionDescription) error
	LocalDescription() *webrtc.SessionDescription
	ConnectionState() webrtc.PeerConnectionState
	GetStats() webrtc.StatsReport
	Close() error
}

// peerConnFactory builds a peerConn together with a function that blocks until
// ICE gathering completes (used to inline candidates into the offer SDP). It is
// injectable so tests can supply a fake; production code uses newPionPeerConn.
type peerConnFactory func() (pc peerConn, gatherComplete func(context.Context) error, err error)

// newPionPeerConn constructs a real pion PeerConnection with a media engine that
// supports the default Opus audio codec.
func newPionPeerConn() (peerConn, func(context.Context) error, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, nil, fmt.Errorf("failed to register WebRTC codecs: %w", err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create WebRTC peer connection: %w", err)
	}

	gather := func(ctx context.Context) error {
		done := webrtc.GatheringCompletePromise(pc)
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return pc, gather, nil
}

// silentLocalTrack builds a placeholder Opus track used when no AudioBackend is
// supplied. It is never written to, so the session sends silence while keeping
// the audio transceiver in a sendrecv configuration that matches the reference.
func silentLocalTrack() (webrtc.TrackLocal, error) {
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"realtime-mic",
		"realtime",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create placeholder audio track: %w", err)
	}
	return track, nil
}
