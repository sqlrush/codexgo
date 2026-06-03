// Package sleepinhibitor provides a cross-platform helper for preventing idle
// system sleep while a long-running operation (a "turn") is in progress.
//
// It is a Go port of the codex-utils-sleep-inhibitor Rust crate and preserves
// the same public behavior. The high-level API mirrors the Rust type:
//
//   - [New] constructs an inhibitor that is either enabled or disabled.
//   - [SleepInhibitor.SetTurnRunning] records the active-turn state and toggles
//     OS-level sleep prevention on or off as needed.
//   - [SleepInhibitor.IsTurnRunning] reports the latest requested turn state.
//
// Platform-specific behavior (best-effort; failures are logged and never
// propagated):
//
//   - macOS: spawns `caffeinate` while a turn is active.
//   - Linux: spawns `systemd-inhibit` or `gnome-session-inhibit` while active.
//   - Windows: uses `SetThreadExecutionState` with ES_SYSTEM_REQUIRED.
//   - Other platforms: a no-op backend.
//
// All sleep-prevention work is best-effort: when no backend is available the
// inhibitor degrades gracefully and the caller-visible state still tracks the
// requested turn state.
//
// The standard library only is used; no third-party dependencies are required.
package sleepinhibitor

import (
	"io"
	"log"
)

// backend is the per-platform implementation of sleep prevention. Each method
// is best-effort and must not panic; errors are surfaced through the package
// logger rather than returned to callers, matching the Rust crate which logs
// via `tracing::warn`.
//
// Implementations are not required to be safe for concurrent use; the owning
// [SleepInhibitor] serializes all access.
type backend interface {
	// acquire requests that the system stay awake. It is idempotent: calling
	// it while already active is a no-op (or, where the backend supervises a
	// helper process, may transparently restart a dead helper).
	acquire()
	// release stops requesting that the system stay awake. It is idempotent.
	release()
}

// logger is the destination for best-effort warning messages emitted by the
// platform backends. It defaults to a discarding logger so that the library is
// silent unless a caller opts in via [SetLogger]; this mirrors the Rust crate's
// reliance on the host application's tracing subscriber.
var logger = log.New(io.Discard, "sleepinhibitor: ", log.LstdFlags)

// SetLogger redirects the package's best-effort warning output to w. Passing a
// nil writer is treated as discarding all output. The change applies to all
// future warnings emitted by any [SleepInhibitor]; existing inhibitors are
// unaffected only in that they share this same package-level logger.
//
// This is provided because Go's standard library has no equivalent of the Rust
// `tracing` facade; callers that want visibility into backend failures can wire
// in their own writer.
func SetLogger(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	logger = log.New(w, "sleepinhibitor: ", log.LstdFlags)
}

// warn logs a best-effort warning. Backends call this instead of returning
// errors so that sleep prevention never blocks or fails a caller's turn.
func warn(format string, args ...any) {
	logger.Printf(format, args...)
}

// SleepInhibitor keeps the machine awake while a turn is in progress, but only
// when it was constructed with enabled set to true.
//
// SleepInhibitor is not safe for concurrent use. Callers that share an
// inhibitor across goroutines must provide their own synchronization.
type SleepInhibitor struct {
	enabled     bool
	turnRunning bool
	platform    backend
}

// New constructs a SleepInhibitor. When enabled is false the inhibitor records
// turn state but never engages OS-level sleep prevention.
func New(enabled bool) *SleepInhibitor {
	return &SleepInhibitor{
		enabled:     enabled,
		turnRunning: false,
		platform:    newBackend(),
	}
}

// SetTurnRunning updates the active-turn state and turns sleep prevention on or
// off as needed. When the inhibitor is disabled it always releases any active
// sleep prevention regardless of turnRunning.
//
// The method is idempotent: repeated calls with the same value have no
// additional externally observable effect beyond the first.
func (s *SleepInhibitor) SetTurnRunning(turnRunning bool) {
	s.turnRunning = turnRunning
	if !s.enabled {
		s.release()
		return
	}

	if turnRunning {
		s.acquire()
	} else {
		s.release()
	}
}

func (s *SleepInhibitor) acquire() {
	s.platform.acquire()
}

func (s *SleepInhibitor) release() {
	s.platform.release()
}

// IsTurnRunning reports the latest turn-running state requested by the caller.
func (s *SleepInhibitor) IsTurnRunning() bool {
	return s.turnRunning
}

// Close releases any active OS-level sleep prevention. It is safe to call
// multiple times. Callers should invoke Close when an inhibitor is no longer
// needed; the Rust crate relies on Drop for this, which Go does not provide.
func (s *SleepInhibitor) Close() error {
	s.release()
	return nil
}
