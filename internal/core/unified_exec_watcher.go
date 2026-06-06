package core

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

// This file is the core bridge for the unified-exec background watcher
// (core/src/unified_exec/async_watcher.rs). The watcher logic itself lives in
// internal/unifiedexec (which must not depend on core); this file supplies the
// event sink that turns the watcher's OutputDelta / ExecEnd callbacks into the
// ExecCommandOutputDelta / ExecCommandEnd events a Session emits, and arms the
// watcher whenever the executor persists a live session (the Rust store_process
// calling start_streaming_output + spawn_exit_watcher).

// unifiedExecHost is implemented by tool executors that own a unified-exec
// Executor, letting the session discover it to arm the background watcher. It is
// the seam that lets internal/unifiedexec stay free of any core dependency while
// the watcher still routes its events through the owning Session.
type unifiedExecHost interface {
	unifiedExecExecutor() *unifiedexec.Executor
}

// armSessionUnifiedExecWatcher discovers the unified-exec executor on the
// session's tool router (when one is registered) and arms the background watcher
// against this session. It is a no-op when the router has no unified-exec tools.
// This is the production wiring point for async_watcher.rs: the Rust
// UnifiedExecProcessManager lives on the Session, so its watcher already has the
// session/turn context; codexgo builds the executor before the session, so the
// session arms the watcher here once both exist.
func armSessionUnifiedExecWatcher(sess *Session) {
	if sess == nil {
		return
	}
	router, ok := sess.services.ToolRouter.(*DefaultToolRouter)
	if !ok {
		return
	}
	for _, ex := range router.executors {
		if host, ok := ex.(unifiedExecHost); ok {
			if executor := host.unifiedExecExecutor(); executor != nil {
				armUnifiedExecWatcher(sess, executor)
				return
			}
		}
	}
}

// armUnifiedExecWatcher installs the session-stored hook on the executor so that
// every persisted unified-exec session spawns the background streaming/exit
// watcher, emitting late output deltas and a late exec_command_end through sess.
// Mirrors store_process arming the watcher with the session/turn context.
func armUnifiedExecWatcher(sess *Session, executor *unifiedexec.Executor) {
	if sess == nil || executor == nil {
		return
	}
	executor.SetSessionStoredHook(func(stored unifiedexec.StoredSession) {
		sink := &unifiedExecWatcherSink{
			session:          sess,
			turnID:           stored.TurnID,
			truncationPolicy: stored.TruncationPolicy,
		}
		unifiedexec.StartSessionWatcher(
			stored.Process,
			stored.Transcript,
			stored.TranscriptMu,
			unifiedexec.WatcherInfo{
				CallID:    stored.CallID,
				Command:   stored.Command,
				Cwd:       stored.Cwd,
				ProcessID: stored.ProcessID,
				StartedAt: stored.StartedAt,
			},
			sink,
		)
	})
}

// unifiedExecWatcherSink translates the background watcher callbacks into Session
// events. It is the Go analogue of the session.send_event calls in
// async_watcher.rs (process_chunk + emit_exec_end_for_unified_exec). It is safe
// for concurrent use because Session.SendEvent is.
type unifiedExecWatcherSink struct {
	session *Session
	// turnID correlates the emitted events with the turn that opened the session
	// (the Rust turn.sub_id captured by spawn_exit_watcher), not the active turn.
	turnID string
	// truncationPolicy is the originating turn's model-output truncation policy
	// (the Rust ctx.turn.truncation_policy the emitter reads). The late end formats
	// its FormattedOutput with it, exactly like the in-call path.
	truncationPolicy truncation.TruncationPolicy
}

// OutputDelta emits a late ExecCommandOutputDelta for streamed session output.
// Mirrors process_chunk's session.send_event(EventMsg::ExecCommandOutputDelta).
func (s *unifiedExecWatcherSink) OutputDelta(callID string, chunk []byte) {
	if s.session == nil {
		return
	}
	cp := make([]byte, len(chunk))
	copy(cp, chunk)
	s.session.SendEvent(s.turnID, protocol.EventMsg{
		Type: protocol.EventMsgKindExecCommandOutputDelta,
		ExecCommandOutputDelta: &protocol.ExecCommandOutputDeltaEvent{
			CallID: callID,
			Stream: protocol.ExecOutputStreamStdout,
			Chunk:  cp,
		},
	})
}

// ExecEnd emits the single late ExecCommandEnd once the session exits. Mirrors
// emit_exec_end_for_unified_exec / emit_failed_exec_end_for_unified_exec: the
// event is tagged unified_exec_startup, carries the process id, and reports the
// aggregated transcript plus exit code and duration. A non-empty Failure renders
// the failed-end shape (exit code -1, message folded into stdout/stderr).
func (s *unifiedExecWatcherSink) ExecEnd(info unifiedexec.ExecEndInfo) {
	if s.session == nil {
		return
	}

	stdout := info.Output
	stderr := ""
	aggregated := info.Output
	exitCode := int32(info.ExitCode)
	status := protocol.ExecCommandStatusCompleted

	if info.Failure != "" {
		// emit_failed_exec_end_for_unified_exec: fold the failure message into the
		// streams and force the failed exit code.
		stderr = info.Failure
		if stdout == "" {
			aggregated = info.Failure
		} else {
			aggregated = stdout + "\n" + info.Failure
		}
		exitCode = -1
		status = protocol.ExecCommandStatusFailed
	} else if info.ExitCode != 0 {
		status = protocol.ExecCommandStatusFailed
	}

	// codex computes formatted_output via format_exec_output_str(&output,
	// turn.truncation_policy) inside the emitter, which truncates the aggregated
	// output with the originating turn's policy (timed_out is always false here,
	// so build_content_with_timeout is just the aggregated output). The policy was
	// captured at store time and threaded through the session-stored hook, so the
	// late end now formats exactly like the in-call path.
	formatted := truncation.FormattedTruncateText(aggregated, s.truncationPolicy)

	processID := fmt.Sprintf("%d", info.ProcessID)
	s.session.SendEvent(s.turnID, protocol.EventMsg{
		Type: protocol.EventMsgKindExecCommandEnd,
		ExecCommandEnd: &protocol.ExecCommandEndEvent{
			CallID:           info.CallID,
			ProcessID:        &processID,
			TurnID:           s.turnID,
			CompletedAt:      nowUnixMillis(),
			Command:          info.Command,
			Cwd:              protocol.AbsolutePath(info.Cwd),
			Source:           protocol.ExecCommandSourceUnifiedExecStartup,
			Stdout:           stdout,
			Stderr:           stderr,
			AggregatedOutput: aggregated,
			ExitCode:         exitCode,
			Duration:         stdDurationRaw(info.Duration),
			FormattedOutput:  formatted,
			Status:           status,
		},
	})
}

// stdDurationRaw encodes d as a Rust std::time::Duration (`{"secs":N,"nanos":N}`)
// so the late end event matches the wire shape of ExecCommandEndEvent.duration.
func stdDurationRaw(d time.Duration) json.RawMessage {
	if d < 0 {
		d = 0
	}
	secs := int64(d / time.Second)
	nanos := int64(d % time.Second)
	b, err := json.Marshal(struct {
		Secs  int64 `json:"secs"`
		Nanos int64 `json:"nanos"`
	}{Secs: secs, Nanos: nanos})
	if err != nil {
		return json.RawMessage(`{"secs":0,"nanos":0}`)
	}
	return b
}
