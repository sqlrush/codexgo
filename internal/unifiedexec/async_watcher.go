package unifiedexec

import (
	"sync"
	"time"
	"unicode/utf8"
)

// This file ports core/src/unified_exec/async_watcher.rs: the background tasks
// that stream a live PTY session's output as deltas and emit a single late
// exec-end event when the session finally exits.
//
// The Rust watcher lives inside the unified_exec module and reaches into the
// core Session/TurnContext directly to send events. This package must NOT depend
// on core (that would cycle), so the watcher drives a [WatcherSink] callback
// surface instead; the core bridge supplies a sink that translates the calls
// into ExecCommandOutputDelta / ExecCommandEnd events.

// trailingOutputGrace is how long the streaming task drains output after the
// exit signal before finalizing the transcript. Mirrors TRAILING_OUTPUT_GRACE.
const trailingOutputGrace = 100 * time.Millisecond

// unifiedExecOutputDeltaMaxBytes is the upper bound for a single output-delta
// chunk. Mirrors UNIFIED_EXEC_OUTPUT_DELTA_MAX_BYTES; downstream consumers
// (app-server JSON-RPC) should not process arbitrarily large delta payloads.
const unifiedExecOutputDeltaMaxBytes = 8192

// maxExecOutputDeltasPerCall caps how many output-delta events a single session
// emits. Mirrors crate::exec::MAX_EXEC_OUTPUT_DELTAS_PER_CALL.
const maxExecOutputDeltasPerCall = 10_000

// WatcherInfo captures the originating-call context the late exec-end carries.
// Mirrors the arguments spawn_exit_watcher closes over (call_id, command, cwd,
// process_id, started_at).
type WatcherInfo struct {
	// CallID is the originating exec_command tool call id.
	CallID string
	// Command is the argv the session was opened with.
	Command []string
	// Cwd is the working directory the session ran in.
	Cwd string
	// ProcessID is the logical session id.
	ProcessID int
	// StartedAt is when the session was opened, used to compute the duration.
	StartedAt time.Time
}

// ExecEndInfo is the aggregated result the watcher hands to the sink when a
// session exits. Mirrors the fields emit_exec_end_for_unified_exec /
// emit_failed_exec_end_for_unified_exec build the ExecCommandEndEvent from.
type ExecEndInfo struct {
	// CallID, Command, Cwd, ProcessID carry the originating-call context.
	CallID    string
	Command   []string
	Cwd       string
	ProcessID int
	// Output is the aggregated transcript (lossy UTF-8 of the retained buffer).
	Output string
	// ExitCode is the process exit code (-1 when unknown or on failure).
	ExitCode int
	// Duration is the wall time from StartedAt to exit.
	Duration time.Duration
	// Failure is non-empty when the session ended via a recorded failure; the
	// sink should render a failed end in that case (the Rust failed-end arm).
	Failure string
}

// WatcherSink receives the events the background watcher produces. It is the
// reduced analogue of the Session.send_event surface async_watcher.rs uses.
// Implementations must be safe for concurrent use.
type WatcherSink interface {
	// OutputDelta streams an incremental, UTF-8-boundary-aligned output chunk for
	// the originating call. Mirrors EventMsg::ExecCommandOutputDelta.
	OutputDelta(callID string, chunk []byte)
	// ExecEnd emits the single late exec-end once the session exits. Mirrors
	// emit_exec_end_for_unified_exec / emit_failed_exec_end_for_unified_exec.
	ExecEnd(info ExecEndInfo)
}

// StartSessionWatcher arms the two background tasks for a live session:
// streaming output deltas (start_streaming_output) and the exit watcher
// (spawn_exit_watcher). The transcript buffer (guarded by transcriptMu) collects
// every streamed byte so the late end can report the aggregated output; it
// mirrors the shared Arc<Mutex<HeadTailBuffer>> the Rust watcher threads through
// both tasks.
//
// The tasks are tied to the process lifetime: exit/termination/Kill/Shutdown all
// cancel the process, which ends both goroutines, so no goroutine leaks. The
// streaming task signals the exit watcher via a shared drained channel once the
// transcript is fully flushed (the Rust output_drained Notify), closed exactly
// once so the exit watcher cannot miss the wakeup.
func StartSessionWatcher(process *Process, transcript *HeadTailBuffer, transcriptMu *sync.Mutex, info WatcherInfo, sink WatcherSink) {
	drained := make(chan struct{})
	startStreamingOutput(process, transcript, transcriptMu, info.CallID, sink, drained)
	spawnExitWatcher(process, transcript, transcriptMu, info, sink, drained)
}

// startStreamingOutput spawns a task that continuously reads broadcast output,
// appends it to the transcript, and emits OutputDelta calls on UTF-8 boundaries.
// On the exit signal it drains for trailingOutputGrace before finishing, then
// closes drained to release the exit watcher. Mirrors start_streaming_output.
func startStreamingOutput(process *Process, transcript *HeadTailBuffer, transcriptMu *sync.Mutex, callID string, sink WatcherSink, drained chan struct{}) {
	receiver, unsubscribe := process.SubscribeOutput()
	exitToken := process.cancellation

	go func() {
		defer unsubscribe()
		defer close(drained)

		var pending []byte
		emittedDeltas := 0

		var graceTimer *time.Timer
		var graceCh <-chan time.Time

		drainPending := func() {
			for {
				prefix, ok := splitValidUTF8Prefix(&pending)
				if !ok {
					return
				}
				transcriptMu.Lock()
				transcript.PushChunk(prefix)
				transcriptMu.Unlock()

				if emittedDeltas >= maxExecOutputDeltasPerCall {
					continue
				}
				sink.OutputDelta(callID, prefix)
				emittedDeltas++
			}
		}

		for {
			select {
			case <-exitToken.cancelled():
				if graceTimer == nil {
					// Exit signalled: give trailing output a brief grace window
					// before finalizing, mirroring TRAILING_OUTPUT_GRACE.
					graceTimer = time.NewTimer(trailingOutputGrace)
					graceCh = graceTimer.C
				}
			case <-graceCh:
				return
			case chunk, ok := <-receiver:
				if !ok {
					// The broadcast closed (output ended). Drain whatever is left,
					// then release the exit watcher.
					drainPending()
					return
				}
				pending = append(pending, chunk...)
				drainPending()
			}
		}
	}()
}

// spawnExitWatcher spawns a task that waits for the process to exit and the
// streaming task to drain, then emits a single late exec-end with the aggregated
// transcript. Mirrors spawn_exit_watcher.
func spawnExitWatcher(process *Process, transcript *HeadTailBuffer, transcriptMu *sync.Mutex, info WatcherInfo, sink WatcherSink, drained <-chan struct{}) {
	exitToken := process.cancellation

	go func() {
		<-exitToken.cancelled()
		<-drained

		duration := time.Since(info.StartedAt)
		aggregated := resolveAggregatedOutput(transcript, transcriptMu, "")

		if message, failed := process.FailureMessage(); failed {
			sink.ExecEnd(ExecEndInfo{
				CallID:    info.CallID,
				Command:   info.Command,
				Cwd:       info.Cwd,
				ProcessID: info.ProcessID,
				Output:    aggregated,
				ExitCode:  -1,
				Duration:  duration,
				Failure:   message,
			})
			return
		}

		exitCode := -1
		if code, ok := process.ExitCode(); ok {
			exitCode = code
		}
		sink.ExecEnd(ExecEndInfo{
			CallID:    info.CallID,
			Command:   info.Command,
			Cwd:       info.Cwd,
			ProcessID: info.ProcessID,
			Output:    aggregated,
			ExitCode:  exitCode,
			Duration:  duration,
		})
	}()
}

// resolveAggregatedOutput returns the lossy-UTF-8 transcript, falling back to
// the provided text when the transcript is empty. Mirrors
// resolve_aggregated_output.
func resolveAggregatedOutput(transcript *HeadTailBuffer, transcriptMu *sync.Mutex, fallback string) string {
	transcriptMu.Lock()
	defer transcriptMu.Unlock()
	if transcript.RetainedBytes() == 0 {
		return fallback
	}
	return string(toValidUTF8(transcript.ToBytes()))
}

// splitValidUTF8Prefix splits the largest valid-UTF-8 prefix off buffer (up to
// unifiedExecOutputDeltaMaxBytes). Mirrors split_valid_utf8_prefix.
func splitValidUTF8Prefix(buffer *[]byte) ([]byte, bool) {
	return splitValidUTF8PrefixWithMax(buffer, unifiedExecOutputDeltaMaxBytes)
}

// splitValidUTF8PrefixWithMax splits a valid-UTF-8 prefix of at most maxBytes
// off buffer, returning it and removing it from buffer. When no valid prefix is
// found near the boundary it emits a single byte so the stream keeps making
// progress. Mirrors split_valid_utf8_prefix_with_max.
func splitValidUTF8PrefixWithMax(buffer *[]byte, maxBytes int) ([]byte, bool) {
	buf := *buffer
	if len(buf) == 0 {
		return nil, false
	}

	maxLen := len(buf)
	if maxBytes < maxLen {
		maxLen = maxBytes
	}
	split := maxLen
	for split > 0 {
		if utf8.Valid(buf[:split]) {
			prefix := cloneBytes(buf[:split])
			*buffer = buf[split:]
			return prefix, true
		}
		if maxLen-split > 4 {
			break
		}
		split--
	}

	// No valid UTF-8 prefix near the boundary: emit the first byte so the
	// transcript still reflects all bytes and the stream progresses.
	prefix := cloneBytes(buf[:1])
	*buffer = buf[1:]
	return prefix, true
}

// toValidUTF8 replaces invalid UTF-8 sequences with the replacement character,
// matching Rust's String::from_utf8_lossy used by resolve_aggregated_output.
func toValidUTF8(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	return []byte(string([]rune(string(b))))
}
