package realtimeconv

import (
	"context"
	"errors"
	"sync"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// Channel capacities mirror the Rust queue-capacity constants.
const (
	audioInQueueCapacity      = 256
	userTextInQueueCapacity   = 64
	handoffOutQueueCapacity   = 64
	outputEventsQueueCapacity = 256
)

// ErrNotRunning is returned by Manager methods when no conversation is active.
// It mirrors the Rust CodexErr::InvalidRequest("conversation is not running").
var ErrNotRunning = errors.New("conversation is not running")

// Manager owns the at-most-one running realtime conversation and exposes the
// input/handoff API used to drive it. It mirrors the Rust
// RealtimeConversationManager. It is safe for concurrent use.
type Manager struct {
	mu    sync.Mutex
	state *conversationState
}

// conversationState holds the live channels, handoff state, and cancellation
// handle for the running conversation. Mirrors the Rust ConversationState.
type conversationState struct {
	audioTx    chan<- protocol.RealtimeAudioFrame
	userTextTx chan<- string

	sessionKind SessionKind
	handoff     *handoffState

	cancel context.CancelFunc
	done   chan struct{}

	// active mirrors the Rust realtime_active AtomicBool and identifies this
	// conversation generation for finish/register operations.
	active *activeFlag
}

// activeFlag is a comparable, shared liveness flag identifying one conversation
// generation. It mirrors the Rust Arc<AtomicBool>: callers hold a handle and the
// manager compares identity via pointer equality.
type activeFlag struct {
	mu    sync.Mutex
	value bool
}

func newActiveFlag() *activeFlag { return &activeFlag{value: true} }

func (f *activeFlag) load() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.value
}

func (f *activeFlag) store(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.value = v
}

// swap sets the flag to v and returns the previous value, mirroring the Rust
// AtomicBool::swap used by the fanout's terminal handling.
func (f *activeFlag) swap(v bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	prev := f.value
	f.value = v
	return prev
}

// NewManager constructs an idle Manager. Mirrors the Rust
// RealtimeConversationManager::new.
func NewManager() *Manager { return &Manager{} }

// StartOutput is returned by Start. It bundles the active-flag handle and the
// output events channel for the caller's fanout, mirroring the Rust
// RealtimeStartOutput (minus the SDP, which is owned by the transport setup the
// caller performs before Start).
type StartOutput struct {
	// Active identifies this conversation generation; pass it to FinishIfActive
	// from the caller's fanout when the stream ends.
	Active *activeFlag
	// Events streams realtime output events until the conversation ends. The
	// caller's fanout drains it.
	Events <-chan Event
}

// Start launches the conversation loop over conn, replacing any prior running
// conversation (aborting it first). It mirrors the Rust
// RealtimeConversationManager::start + start_inner: it wires the input/output
// channels, spawns the loop, and records the new state.
//
// The supplied context governs the lifetime of the loop; cancelling it (or
// calling Shutdown/FinishIfActive) stops the loop and closes Events.
func (m *Manager) Start(ctx context.Context, conn Connection, kind SessionKind) (StartOutput, error) {
	if conn == nil {
		return StartOutput{}, errors.New("realtimeconv: nil connection")
	}

	// Abort any previous conversation before starting a new one.
	m.mu.Lock()
	prev := m.state
	m.state = nil
	m.mu.Unlock()
	if prev != nil {
		stopConversationState(prev)
	}

	audioTx := make(chan protocol.RealtimeAudioFrame, audioInQueueCapacity)
	userTextTx := make(chan string, userTextInQueueCapacity)
	handoffTx := make(chan handoffOutput, handoffOutQueueCapacity)
	eventsTx := make(chan Event, outputEventsQueueCapacity)

	handoff := newHandoffState(handoffTx, kind)
	active := newActiveFlag()

	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer close(eventsTx)
		runInputTask(loopCtx, inputTask{
			writer:       conn.Writer(),
			events:       conn.Events(),
			userText:     userTextTx,
			handoff:      handoffTx,
			audio:        audioTx,
			eventsTx:     eventsTx,
			handoffState: handoff,
			sessionKind:  kind,
		})
	}()

	state := &conversationState{
		audioTx:     audioTx,
		userTextTx:  userTextTx,
		sessionKind: kind,
		handoff:     handoff,
		cancel:      cancel,
		done:        done,
		active:      active,
	}

	m.mu.Lock()
	m.state = state
	m.mu.Unlock()

	return StartOutput{Active: active, Events: eventsTx}, nil
}

// RunningState reports whether a conversation is currently active. It mirrors
// the Rust running_state (returning a presence signal rather than a unit).
func (m *Manager) RunningState() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state != nil && m.state.active.load()
}

// IsRunningV2 reports whether the active conversation is a V2 session. Mirrors
// the Rust is_running_v2.
func (m *Manager) IsRunningV2() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state != nil && m.state.active.load() && m.state.sessionKind == SessionKindV2
}

// AudioIn enqueues a microphone frame for the running conversation. A full queue
// drops the frame (logged as a warning in Rust) and is not an error; a stopped
// conversation returns ErrNotRunning. Mirrors the Rust audio_in.
func (m *Manager) AudioIn(frame protocol.RealtimeAudioFrame) error {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state == nil {
		return ErrNotRunning
	}

	select {
	case <-state.done:
		return ErrNotRunning
	case state.audioTx <- frame:
		return nil
	default:
		// Full queue: drop the frame to preserve liveness.
		return nil
	}
}

// TextIn enqueues user text for the running conversation, applying the V2 user
// prefix. Blocks until the loop accepts it (matching the Rust bounded send).
// Mirrors the Rust text_in.
func (m *Manager) TextIn(ctx context.Context, text string) error {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state == nil {
		return ErrNotRunning
	}

	text = prefixText(text, UserTextPrefix, state.sessionKind)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-state.done:
		return ErrNotRunning
	case state.userTextTx <- text:
		return nil
	}
}

// HandoffOut records and enqueues a background-agent progress update for the
// active handoff (applying the V2 backend prefix). It is a no-op when no handoff
// is active. Mirrors the Rust handoff_out.
func (m *Manager) HandoffOut(ctx context.Context, outputText string) error {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state == nil {
		return ErrNotRunning
	}

	handoffID, active := state.handoff.activeID()
	if !active {
		return nil
	}

	outputText = prefixText(outputText, BackendTextPrefix, state.handoff.sessionKind)
	state.handoff.setLastOutput(&outputText)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-state.done:
		return ErrNotRunning
	case state.handoff.outputTx <- handoffOutput{kind: handoffProgress, handoffID: handoffID, outputText: outputText}:
		return nil
	}
}

// HandoffComplete enqueues the terminal background-agent update for the active
// V2 handoff. It is a no-op for V1 sessions and when no handoff/output is
// recorded. Mirrors the Rust handoff_complete.
func (m *Manager) HandoffComplete(ctx context.Context) error {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state == nil {
		return nil
	}
	if state.handoff.sessionKind != SessionKindV2 {
		return nil
	}

	handoffID, active := state.handoff.activeID()
	if !active {
		return nil
	}
	outputText, has := state.handoff.lastOutput()
	if !has {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-state.done:
		return ErrNotRunning
	case state.handoff.outputTx <- handoffOutput{kind: handoffFinal, handoffID: handoffID, outputText: outputText}:
		return nil
	}
}

// ActiveHandoffID returns the active handoff id, or ("", false) when none is
// active. Mirrors the Rust active_handoff_id.
func (m *Manager) ActiveHandoffID() (string, bool) {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state == nil {
		return "", false
	}
	return state.handoff.activeID()
}

// ClearActiveHandoff clears the active handoff id and last output. Mirrors the
// Rust clear_active_handoff.
func (m *Manager) ClearActiveHandoff() {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state != nil {
		state.handoff.clear()
	}
}

// FinishIfActive stops the conversation identified by active if it is still the
// running one. Callers pass the Active handle from StartOutput; identity is
// compared by pointer, matching the Rust Arc::ptr_eq guard in finish_if_active.
func (m *Manager) FinishIfActive(active *activeFlag) {
	m.mu.Lock()
	var state *conversationState
	if m.state != nil && m.state.active == active {
		state = m.state
		m.state = nil
	}
	m.mu.Unlock()

	if state != nil {
		stopConversationState(state)
	}
}

// Shutdown stops any running conversation. Mirrors the Rust shutdown.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	state := m.state
	m.state = nil
	m.mu.Unlock()
	if state != nil {
		stopConversationState(state)
	}
}

// stopConversationState marks the conversation inactive, cancels the loop, and
// waits for it to drain. Mirrors the Rust stop_conversation_state.
func stopConversationState(state *conversationState) {
	state.active.store(false)
	state.cancel()
	<-state.done
}
