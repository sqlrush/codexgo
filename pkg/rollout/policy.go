package rollout

import (
	"github.com/sqlrush/codexgo/internal/utils/strutil"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// persistedExecAggregatedOutputMaxBytes is the maximum size of the aggregated
// command output retained when persisting ExecCommandEnd events in Extended
// mode.
const persistedExecAggregatedOutputMaxBytes = 10_000

// EventPersistenceMode controls which event messages are persisted to rollout
// files.
//
// Mirrors the Rust `EventPersistenceMode` enum. The zero value is Limited,
// matching the Rust `#[default]`.
type EventPersistenceMode int

const (
	// EventPersistenceModeLimited persists only the minimal set of events
	// needed to replay a session. This is the default.
	EventPersistenceModeLimited EventPersistenceMode = iota
	// EventPersistenceModeExtended additionally persists diagnostic events
	// (with aggregated command output truncated to a fixed budget).
	EventPersistenceModeExtended
)

// IsPersistedRolloutItem reports whether a rollout item should be persisted for
// the provided persistence mode.
func IsPersistedRolloutItem(item RolloutItem, mode EventPersistenceMode) bool {
	switch item.Kind {
	case RolloutItemKindResponseItem:
		if item.ResponseItem == nil {
			return false
		}
		return ShouldPersistResponseItem(*item.ResponseItem)
	case RolloutItemKindEventMsg:
		if item.EventMsg == nil {
			return false
		}
		return ShouldPersistEventMsg(*item.EventMsg, mode)
	case RolloutItemKindCompacted, RolloutItemKindTurnContext, RolloutItemKindSessionMeta:
		// Persist Codex executive markers so flows can be analyzed.
		return true
	default:
		return false
	}
}

// PersistedRolloutItems returns the canonical rollout items that should be
// persisted for a live append, sanitizing each retained item for the mode.
//
// The returned slice is a fresh allocation; input items are never mutated.
func PersistedRolloutItems(items []RolloutItem, mode EventPersistenceMode) []RolloutItem {
	persisted := make([]RolloutItem, 0, len(items))
	for _, item := range items {
		if IsPersistedRolloutItem(item, mode) {
			persisted = append(persisted, sanitizeRolloutItemForPersistence(item, mode))
		}
	}
	return persisted
}

// sanitizeRolloutItemForPersistence truncates large fields from ExecCommandEnd
// events when persisting in Extended mode. It returns a new item without
// mutating the input.
func sanitizeRolloutItemForPersistence(item RolloutItem, mode EventPersistenceMode) RolloutItem {
	if mode != EventPersistenceModeExtended {
		return item
	}
	if item.Kind != RolloutItemKindEventMsg || item.EventMsg == nil {
		return item
	}
	ev := *item.EventMsg
	if ev.Type != protocol.EventMsgKindExecCommandEnd || ev.ExecCommandEnd == nil {
		return item
	}

	sanitizedEnd := *ev.ExecCommandEnd
	sanitizedEnd.AggregatedOutput = strutil.TruncateMiddleChars(
		sanitizedEnd.AggregatedOutput, persistedExecAggregatedOutputMaxBytes)
	sanitizedEnd.Stdout = ""
	sanitizedEnd.Stderr = ""
	sanitizedEnd.FormattedOutput = ""

	ev.ExecCommandEnd = &sanitizedEnd
	return RolloutItem{Kind: RolloutItemKindEventMsg, EventMsg: &ev}
}

// ShouldPersistResponseItem reports whether a ResponseItem should be persisted
// in rollout files.
func ShouldPersistResponseItem(item protocol.ResponseItem) bool {
	switch item.Type {
	case protocol.ResponseItemKindMessage,
		protocol.ResponseItemKindReasoning,
		protocol.ResponseItemKindLocalShellCall,
		protocol.ResponseItemKindFunctionCall,
		protocol.ResponseItemKindToolSearchCall,
		protocol.ResponseItemKindFunctionCallOutput,
		protocol.ResponseItemKindToolSearchOutput,
		protocol.ResponseItemKindCustomToolCall,
		protocol.ResponseItemKindCustomToolCallOutput,
		protocol.ResponseItemKindWebSearchCall,
		protocol.ResponseItemKindImageGenerationCall,
		protocol.ResponseItemKindCompaction,
		protocol.ResponseItemKindContextCompaction:
		return true
	case protocol.ResponseItemKindCompactionTrigger, protocol.ResponseItemKindOther:
		return false
	default:
		return false
	}
}

// ShouldPersistResponseItemForMemories reports whether a ResponseItem should be
// persisted for memory generation.
func ShouldPersistResponseItemForMemories(item protocol.ResponseItem) bool {
	switch item.Type {
	case protocol.ResponseItemKindMessage:
		return item.Role != "developer"
	case protocol.ResponseItemKindLocalShellCall,
		protocol.ResponseItemKindFunctionCall,
		protocol.ResponseItemKindToolSearchCall,
		protocol.ResponseItemKindFunctionCallOutput,
		protocol.ResponseItemKindToolSearchOutput,
		protocol.ResponseItemKindCustomToolCall,
		protocol.ResponseItemKindCustomToolCallOutput,
		protocol.ResponseItemKindWebSearchCall:
		return true
	default:
		return false
	}
}

// ShouldPersistEventMsg reports whether an EventMsg should be persisted for the
// provided persistence mode.
func ShouldPersistEventMsg(ev protocol.EventMsg, mode EventPersistenceMode) bool {
	minMode, ok := eventMsgPersistenceMode(ev)
	if !ok {
		return false
	}
	switch mode {
	case EventPersistenceModeLimited:
		return minMode == EventPersistenceModeLimited
	case EventPersistenceModeExtended:
		return minMode == EventPersistenceModeLimited || minMode == EventPersistenceModeExtended
	default:
		return false
	}
}

// eventMsgPersistenceMode returns the minimum persistence mode that includes the
// event. The second return value is false when the event should never be
// persisted.
func eventMsgPersistenceMode(ev protocol.EventMsg) (EventPersistenceMode, bool) {
	switch ev.Type {
	case protocol.EventMsgKindUserMessage,
		protocol.EventMsgKindAgentMessage,
		protocol.EventMsgKindAgentReasoning,
		protocol.EventMsgKindAgentReasoningRawContent,
		protocol.EventMsgKindPatchApplyEnd,
		protocol.EventMsgKindTokenCount,
		protocol.EventMsgKindThreadGoalUpdated,
		protocol.EventMsgKindContextCompacted,
		protocol.EventMsgKindEnteredReviewMode,
		protocol.EventMsgKindExitedReviewMode,
		protocol.EventMsgKindMcpToolCallEnd,
		protocol.EventMsgKindThreadRolledBack,
		protocol.EventMsgKindTurnAborted,
		protocol.EventMsgKindTurnStarted,
		protocol.EventMsgKindTurnComplete,
		protocol.EventMsgKindWebSearchEnd,
		protocol.EventMsgKindImageGenerationEnd:
		return EventPersistenceModeLimited, true
	case protocol.EventMsgKindItemCompleted:
		// Plan items are derived from streaming tags and are not part of the raw
		// ResponseItem history, so persist their completion (Limited) to replay
		// them on resume; all other item lifecycle completions are not persisted.
		if ev.ItemCompleted != nil && ev.ItemCompleted.Item.Type == protocol.TurnItemKindPlan {
			return EventPersistenceModeLimited, true
		}
		return 0, false
	case protocol.EventMsgKindError,
		protocol.EventMsgKindGuardianAssessment,
		protocol.EventMsgKindExecCommandEnd,
		protocol.EventMsgKindViewImageToolCall,
		protocol.EventMsgKindCollabAgentSpawnEnd,
		protocol.EventMsgKindCollabAgentInteractionEnd,
		protocol.EventMsgKindCollabWaitingEnd,
		protocol.EventMsgKindCollabCloseEnd,
		protocol.EventMsgKindCollabResumeEnd,
		protocol.EventMsgKindDynamicToolCallRequest,
		protocol.EventMsgKindDynamicToolCallResponse:
		return EventPersistenceModeExtended, true
	default:
		// Everything else is never persisted.
		return 0, false
	}
}
