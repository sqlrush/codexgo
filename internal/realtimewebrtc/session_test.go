package realtimewebrtc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// drainUntil collects events until the predicate is satisfied or the channel
// closes, returning all collected events.
func drainUntil(t *testing.T, events <-chan Event, pred func(Event) bool) []Event {
	t.Helper()
	var out []Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, ev)
			if pred(ev) {
				return out
			}
		case <-timeout:
			t.Fatalf("timed out waiting for event; collected %v", out)
		}
	}
}

func hasKind(events []Event, kind EventKind) bool {
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

func TestStartProducesOfferSDP(t *testing.T) {
	fake := newFakePeerConn()
	started, err := Start(context.Background(), withPeerConnFactory(fakeFactory(fake, nil)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer started.Handle.Close()

	if started.OfferSDP != fake.localSDP {
		t.Fatalf("OfferSDP = %q, want %q", started.OfferSDP, fake.localSDP)
	}
	if started.Handle == nil || started.Events == nil {
		t.Fatal("missing handle or events")
	}
}

func TestStartErrors(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*fakePeerConn)
		gatherErr error
		wantSub   string
	}{
		{
			name:    "create offer fails",
			mutate:  func(f *fakePeerConn) { f.createOfferErr = errBoom },
			wantSub: "failed to create WebRTC offer",
		},
		{
			name:    "set local fails",
			mutate:  func(f *fakePeerConn) { f.setLocalErr = errBoom },
			wantSub: "failed to set local WebRTC description",
		},
		{
			name:    "add transceiver fails",
			mutate:  func(f *fakePeerConn) { f.addTransceiverErr = errBoom },
			wantSub: "failed to add audio transceiver",
		},
		{
			name:      "gather fails",
			gatherErr: errBoom,
			wantSub:   "failed to gather ICE candidates",
		},
		{
			name:    "nil local description",
			mutate:  func(f *fakePeerConn) { f.localDescriptionNil = true },
			wantSub: "local WebRTC description unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakePeerConn()
			if tt.mutate != nil {
				tt.mutate(fake)
			}
			_, err := Start(context.Background(), withPeerConnFactory(fakeFactory(fake, tt.gatherErr)))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantSub)
			}
			if fake.closes() == 0 {
				t.Fatal("expected peer connection to be closed on failure")
			}
		})
	}
}

func TestApplyAnswerSDPEmitsConnected(t *testing.T) {
	fake := newFakePeerConn()
	started, err := Start(context.Background(), withPeerConnFactory(fakeFactory(fake, nil)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := started.Handle.ApplyAnswerSDP("the-answer"); err != nil {
		t.Fatalf("ApplyAnswerSDP: %v", err)
	}

	events := drainUntil(t, started.Events, func(ev Event) bool { return ev.Kind == EventKindConnected })
	if !hasKind(events, EventKindConnected) {
		t.Fatalf("expected Connected event, got %v", events)
	}

	if calls := fake.remoteCalls(); len(calls) != 1 || calls[0] != "the-answer" {
		t.Fatalf("SetRemoteDescription calls = %v", calls)
	}

	started.Handle.Close()
	rest := drainUntil(t, started.Events, func(ev Event) bool { return ev.Kind == EventKindClosed })
	if !hasKind(rest, EventKindClosed) {
		t.Fatalf("expected Closed event, got %v", rest)
	}
}

func TestApplyAnswerSDPErrorDoesNotConnect(t *testing.T) {
	fake := newFakePeerConn()
	fake.setRemoteErr = errBoom
	started, err := Start(context.Background(), withPeerConnFactory(fakeFactory(fake, nil)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer started.Handle.Close()

	err = started.Handle.ApplyAnswerSDP("bad-answer")
	if err == nil || !strings.Contains(err.Error(), "failed to set remote WebRTC description") {
		t.Fatalf("expected remote description error, got %v", err)
	}
}

func TestApplyAnswerAfterCloseReturnsWorkerStopped(t *testing.T) {
	fake := newFakePeerConn()
	started, err := Start(context.Background(), withPeerConnFactory(fakeFactory(fake, nil)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	started.Handle.Close()
	// Drain to completion so the worker exits.
	drainUntil(t, started.Events, func(ev Event) bool { return ev.Kind == EventKindClosed })
	// Allow the worker goroutine to fully exit (done channel close).
	deadline := time.After(2 * time.Second)
	for {
		err = started.Handle.ApplyAnswerSDP("late")
		if err != nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("expected worker stopped error")
		default:
		}
	}
	if !errors.Is(err, ErrWorkerStopped) {
		t.Fatalf("expected ErrWorkerStopped, got %v", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	fake := newFakePeerConn()
	started, err := Start(context.Background(), withPeerConnFactory(fakeFactory(fake, nil)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	started.Handle.Close()
	started.Handle.Close() // must not panic or block
	drainUntil(t, started.Events, func(ev Event) bool { return ev.Kind == EventKindClosed })
	if fake.closes() != 1 {
		t.Fatalf("expected exactly 1 close, got %d", fake.closes())
	}
}

func TestCloseWithoutAnswerEmitsClosed(t *testing.T) {
	fake := newFakePeerConn()
	started, err := Start(context.Background(), withPeerConnFactory(fakeFactory(fake, nil)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	started.Handle.Close()
	events := drainUntil(t, started.Events, func(ev Event) bool { return ev.Kind == EventKindClosed })
	if hasKind(events, EventKindConnected) {
		t.Fatalf("did not expect Connected without answer, got %v", events)
	}
	if !hasKind(events, EventKindClosed) {
		t.Fatalf("expected Closed, got %v", events)
	}
}

func TestBackendLifecycle(t *testing.T) {
	fake := newFakePeerConn()
	backend := &fakeBackend{}
	backend.setPeak(4242)

	started, err := Start(context.Background(),
		withPeerConnFactory(fakeFactory(fake, nil)),
		WithAudioBackend(backend),
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := started.Handle.ApplyAnswerSDP("answer"); err != nil {
		t.Fatalf("ApplyAnswerSDP: %v", err)
	}

	events := drainUntil(t, started.Events, func(ev Event) bool { return ev.Kind == EventKindLocalAudioLevel })
	var got Event
	for _, ev := range events {
		if ev.Kind == EventKindLocalAudioLevel {
			got = ev
		}
	}
	if got.AudioLevel != 4242 {
		t.Fatalf("audio level = %d, want 4242", got.AudioLevel)
	}
	if peak := started.Handle.LocalAudioPeak(); peak != 4242 {
		t.Fatalf("LocalAudioPeak = %d, want 4242", peak)
	}

	started.Handle.Close()
	drainUntil(t, started.Events, func(ev Event) bool { return ev.Kind == EventKindClosed })
	if !backend.isClosed() {
		t.Fatal("expected backend.Close to be called")
	}
}

func TestBackendLocalTrackError(t *testing.T) {
	fake := newFakePeerConn()
	backend := &fakeBackend{localTrackErr: errBoom}
	_, err := Start(context.Background(),
		withPeerConnFactory(fakeFactory(fake, nil)),
		WithAudioBackend(backend),
	)
	if err == nil || !strings.Contains(err.Error(), "failed to obtain local audio track") {
		t.Fatalf("expected local track error, got %v", err)
	}
	if !backend.isClosed() {
		t.Fatal("expected backend.Close on Start failure")
	}
}

func TestAudioPeakFromStats(t *testing.T) {
	report := webrtc.StatsReport{
		"src": webrtc.AudioSourceStats{Kind: "audio", AudioLevel: 1.0},
	}
	peak, ok := audioPeakFromStats(report)
	if !ok || peak != 32767 {
		t.Fatalf("audioPeakFromStats = %d, %v", peak, ok)
	}

	empty := webrtc.StatsReport{}
	if _, ok := audioPeakFromStats(empty); ok {
		t.Fatal("expected ok=false for empty stats")
	}
}

// TestStartWithRealPion exercises the real pion peer connection path end to end:
// generating an offer SDP with gathered ICE candidates. This validates the
// production factory wiring rather than the fakes.
func TestStartWithRealPion(t *testing.T) {
	started, err := Start(context.Background())
	if err != nil {
		t.Fatalf("Start (real pion): %v", err)
	}
	defer started.Handle.Close()

	if !strings.HasPrefix(started.OfferSDP, "v=0") {
		t.Fatalf("offer SDP does not look like SDP: %q", started.OfferSDP)
	}
	if !strings.Contains(started.OfferSDP, "m=audio") {
		t.Fatalf("offer SDP missing audio m-line: %q", started.OfferSDP)
	}
}
