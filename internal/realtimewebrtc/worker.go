package realtimewebrtc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
)

// worker owns the peer connection for the lifetime of a session and serializes
// all interaction with it. It mirrors the Rust worker_main loop: process
// commands, emit lifecycle events, and run the local audio level task once the
// answer has been applied.
type worker struct {
	pc      peerConn
	backend AudioBackend

	commands <-chan command
	events   chan<- Event
	done     chan<- struct{}

	localAudioPeak *atomic.Uint32
}

// run is the worker's main loop. It drains commands until Close (or channel
// close), then tears down the connection and emits the terminal Closed event.
// On exit the events channel is closed so consumers observe end-of-stream.
func (w *worker) run() {
	// Signal a stopped worker before closing the events channel so handle
	// methods unblock promptly.
	defer close(w.events)
	defer close(w.done)

	var meterStop context.CancelFunc
	var meterDone sync.WaitGroup
	connected := false

	stopMeter := func() {
		if meterStop != nil {
			meterStop()
			meterDone.Wait()
			meterStop = nil
		}
	}

	for cmd := range w.commands {
		switch cmd.kind {
		case commandApplyAnswer:
			err := w.applyAnswer(cmd.answerSDP)
			if err == nil && !connected {
				connected = true
				w.emit(Connected())
				ctx, cancel := context.WithCancel(context.Background())
				meterStop = cancel
				meterDone.Add(1)
				go func() {
					defer meterDone.Done()
					w.runAudioLevelTask(ctx)
				}()
			}
			if cmd.replyError != nil {
				cmd.replyError <- err
				close(cmd.replyError)
			}
		case commandClose:
			stopMeter()
			w.shutdown()
			return
		}
	}

	// Command channel closed without an explicit Close command.
	stopMeter()
	w.shutdown()
}

// applyAnswer parses and applies the remote answer SDP. It mirrors the Rust
// apply_answer helper.
func (w *worker) applyAnswer(answerSDP string) error {
	answer := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answerSDP}
	if err := w.pc.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("failed to set remote WebRTC description: %w", err)
	}
	return nil
}

// shutdown closes the peer connection and backend and emits the Closed event.
func (w *worker) shutdown() {
	if err := w.pc.Close(); err != nil {
		w.emit(Failed(fmt.Sprintf("failed to close WebRTC peer connection: %v", err)))
	}
	if w.backend != nil {
		w.backend.Close()
	}
	w.emit(Closed())
}

// emit delivers an event without blocking the worker. If the consumer is not
// draining fast enough the event is dropped, preserving worker liveness; this
// matches the best-effort semantics of the Rust mpsc sends.
func (w *worker) emit(ev Event) {
	select {
	case w.events <- ev:
	default:
	}
}

// runAudioLevelTask polls the local microphone peak every audioLevelInterval
// and emits LocalAudioLevel events until the context is cancelled or the
// connection reaches a terminal state. It mirrors start_local_audio_level_task.
func (w *worker) runAudioLevelTask(ctx context.Context) {
	ticker := time.NewTicker(audioLevelInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := w.pc.ConnectionState()
			if state == webrtc.PeerConnectionStateClosed ||
				state == webrtc.PeerConnectionStateFailed {
				return
			}
			if peak, ok := w.localAudioLevel(); ok {
				w.localAudioPeak.Store(uint32(peak))
				w.emit(LocalAudioLevel(peak))
			}
		}
	}
}

// localAudioLevel resolves the current local microphone peak. It prefers the
// AudioBackend reading (the cross-platform source of truth, since pion has no
// platform ADM) and otherwise falls back to scanning the connection's media
// source stats, matching the Rust local_audio_level logic.
func (w *worker) localAudioLevel() (uint16, bool) {
	if w.backend != nil {
		if peak, ok := w.backend.LocalAudioPeak(); ok {
			return peak, true
		}
	}
	return audioPeakFromStats(w.pc.GetStats())
}

// audioPeakFromStats finds the first audio MediaSource (AudioSourceStats) entry
// in a stats report and converts its level to a peak. Returns ok=false when no
// such entry is present (the common case with pion, which has no capture
// device).
func audioPeakFromStats(report webrtc.StatsReport) (uint16, bool) {
	for _, stat := range report {
		if src, ok := stat.(webrtc.AudioSourceStats); ok && src.Kind == "audio" {
			return audioLevelToPeak(src.AudioLevel), true
		}
	}
	return 0, false
}
