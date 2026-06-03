package core

import (
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// ForkSnapshot selects how an existing thread's persisted history is snapshotted
// when forking. It is a faithful port of the Rust `thread_manager::ForkSnapshot`.
type ForkSnapshot struct {
	// Kind selects the snapshot strategy.
	Kind ForkSnapshotKind
	// NthUserMessage is the 0-based user-message boundary for
	// ForkSnapshotTruncateBeforeNthUserMessage.
	NthUserMessage int
}

// ForkSnapshotKind enumerates the fork snapshot strategies.
type ForkSnapshotKind int

const (
	// ForkSnapshotTruncateBeforeNthUserMessage forks a committed prefix ending
	// strictly before the nth user message (0-based). Out-of-range values keep
	// the full committed history at a turn boundary, but when the source thread
	// is currently mid-turn they fall back to cutting before the active turn's
	// opening boundary so the fork drops the unfinished suffix.
	ForkSnapshotTruncateBeforeNthUserMessage ForkSnapshotKind = iota
	// ForkSnapshotInterrupted forks the current persisted history as if the
	// source thread had been interrupted now. If the snapshot ends mid-turn it
	// appends the same <turn_aborted> marker produced by a real interrupt.
	ForkSnapshotInterrupted
)

// ForkBeforeNthUserMessage returns the truncate-before-nth-user-message snapshot,
// mirroring the Rust `From<usize> for ForkSnapshot`.
func ForkBeforeNthUserMessage(n int) ForkSnapshot {
	return ForkSnapshot{Kind: ForkSnapshotTruncateBeforeNthUserMessage, NthUserMessage: n}
}

// ForkInterrupted returns the interrupted-now snapshot.
func ForkInterrupted() ForkSnapshot {
	return ForkSnapshot{Kind: ForkSnapshotInterrupted}
}

// snapshotTurnState captures whether a persisted history ends mid-turn and, when
// it does, the active turn id and its opening boundary index. It mirrors the
// Rust `SnapshotTurnState` and the reduced `snapshot_turn_state`.
//
// STUB: the Rust version drives this off the app-server `ThreadHistoryBuilder`'s
// explicit turn lifecycle tracking. Here we reconstruct the equivalent boundary
// information directly from the rollout items: a history ends mid-turn when the
// last user message is not followed by a TurnComplete/TurnAborted event.
type snapshotTurnState struct {
	endsMidTurn          bool
	activeTurnID         *string
	activeTurnStartIndex *int
}

// computeSnapshotTurnState derives the snapshot turn state from an initial
// history, mirroring the Rust `snapshot_turn_state`.
func computeSnapshotTurnState(history rollout.InitialHistory) snapshotTurnState {
	items := history.GetRolloutItems()
	lastUser := lastUserMessagePosition(items)
	if lastUser < 0 {
		return snapshotTurnState{}
	}
	// A history ends mid-turn when no terminating boundary (TurnComplete /
	// TurnAborted) follows the last user message.
	for _, it := range items[lastUser+1:] {
		if it.Kind == rollout.RolloutItemKindEventMsg && it.EventMsg != nil {
			switch it.EventMsg.Type {
			case protocol.EventMsgKindTurnComplete, protocol.EventMsgKindTurnAborted:
				return snapshotTurnState{}
			}
		}
	}
	start := lastUser
	return snapshotTurnState{
		endsMidTurn:          true,
		activeTurnStartIndex: &start,
	}
}

// userMessagePositionsInRollout returns the indices of user-message response
// items in the rollout, mirroring the Rust
// `truncation::user_message_positions_in_rollout`.
func userMessagePositionsInRollout(items []rollout.RolloutItem) []int {
	var positions []int
	for i, it := range items {
		if it.Kind == rollout.RolloutItemKindResponseItem && it.ResponseItem != nil &&
			it.ResponseItem.IsUserMessage() {
			positions = append(positions, i)
		}
	}
	return positions
}

// lastUserMessagePosition returns the index of the final user-message response
// item, or -1 when none is present.
func lastUserMessagePosition(items []rollout.RolloutItem) int {
	last := -1
	for i, it := range items {
		if it.Kind == rollout.RolloutItemKindResponseItem && it.ResponseItem != nil &&
			it.ResponseItem.IsUserMessage() {
			last = i
		}
	}
	return last
}

// truncateRolloutBeforeNthUserMessage returns the prefix of items ending strictly
// before the nth (0-based) user message. When n is out of range the full slice is
// returned, mirroring the Rust
// `truncation::truncate_rollout_before_nth_user_message_from_start`.
func truncateRolloutBeforeNthUserMessage(items []rollout.RolloutItem, n int) []rollout.RolloutItem {
	positions := userMessagePositionsInRollout(items)
	if n < 0 || n >= len(positions) {
		return append([]rollout.RolloutItem(nil), items...)
	}
	cut := positions[n]
	return append([]rollout.RolloutItem(nil), items[:cut]...)
}

// truncateBeforeNthUserMessage applies the truncate-before-nth-user-message fork
// strategy, mirroring the Rust `truncate_before_nth_user_message`.
func truncateBeforeNthUserMessage(history rollout.InitialHistory, n int, state snapshotTurnState) rollout.InitialHistory {
	items := history.GetRolloutItems()
	positions := userMessagePositionsInRollout(items)
	var rolled []rollout.RolloutItem
	if state.endsMidTurn && n >= len(positions) {
		cut := -1
		if state.activeTurnStartIndex != nil {
			cut = *state.activeTurnStartIndex
		} else if len(positions) > 0 {
			cut = positions[len(positions)-1]
		}
		if cut >= 0 {
			rolled = append([]rollout.RolloutItem(nil), items[:cut]...)
		} else {
			rolled = append([]rollout.RolloutItem(nil), items...)
		}
	} else {
		rolled = truncateRolloutBeforeNthUserMessage(items, n)
	}
	if len(rolled) == 0 {
		return rollout.NewInitialHistory()
	}
	return rollout.InitialHistory{Kind: rollout.InitialHistoryKindForked, Forked: rolled}
}

// forkHistoryFromSnapshot produces the initial history for a fork according to
// the snapshot strategy, mirroring the Rust `fork_history_from_snapshot`.
func forkHistoryFromSnapshot(snapshot ForkSnapshot, history rollout.InitialHistory) rollout.InitialHistory {
	state := computeSnapshotTurnState(history)
	switch snapshot.Kind {
	case ForkSnapshotTruncateBeforeNthUserMessage:
		return truncateBeforeNthUserMessage(history, snapshot.NthUserMessage, state)
	case ForkSnapshotInterrupted:
		forked := toForkedHistory(history)
		if state.endsMidTurn {
			return appendInterruptedBoundary(forked, state.activeTurnID)
		}
		return forked
	default:
		return history
	}
}

// toForkedHistory converts any initial history into the Forked variant carrying
// the same rollout items, mirroring the Rust match in `fork_history_from_snapshot`.
func toForkedHistory(history rollout.InitialHistory) rollout.InitialHistory {
	switch history.Kind {
	case rollout.InitialHistoryKindNew, rollout.InitialHistoryKindCleared:
		return history
	case rollout.InitialHistoryKindForked:
		return history
	case rollout.InitialHistoryKindResumed:
		items := history.GetRolloutItems()
		return rollout.InitialHistory{Kind: rollout.InitialHistoryKindForked, Forked: items}
	default:
		return history
	}
}

// appendInterruptedBoundary appends the same persisted interrupt boundary used by
// the live interrupt path, mirroring the Rust `append_interrupted_boundary`.
//
// STUB: the Rust version optionally prepends a configurable
// InterruptedTurnHistoryMarker response item before the aborted event. That
// marker is owned by the tasks area agent; here only the TurnAborted boundary is
// appended.
func appendInterruptedBoundary(history rollout.InitialHistory, turnID *string) rollout.InitialHistory {
	aborted := rollout.NewEventMsgItem(protocol.EventMsg{
		Type: protocol.EventMsgKindTurnAborted,
		TurnAborted: &protocol.TurnAbortedEvent{
			TurnID: clonePtr(turnID),
			Reason: protocol.TurnAbortReasonInterrupted,
		},
	})
	switch history.Kind {
	case rollout.InitialHistoryKindNew, rollout.InitialHistoryKindCleared:
		return rollout.InitialHistory{Kind: rollout.InitialHistoryKindForked, Forked: []rollout.RolloutItem{aborted}}
	default:
		items := history.GetRolloutItems()
		items = append(items, aborted)
		return rollout.InitialHistory{Kind: rollout.InitialHistoryKindForked, Forked: items}
	}
}
