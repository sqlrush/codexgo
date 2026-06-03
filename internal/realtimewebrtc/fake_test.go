package realtimewebrtc

import (
	"context"
	"errors"
	"sync"

	"github.com/pion/webrtc/v4"
)

// fakePeerConn is a test double implementing peerConn. It records calls and
// lets tests inject errors and connection state.
type fakePeerConn struct {
	mu sync.Mutex

	onTrack func(*webrtc.TrackRemote, *webrtc.RTPReceiver)

	createOfferErr      error
	setLocalErr         error
	setRemoteErr        error
	addTransceiverErr   error
	closeErr            error
	localDescriptionNil bool

	offerSDP string
	localSDP string

	state webrtc.PeerConnectionState
	stats webrtc.StatsReport

	setRemoteCalls []string
	closeCalls     int
}

func newFakePeerConn() *fakePeerConn {
	return &fakePeerConn{
		offerSDP: "v=0\r\no=- offer\r\n",
		localSDP: "v=0\r\no=- local-with-candidates\r\n",
		state:    webrtc.PeerConnectionStateConnected,
		stats:    webrtc.StatsReport{},
	}
}

func (f *fakePeerConn) OnTrack(fn func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onTrack = fn
}

func (f *fakePeerConn) AddTransceiverFromTrack(_ webrtc.TrackLocal, _ ...webrtc.RTPTransceiverInit) (*webrtc.RTPTransceiver, error) {
	return nil, f.addTransceiverErr
}

func (f *fakePeerConn) AddTransceiverFromKind(_ webrtc.RTPCodecType, _ ...webrtc.RTPTransceiverInit) (*webrtc.RTPTransceiver, error) {
	return nil, f.addTransceiverErr
}

func (f *fakePeerConn) CreateOffer(*webrtc.OfferOptions) (webrtc.SessionDescription, error) {
	if f.createOfferErr != nil {
		return webrtc.SessionDescription{}, f.createOfferErr
	}
	return webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: f.offerSDP}, nil
}

func (f *fakePeerConn) SetLocalDescription(webrtc.SessionDescription) error {
	return f.setLocalErr
}

func (f *fakePeerConn) SetRemoteDescription(d webrtc.SessionDescription) error {
	f.mu.Lock()
	f.setRemoteCalls = append(f.setRemoteCalls, d.SDP)
	f.mu.Unlock()
	return f.setRemoteErr
}

func (f *fakePeerConn) LocalDescription() *webrtc.SessionDescription {
	if f.localDescriptionNil {
		return nil
	}
	return &webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: f.localSDP}
}

func (f *fakePeerConn) ConnectionState() webrtc.PeerConnectionState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakePeerConn) setState(s webrtc.PeerConnectionState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
}

func (f *fakePeerConn) GetStats() webrtc.StatsReport {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats
}

func (f *fakePeerConn) Close() error {
	f.mu.Lock()
	f.closeCalls++
	f.mu.Unlock()
	return f.closeErr
}

func (f *fakePeerConn) remoteCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.setRemoteCalls...)
}

func (f *fakePeerConn) closes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCalls
}

// fakeFactory returns a peerConnFactory yielding the given fake and a gather
// function honoring the supplied behavior.
func fakeFactory(f *fakePeerConn, gatherErr error) peerConnFactory {
	return func() (peerConn, func(context.Context) error, error) {
		gather := func(ctx context.Context) error {
			if gatherErr != nil {
				return gatherErr
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		}
		return f, gather, nil
	}
}

// fakeBackend is a test AudioBackend.
type fakeBackend struct {
	mu sync.Mutex

	localTrackErr error
	localTrack    webrtc.TrackLocal
	peak          uint16
	peakOK        bool
	closed        bool
	remoteCalls   int
}

func (b *fakeBackend) LocalTrack() (webrtc.TrackLocal, error) {
	return b.localTrack, b.localTrackErr
}

func (b *fakeBackend) OnRemoteTrack(*webrtc.TrackRemote, *webrtc.RTPReceiver) {
	b.mu.Lock()
	b.remoteCalls++
	b.mu.Unlock()
}

func (b *fakeBackend) LocalAudioPeak() (uint16, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.peak, b.peakOK
}

func (b *fakeBackend) setPeak(p uint16) {
	b.mu.Lock()
	b.peak = p
	b.peakOK = true
	b.mu.Unlock()
}

func (b *fakeBackend) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
}

func (b *fakeBackend) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

var errBoom = errors.New("boom")
