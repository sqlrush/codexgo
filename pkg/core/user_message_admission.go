package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports `user_message_admission.rs` + `Session::steer_input`
// (upstream 0.147; spec 50 D0.2): a submitted user message is ADMITTED either
// by starting a new turn or by steering it into the running regular turn, and
// the submitter can learn which turn took it.

// UserMessageAdmissionKind says how a submitted user message was admitted.
type UserMessageAdmissionKind string

const (
	// UserMessageAdmissionStarted: core installed a new turn for the message.
	UserMessageAdmissionStarted UserMessageAdmissionKind = "started"
	// UserMessageAdmissionSteered: core accepted the message into an
	// already-running turn.
	UserMessageAdmissionSteered UserMessageAdmissionKind = "steered"
)

// UserMessageAdmission is the turn that accepted a submitted user message.
// Mirrors Rust `UserMessageAdmission`.
type UserMessageAdmission struct {
	Kind   UserMessageAdmissionKind
	TurnID string
}

// admissionResult is what a pending admission resolves to.
type admissionResult struct {
	admission UserMessageAdmission
	err       error
}

// pendingUserMessageAdmissions maps a submission id to the waiter that wants
// its admission outcome. Mirrors Rust `PendingUserMessageAdmissions`.
type pendingUserMessageAdmissions struct {
	mu      sync.Mutex
	pending map[string]chan admissionResult
}

func newPendingUserMessageAdmissions() *pendingUserMessageAdmissions {
	return &pendingUserMessageAdmissions{pending: make(map[string]chan admissionResult)}
}

// register creates a waiter for submissionID. The returned cancel removes the
// waiter (the Rust drop guard); the channel receives exactly one result.
func (p *pendingUserMessageAdmissions) register(submissionID string) (<-chan admissionResult, func()) {
	ch := make(chan admissionResult, 1)
	p.mu.Lock()
	p.pending[submissionID] = ch
	p.mu.Unlock()
	return ch, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.pending, submissionID)
	}
}

// complete delivers the outcome to the waiter for submissionID, if any.
func (p *pendingUserMessageAdmissions) complete(submissionID string, admission UserMessageAdmission, err error) {
	p.mu.Lock()
	ch, ok := p.pending[submissionID]
	delete(p.pending, submissionID)
	p.mu.Unlock()
	if ok {
		ch <- admissionResult{admission: admission, err: err}
	}
}

// SteerInputErrorKind classifies why input could not be steered.
type SteerInputErrorKind string

const (
	// SteerInputNoActiveTurn: nothing is running; the caller starts a turn.
	SteerInputNoActiveTurn SteerInputErrorKind = "no_active_turn"
	// SteerInputExpectedTurnMismatch: the caller named a turn that is not the
	// active one.
	SteerInputExpectedTurnMismatch SteerInputErrorKind = "expected_turn_mismatch"
	// SteerInputActiveTurnNotSteerable: the running task is a review or a
	// compaction, which do not accept steering.
	SteerInputActiveTurnNotSteerable SteerInputErrorKind = "active_turn_not_steerable"
	// SteerInputEmptyInput: nothing to steer.
	SteerInputEmptyInput SteerInputErrorKind = "empty_input"
)

// SteerInputError reports why [Session.SteerInput] refused the input. Mirrors
// Rust `SteerInputError`.
type SteerInputError struct {
	Kind SteerInputErrorKind
	// Expected / Actual are set for ExpectedTurnMismatch.
	Expected, Actual string
	// TurnKind is set for ActiveTurnNotSteerable ("review" | "compact").
	TurnKind string
}

func (e *SteerInputError) Error() string {
	switch e.Kind {
	case SteerInputNoActiveTurn:
		return "core: no active turn to steer"
	case SteerInputExpectedTurnMismatch:
		return fmt.Sprintf("core: expected turn %s but the active turn is %s", e.Expected, e.Actual)
	case SteerInputActiveTurnNotSteerable:
		return fmt.Sprintf("core: the active %s turn cannot be steered", e.TurnKind)
	case SteerInputEmptyInput:
		return "core: steer input is empty"
	default:
		return "core: steer input error"
	}
}

// ToErrorEvent renders the client-facing Error event, mirroring
// `SteerInputError::to_error_event` (BadRequest classification).
func (e *SteerInputError) ToErrorEvent() *protocol.ErrorEvent {
	return &protocol.ErrorEvent{
		Message:        e.Error(),
		CodexErrorInfo: &protocol.CodexErrorInfo{Kind: protocol.CodexErrorInfoBadRequest},
	}
}

// IsSteerInputError reports whether err is a SteerInputError of the given kind.
func IsSteerInputError(err error, kind SteerInputErrorKind) bool {
	var se *SteerInputError
	return errors.As(err, &se) && se.Kind == kind
}

// SteerInput folds input into the running regular turn: it becomes pending
// input consumed at the next sampling boundary and wakes input-queue
// subscribers with a Steer activity. expectedTurnID (when non-empty) must name
// the active turn. Returns the active turn id. Mirrors `Session::steer_input`.
func (s *Session) SteerInput(input []protocol.UserInput, expectedTurnID string, clientUserMessageID *string) (string, error) {
	active := s.ActiveTurn()
	if active == nil || active.Task == nil || active.Task.TurnContext == nil {
		return "", &SteerInputError{Kind: SteerInputNoActiveTurn}
	}
	activeTurnID := active.Task.TurnContext.SubID
	if expectedTurnID != "" && expectedTurnID != activeTurnID {
		return "", &SteerInputError{Kind: SteerInputExpectedTurnMismatch, Expected: expectedTurnID, Actual: activeTurnID}
	}
	switch active.Task.Kind {
	case TaskKindReview:
		return "", &SteerInputError{Kind: SteerInputActiveTurnNotSteerable, TurnKind: "review"}
	case TaskKindCompact:
		return "", &SteerInputError{Kind: SteerInputActiveTurnNotSteerable, TurnKind: "compact"}
	}
	if len(input) == 0 {
		return "", &SteerInputError{Kind: SteerInputEmptyInput}
	}
	q, _ := s.queueState()
	q.ExtendPendingInputAndAcceptMailboxDelivery(active.State, []turnInput{{
		UserContent: input,
		ClientID:    clientUserMessageID,
	}})
	return activeTurnID, nil
}

// InputQueue exposes the session's input queue (wait_agent subscribes to it).
func (s *Session) InputQueue() *InputQueue {
	q, _ := s.queueState()
	return q
}

// queueState returns the input queue and admission registry, creating them on
// first use (Spawn pre-creates them; directly constructed sessions get them
// lazily).
func (s *Session) queueState() (*InputQueue, *pendingUserMessageAdmissions) {
	s.queueOnce.Do(func() {
		if s.inputQueue == nil {
			s.inputQueue = NewInputQueue()
		}
		if s.admissions == nil {
			s.admissions = newPendingUserMessageAdmissions()
		}
	})
	return s.inputQueue, s.admissions
}
