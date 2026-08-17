package core

import (
	"strings"
	"sync"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports codex-core's `session/input_queue.rs` (upstream 0.147; spec 50
// D0.2): session-scoped pending-input storage and active-turn mailbox delivery
// coordination. Two kinds of activity feed a running turn:
//
//   - Steer: user input submitted while a regular turn is running is folded
//     into that turn at the next sampling boundary instead of aborting it
//     (`steer_input`);
//   - Mailbox: inter-agent communications queue here and are drained into the
//     next sampling request of the current turn (while it still accepts
//     mailbox delivery) or start a new turn when they ask to trigger one.
//
// Subscribers (wait_agent) observe activity so a steer interrupts a wait.

// InputQueueActivity classifies what woke an input-queue subscriber.
type InputQueueActivity int

const (
	// InputQueueActivityMailbox is an inter-agent communication enqueued.
	InputQueueActivityMailbox InputQueueActivity = iota
	// InputQueueActivitySteer is user input steered into the running turn.
	InputQueueActivitySteer
)

// String returns the activity name.
func (a InputQueueActivity) String() string {
	if a == InputQueueActivitySteer {
		return "steer"
	}
	return "mailbox"
}

// pendingMailboxCommunication is one queued inter-agent message plus the parent
// turn that sent it (only recorded when it triggers a turn).
type pendingMailboxCommunication struct {
	communication protocol.InterAgentCommunication
	parentTurnID  *string
}

// InputQueue is the session-scoped input queue. Mirrors Rust `InputQueue`; the
// tokio watch channel becomes a set of subscriber channels that receive the
// latest activity (coalesced, never blocking the producer).
type InputQueue struct {
	mu          sync.Mutex
	mailbox     []pendingMailboxCommunication
	subscribers map[int]chan InputQueueActivity
	nextSubID   int
}

// NewInputQueue returns an empty queue.
func NewInputQueue() *InputQueue {
	return &InputQueue{subscribers: make(map[int]chan InputQueueActivity)}
}

// SubscribeActivity registers a subscriber and reports any activity already
// pending: a steer when the given turn state holds user input, else a mailbox
// activity when mails are queued. Mirrors `subscribe_activity`. The returned
// cancel func removes the subscription; the channel is closed by cancel.
func (q *InputQueue) SubscribeActivity(turnState *TurnState) (ch <-chan InputQueueActivity, pending *InputQueueActivity, cancel func()) {
	q.mu.Lock()
	id := q.nextSubID
	q.nextSubID++
	c := make(chan InputQueueActivity, 1)
	q.subscribers[id] = c
	hasMail := len(q.mailbox) > 0
	q.mu.Unlock()

	if turnState != nil && turnState.HasPendingUserInput() {
		a := InputQueueActivitySteer
		pending = &a
	} else if hasMail {
		a := InputQueueActivityMailbox
		pending = &a
	}
	cancel = func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if sub, ok := q.subscribers[id]; ok {
			delete(q.subscribers, id)
			close(sub)
		}
	}
	return c, pending, cancel
}

// notify publishes activity to every subscriber; a subscriber that has not
// consumed the previous notification keeps the newest one (watch semantics).
func (q *InputQueue) notify(activity InputQueueActivity) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, sub := range q.subscribers {
		select {
		case sub <- activity:
		default:
			select {
			case <-sub:
			default:
			}
			sub <- activity
		}
	}
}

// EnqueueMailboxCommunication queues an inter-agent message. parentTurnID is
// only recorded for messages that trigger a turn. Mirrors
// `enqueue_mailbox_communication`.
func (q *InputQueue) EnqueueMailboxCommunication(communication protocol.InterAgentCommunication, parentTurnID *string) {
	q.mu.Lock()
	q.mailbox = append(q.mailbox, pendingMailboxCommunication{communication: communication, parentTurnID: parentTurnID})
	q.mu.Unlock()
	q.notify(InputQueueActivityMailbox)
}

// HasPendingMailboxItems reports whether any mail is queued.
func (q *InputQueue) HasPendingMailboxItems() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.mailbox) > 0
}

// HasTriggerTurnMailboxItems reports whether any queued mail asks to start a turn.
func (q *InputQueue) HasTriggerTurnMailboxItems() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, mail := range q.mailbox {
		if mail.communication.TriggerTurn {
			return true
		}
	}
	return false
}

// DrainMailboxInputItems removes every queued mail and returns them as turn
// input plus the unique parent turn id of the trigger-turn mails (nil when they
// disagree, are blank, or none triggers a turn). Mirrors
// `drain_mailbox_input_items`.
func (q *InputQueue) DrainMailboxInputItems() ([]turnInput, *string) {
	q.mu.Lock()
	mails := q.mailbox
	q.mailbox = nil
	q.mu.Unlock()

	var parentTurnID *string
	seenTrigger := false
	consistent := true
	items := make([]turnInput, 0, len(mails))
	for _, mail := range mails {
		if mail.communication.TriggerTurn {
			if !seenTrigger {
				parentTurnID = mail.parentTurnID
				seenTrigger = true
			} else if !sameOptionalString(parentTurnID, mail.parentTurnID) {
				consistent = false
			}
		}
		comm := mail.communication
		items = append(items, turnInput{Communication: &comm})
	}
	if !consistent || parentTurnID == nil || strings.TrimSpace(*parentTurnID) == "" {
		parentTurnID = nil
	}
	return items, parentTurnID
}

// ExtendPendingInputAndAcceptMailboxDelivery appends steered input to the
// turn's pending input, re-opens mailbox delivery for the current turn, and
// wakes subscribers with a Steer activity. Mirrors
// `extend_pending_input_and_accept_mailbox_delivery_for_turn_state`.
func (q *InputQueue) ExtendPendingInputAndAcceptMailboxDelivery(turnState *TurnState, input []turnInput) {
	turnState.ExtendPendingInput(input)
	turnState.AcceptMailboxDeliveryForCurrentTurn()
	q.notify(InputQueueActivitySteer)
}

// GetPendingInput drains the active turn's pending input plus queued mail
// (when the turn still accepts mailbox delivery). Mirrors `get_pending_input`.
// With no active turn only the mailbox is drained.
func (q *InputQueue) GetPendingInput(active *ActiveTurn) ([]turnInput, *string) {
	var pending []turnInput
	acceptsMailbox := true
	if active != nil && active.State != nil {
		acceptsMailbox = active.State.AcceptsMailboxDeliveryForCurrentTurn()
		if acceptsMailbox {
			pending = active.State.TakePendingInput()
		}
	}
	if !acceptsMailbox {
		return pending, nil
	}
	mails, parentTurnID := q.DrainMailboxInputItems()
	if len(pending) == 0 {
		return mails, parentTurnID
	}
	return append(pending, mails...), parentTurnID
}

// HasPendingInput reports whether the active turn has steered input or (while
// it accepts mailbox delivery) mail is queued. Mirrors `has_pending_input`.
func (q *InputQueue) HasPendingInput(active *ActiveTurn) bool {
	acceptsMailbox := true
	hasTurnPending := false
	if active != nil && active.State != nil {
		hasTurnPending = active.State.HasPendingInput()
		acceptsMailbox = active.State.AcceptsMailboxDeliveryForCurrentTurn()
	}
	if !acceptsMailbox {
		return false
	}
	if hasTurnPending {
		return true
	}
	return q.HasPendingMailboxItems()
}

// DeferMailboxDeliveryToNextTurn keeps queue-only child mail pending for the
// next turn when the current turn has no explicit same-turn work left. Mirrors
// `defer_mailbox_delivery_to_next_turn`.
func (q *InputQueue) DeferMailboxDeliveryToNextTurn(active *ActiveTurn, subID string) {
	if active == nil || active.State == nil || active.Task == nil || active.Task.TurnContext == nil || active.Task.TurnContext.SubID != subID {
		return
	}
	if active.State.HasExplicitSameTurnWork() {
		return
	}
	active.State.SetMailboxDeliveryPhase(MailboxNextTurn)
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
