//go:build windows

package sleepinhibitor

import "syscall"

// Execution state flags for SetThreadExecutionState. See the Win32 docs for
// SetThreadExecutionState and EXECUTION_STATE.
const (
	// esContinuous keeps the requested state in effect until the next call
	// that resets it (ES_CONTINUOUS).
	esContinuous = 0x80000000
	// esSystemRequired forces the system to be in the working state, preventing
	// idle system sleep (ES_SYSTEM_REQUIRED). This corresponds to the Rust
	// crate's PowerRequestSystemRequired: it keeps the machine awake without
	// forcing the display to stay on.
	esSystemRequired = 0x00000001
)

// winBackend prevents idle system sleep on Windows using
// SetThreadExecutionState with ES_CONTINUOUS | ES_SYSTEM_REQUIRED.
//
// The Rust crate uses PowerCreateRequest + PowerSetRequest(SystemRequired).
// SetThreadExecutionState provides the equivalent "prevent idle system sleep"
// guarantee while remaining within the Go standard library (no third-party
// windows-sys bindings). The flag is cleared on release by resetting to
// ES_CONTINUOUS alone.
type winBackend struct {
	// active records whether the execution state is currently engaged so that
	// acquire and release are idempotent.
	active bool
	// setThreadExecutionState is the resolved kernel32 entry point. It is held
	// on the struct so it is resolved once per inhibitor.
	setThreadExecutionState *syscall.LazyProc
}

// newBackend returns the Windows sleep-prevention backend.
func newBackend() backend {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	return &winBackend{
		setThreadExecutionState: kernel32.NewProc("SetThreadExecutionState"),
	}
}

// acquire engages ES_SYSTEM_REQUIRED. It is idempotent while already active.
func (b *winBackend) acquire() {
	if b.active {
		return
	}
	ret := b.call(esContinuous | esSystemRequired)
	if ret == 0 {
		warn("Failed to acquire Windows sleep-prevention request: SetThreadExecutionState returned 0")
		return
	}
	b.active = true
}

// release clears the engaged execution state, allowing the system to sleep
// again. It is idempotent.
func (b *winBackend) release() {
	if !b.active {
		return
	}
	ret := b.call(esContinuous)
	if ret == 0 {
		warn("Failed to clear Windows sleep-prevention request: SetThreadExecutionState returned 0")
	}
	// Mark inactive regardless: even if the reset call failed, retrying with a
	// fresh acquire later is the correct recovery path.
	b.active = false
}

// call invokes SetThreadExecutionState with the given flags and returns its
// EXECUTION_STATE result (0 indicates failure).
func (b *winBackend) call(flags uintptr) uintptr {
	ret, _, _ := b.setThreadExecutionState.Call(flags)
	return ret
}
