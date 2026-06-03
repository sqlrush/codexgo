package appserverproto

import (
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports the stateless projections from the Rust app-server-protocol
// modules
//
//	src/protocol/mappers.rs        (ExecOneOffCommandParams -> CommandExecParams)
//	src/protocol/event_mapping.rs  (status/lifecycle projections used by
//	                                item_event_to_server_notification)
//	src/protocol/item_builders.rs  (the `From<core>` status conversions feeding
//	                                the synthetic ThreadItem builders)
//
// that are pure value conversions over internal protocol values. The v2 sum
// types these feed (ThreadItem, FileUpdateChange, the per-item status enums, the
// CommandExecParams request) are owned by the v2 item/request files (item_thread.go,
// fs_process_realtime.go); this file reuses them and does not redefine them.
//
// item_event_to_server_notification itself (the EventMsg -> ServerNotification
// dispatcher) is NOT ported here because ServerNotification and its per-variant
// notification structs are owned by the common/notification area and are not yet
// available; once they land, that dispatcher composes the conversions below.

// ExecOneOffCommandParamsToCommandExecParams ports
// `impl From<v1::ExecOneOffCommandParams> for v2::CommandExecParams` (mappers.rs).
//
// The legacy one-off exec request carries only command/timeout/cwd/sandbox; the
// v2 CommandExecParams defaults every streaming/PTY/cap field to its zero value.
// The timeout is narrowed from u64 to i64, defaulting to 60_000ms on overflow,
// and the sandbox policy passes through unchanged (the v2 SandboxPolicy is the
// same internal protocol type in this port).
func ExecOneOffCommandParamsToCommandExecParams(value ExecOneOffCommandParams) CommandExecParams {
	return CommandExecParams{
		Command:            value.Command,
		ProcessID:          nil,
		TTY:                false,
		StreamStdin:        false,
		StreamStdoutStderr: false,
		OutputBytesCap:     nil,
		DisableOutputCap:   false,
		DisableTimeout:     false,
		TimeoutMs:          ExecTimeoutMsFromV1(value.TimeoutMs),
		Cwd:                value.Cwd,
		Env:                nil,
		Size:               nil,
		SandboxPolicy:      value.SandboxPolicy,
		PermissionProfile:  nil,
	}
}

// CollabAgentStateFromAgentStatus ports `impl From<CoreAgentStatus> for
// CollabAgentState` (v2/item.rs), used by the collab arms of
// item_event_to_server_notification. The Completed variant passes through its
// optional message; Errored wraps its required message in Some; every other
// variant carries no message.
func CollabAgentStateFromAgentStatus(status protocol.AgentStatus) CollabAgentState {
	switch status.Kind {
	case protocol.AgentStatusPendingInit:
		return CollabAgentState{Status: CollabAgentStatusPendingInit}
	case protocol.AgentStatusRunning:
		return CollabAgentState{Status: CollabAgentStatusRunning}
	case protocol.AgentStatusInterrupted:
		return CollabAgentState{Status: CollabAgentStatusInterrupted}
	case protocol.AgentStatusCompleted:
		return CollabAgentState{Status: CollabAgentStatusCompleted, Message: status.CompletedMessage}
	case protocol.AgentStatusErrored:
		msg := status.ErroredMessage
		return CollabAgentState{Status: CollabAgentStatusErrored, Message: &msg}
	case protocol.AgentStatusShutdown:
		return CollabAgentState{Status: CollabAgentStatusShutdown}
	case protocol.AgentStatusNotFound:
		return CollabAgentState{Status: CollabAgentStatusNotFound}
	default:
		// Forward-compatible: an unknown core kind degrades to its string rather
		// than panicking, matching the lenient spirit of the wire contract.
		return CollabAgentState{Status: CollabAgentStatus(status.Kind)}
	}
}

// CollabAgentStatesFromMap converts a map of thread-id -> core AgentStatus into
// the v2 agents_states map, mirroring the per-entry conversion used by the
// CollabWaitingEnd arm of event_mapping.rs.
func CollabAgentStatesFromMap(statuses map[string]protocol.AgentStatus) map[string]CollabAgentState {
	out := make(map[string]CollabAgentState, len(statuses))
	for id, status := range statuses {
		out[id] = CollabAgentStateFromAgentStatus(status)
	}
	return out
}

// agentStatusIsFailure reports whether a core AgentStatus is a terminal failure
// (Errored or NotFound). This is the shared predicate the collab end-event arms
// of event_mapping.rs use to decide a tool call's CollabAgentToolCallStatus.
func agentStatusIsFailure(status protocol.AgentStatus) bool {
	switch status.Kind {
	case protocol.AgentStatusErrored, protocol.AgentStatusNotFound:
		return true
	default:
		return false
	}
}

// CollabToolCallStatusFromAgentStatus ports the single-receiver end-event rule
// shared by CollabAgentInteractionEnd, CollabCloseEnd, and CollabResumeEnd: a
// failure status maps to Failed, otherwise Completed.
func CollabToolCallStatusFromAgentStatus(status protocol.AgentStatus) CollabAgentToolCallStatus {
	if agentStatusIsFailure(status) {
		return CollabAgentToolCallStatusFailed
	}
	return CollabAgentToolCallStatusCompleted
}

// CollabToolCallStatusFromStatuses ports the CollabWaitingEnd rule: if ANY agent
// status is a failure, the call is Failed; otherwise Completed.
func CollabToolCallStatusFromStatuses(statuses map[string]protocol.AgentStatus) CollabAgentToolCallStatus {
	for _, status := range statuses {
		if agentStatusIsFailure(status) {
			return CollabAgentToolCallStatusFailed
		}
	}
	return CollabAgentToolCallStatusCompleted
}

// CollabSpawnEndStatus ports the CollabAgentSpawnEnd rule, which differs from the
// other collab end-events: a failure status is Failed; otherwise the call is
// Completed only when a receiver thread was produced (new_thread_id is set), and
// Failed when no receiver exists.
func CollabSpawnEndStatus(status protocol.AgentStatus, hasReceiver bool) CollabAgentToolCallStatus {
	if agentStatusIsFailure(status) {
		return CollabAgentToolCallStatusFailed
	}
	if hasReceiver {
		return CollabAgentToolCallStatusCompleted
	}
	return CollabAgentToolCallStatusFailed
}

// DynamicToolCallStatusFromSuccess ports the success->status mapping in the
// DynamicToolCallResponse arm of event_mapping.rs: true -> Completed, else Failed.
func DynamicToolCallStatusFromSuccess(success bool) DynamicToolCallStatus {
	if success {
		return DynamicToolCallStatusCompleted
	}
	return DynamicToolCallStatusFailed
}

// CommandExecutionStatusFromCore ports `impl From<&CoreExecCommandStatus> for
// CommandExecutionStatus` (v2/item.rs), used by build_command_execution_end_item.
func CommandExecutionStatusFromCore(status protocol.ExecCommandStatus) (CommandExecutionStatus, error) {
	switch status {
	case protocol.ExecCommandStatusCompleted:
		return CommandExecutionStatusCompleted, nil
	case protocol.ExecCommandStatusFailed:
		return CommandExecutionStatusFailed, nil
	case protocol.ExecCommandStatusDeclined:
		return CommandExecutionStatusDeclined, nil
	default:
		return "", fmt.Errorf("appserverproto: unknown ExecCommandStatus %q", status)
	}
}

// CommandExecutionSourceFromCore ports the CoreExecCommandSource ->
// CommandExecutionSource conversion (v2_enum_from_core! macro). Unknown variants
// fall back to the default (Agent), matching the #[default] on the v2 enum.
func CommandExecutionSourceFromCore(source protocol.ExecCommandSource) CommandExecutionSource {
	switch source {
	case protocol.ExecCommandSourceAgent:
		return CommandExecutionSourceAgent
	case protocol.ExecCommandSourceUserShell:
		return CommandExecutionSourceUserShell
	case protocol.ExecCommandSourceUnifiedExecStartup:
		return CommandExecutionSourceUnifiedExecStartup
	case protocol.ExecCommandSourceUnifiedExecInteraction:
		return CommandExecutionSourceUnifiedExecInteractio
	default:
		return DefaultCommandExecutionSource
	}
}

// PatchApplyStatusFromCore ports `impl From<&CorePatchApplyStatus> for
// PatchApplyStatus` (v2/item.rs), used by build_file_change_end_item.
func PatchApplyStatusFromCore(status protocol.PatchApplyStatus) (PatchApplyStatus, error) {
	switch status {
	case protocol.PatchApplyStatusCompleted:
		return PatchApplyStatusCompleted, nil
	case protocol.PatchApplyStatusFailed:
		return PatchApplyStatusFailed, nil
	case protocol.PatchApplyStatusDeclined:
		return PatchApplyStatusDeclined, nil
	default:
		return "", fmt.Errorf("appserverproto: unknown PatchApplyStatus %q", status)
	}
}

// ExecOutputDeltaText ports the ExecCommandOutputDelta arm of event_mapping.rs:
// the raw output chunk becomes the notification delta. Go's []byte->string
// conversion preserves valid bytes and substitutes the Unicode replacement
// character for invalid sequences when later rendered, matching the boundary
// behavior of String::from_utf8_lossy that clients depend on.
func ExecOutputDeltaText(chunk []byte) string {
	return string(chunk)
}

// DurationMillisToI64 ports the `i64::try_from(duration.as_millis()).ok()` pattern
// used for the DynamicToolCall duration_ms: a non-negative millisecond count is
// returned as a pointer; a negative value (an out-of-range u128 in the reference)
// yields nil (None).
func DurationMillisToI64(millis int64) *int64 {
	if millis < 0 {
		return nil
	}
	out := millis
	return &out
}

// DurationMillisSaturating ports the `i64::try_from(...).unwrap_or(i64::MAX)`
// pattern in build_command_execution_end_item for the command duration: negative
// inputs (out-of-range u128 millis) saturate to MaxInt64, all else passes through.
func DurationMillisSaturating(millis int64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	if millis < 0 {
		return maxInt64
	}
	return millis
}

// ExecTimeoutMsFromV1 ports the timeout projection in mappers.rs
// (ExecOneOffCommandParams -> CommandExecParams): an absent timeout stays None,
// a present timeout is narrowed to i64, and an out-of-range value defaults to
// 60_000ms, matching `i64::try_from(timeout).unwrap_or(60_000)`.
func ExecTimeoutMsFromV1(timeoutMs *uint64) *int64 {
	if timeoutMs == nil {
		return nil
	}
	const defaultTimeoutMs = int64(60_000)
	const maxInt64 = uint64(int64(^uint64(0) >> 1))
	var out int64
	if *timeoutMs > maxInt64 {
		out = defaultTimeoutMs
	} else {
		out = int64(*timeoutMs)
	}
	return &out
}
