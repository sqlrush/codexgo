package realtimeconv

import "sync"

const (
	// v2HandoffCompleteAck is sent to the realtime server once a background agent
	// finishes, instructing the model to use the preceding [BACKEND] messages.
	// Mirrors the Rust REALTIME_V2_HANDOFF_COMPLETE_ACKNOWLEDGEMENT.
	v2HandoffCompleteAck = "Background agent finished. Use the preceding [BACKEND] messages as the result."
	// v2SteerAck acknowledges a steering handoff that arrived while another
	// handoff was already active. Mirrors the Rust
	// REALTIME_V2_STEER_ACKNOWLEDGEMENT.
	v2SteerAck = "This was sent to steer the previous background agent task."
)

// handoffOutputKind discriminates handoffOutput values.
type handoffOutputKind int

const (
	// handoffProgress is an intermediate background-agent update.
	handoffProgress handoffOutputKind = iota
	// handoffFinal is the terminal background-agent update.
	handoffFinal
)

// handoffOutput is a background-agent progress or final update destined for the
// realtime server. Mirrors the Rust HandoffOutput enum.
type handoffOutput struct {
	kind       handoffOutputKind
	handoffID  string
	outputText string
}

// handoffState tracks the active background-agent handoff for a running
// conversation. Mirrors the Rust RealtimeHandoffState. It is safe for concurrent
// use: the conversation loop, the manager API surface, and the fanout all touch
// it.
type handoffState struct {
	outputTx    chan<- handoffOutput
	sessionKind SessionKind

	mu             sync.Mutex
	activeHandoff  *string
	lastOutputText *string
}

// newHandoffState constructs a handoffState bound to outputTx. Mirrors the Rust
// RealtimeHandoffState::new.
func newHandoffState(outputTx chan<- handoffOutput, kind SessionKind) *handoffState {
	return &handoffState{outputTx: outputTx, sessionKind: kind}
}

// activeID returns a copy of the active handoff id, or ("", false) when none is
// active.
func (s *handoffState) activeID() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeHandoff == nil {
		return "", false
	}
	return *s.activeHandoff, true
}

// lastOutput returns a copy of the last recorded output text.
func (s *handoffState) lastOutput() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastOutputText == nil {
		return "", false
	}
	return *s.lastOutputText, true
}

// setActive replaces the active handoff id (nil clears it).
func (s *handoffState) setActive(id *string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeHandoff = cloneStringPtr(id)
}

// setLastOutput replaces the last recorded output text (nil clears it).
func (s *handoffState) setLastOutput(text *string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastOutputText = cloneStringPtr(text)
}

// clear resets both the active handoff id and the last output text. Mirrors the
// Rust clear_active_handoff.
func (s *handoffState) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeHandoff = nil
	s.lastOutputText = nil
}

// cloneStringPtr returns a fresh pointer to a copy of *p (or nil), preserving
// the immutability convention of never sharing mutable pointees across owners.
func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
