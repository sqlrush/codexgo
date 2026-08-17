package core

import "github.com/sqlrush/codexgo/internal/protocol"

// autoCompactWindowPrefillKind distinguishes how the prefill baseline for the
// active auto-compaction window was obtained. A server-observed sample always
// takes precedence over an estimated one. Mirrors the Rust
// `AutoCompactWindowPrefill` enum.
type autoCompactWindowPrefillKind int

const (
	prefillNone autoCompactWindowPrefillKind = iota
	prefillServerObserved
	prefillEstimated
)

// AutoCompactWindowIDs are the UUIDv7 identities of the context windows a
// session has moved through: the first window, the previous one (nil for the
// first) and the current one. Mirrors the Rust `AutoCompactWindowIds` (0.147);
// the current id is the `x-codex-window-id` request header value.
type AutoCompactWindowIDs struct {
	FirstWindowID    string
	PreviousWindowID *string
	WindowID         string
}

// newInitialAutoCompactWindowIDs mints the first window identity.
func newInitialAutoCompactWindowIDs() AutoCompactWindowIDs {
	id := protocol.NewUUIDV7()
	return AutoCompactWindowIDs{FirstWindowID: id, WindowID: id}
}

// AutoCompactWindowSnapshot is an immutable view of the auto-compaction window
// accounting. Mirrors the Rust `AutoCompactWindowSnapshot`.
type AutoCompactWindowSnapshot struct {
	// Ordinal is the 1-based index of the current compaction window (the Rust
	// window_number + 1; kept for the existing accounting consumers).
	Ordinal uint64
	// WindowNumber is the 0-based number of compactions performed so far
	// (0.147 `window_number`).
	WindowNumber uint64
	// IDs are the window identities.
	IDs AutoCompactWindowIDs
	// PrefillInputTokens is the absolute input-token baseline for the window, or
	// nil when no baseline has been observed yet.
	PrefillInputTokens *int64
}

// autoCompactWindow tracks the runtime accounting state for the active
// auto-compaction window. It is a port of the Rust `AutoCompactWindow` (0.147):
// it records the request-input baseline (prefill) so that body-after-prefix
// scoping can subtract it from later usage, carries the window identities, the
// pending `new_context_window` request, and the once-per-window token-budget
// reminder / auto-compact fallback claims. A server-observed prefill is sticky
// and cannot be overwritten by an estimate.
type autoCompactWindow struct {
	windowNumber uint64
	ids          AutoCompactWindowIDs

	prefill    int64
	prefillVia autoCompactWindowPrefillKind

	newContextWindowRequested    bool
	tokenBudgetReminderDelivered bool
	autoCompactFallbackDelivered bool
}

// newAutoCompactWindow returns a fresh first window with no prefill.
func newAutoCompactWindow() autoCompactWindow {
	return autoCompactWindow{ids: newInitialAutoCompactWindowIDs(), prefillVia: prefillNone}
}

// clearPrefill drops any recorded prefill baseline.
func (w *autoCompactWindow) clearPrefill() {
	w.prefill = 0
	w.prefillVia = prefillNone
}

// advance moves to the next context window: bumps the window number, mints a
// new window id (keeping the previous), and resets the per-window flags.
// Mirrors the Rust `advance`. The prefill is cleared by the caller
// (start_new_context_window / replace_history), as upstream.
func (w *autoCompactWindow) advance() (uint64, AutoCompactWindowIDs) {
	if w.windowNumber != ^uint64(0) {
		w.windowNumber++
	}
	prev := w.ids.WindowID
	w.ids.PreviousWindowID = &prev
	w.ids.WindowID = protocol.NewUUIDV7()
	w.newContextWindowRequested = false
	w.tokenBudgetReminderDelivered = false
	w.autoCompactFallbackDelivered = false
	return w.windowNumber, w.ids
}

// startNext advances to the next compaction window and clears the prefill (the
// 0.136 spelling used by the compaction paths).
func (w *autoCompactWindow) startNext() {
	w.advance()
	w.clearPrefill()
}

// restore re-installs a persisted window number and identities (resume).
func (w *autoCompactWindow) restore(windowNumber uint64, ids AutoCompactWindowIDs) {
	w.windowNumber = windowNumber
	w.ids = ids
}

// requestNewContextWindow records a `new_context_window` tool request; the turn
// loop honors it at the next sampling boundary.
func (w *autoCompactWindow) requestNewContextWindow() { w.newContextWindowRequested = true }

// takeNewContextWindowRequest returns and clears the pending request.
func (w *autoCompactWindow) takeNewContextWindowRequest() bool {
	requested := w.newContextWindowRequested
	w.newContextWindowRequested = false
	return requested
}

// claimTokenBudgetReminder returns true the first time per window.
func (w *autoCompactWindow) claimTokenBudgetReminder() bool {
	claimed := !w.tokenBudgetReminderDelivered
	w.tokenBudgetReminderDelivered = true
	return claimed
}

// claimAutoCompactFallback returns true the first time per window.
func (w *autoCompactWindow) claimAutoCompactFallback() bool {
	claimed := !w.autoCompactFallbackDelivered
	w.autoCompactFallbackDelivered = true
	return claimed
}

// ensureServerObservedPrefillFromUsage records the request-input side of the
// first server usage sample. Once a server-observed value is set it is sticky.
func (w *autoCompactWindow) ensureServerObservedPrefillFromUsage(usage protocol.TokenUsage) {
	if w.prefillVia == prefillServerObserved {
		return
	}
	w.prefill = maxInt64(usage.InputTokens, 0)
	w.prefillVia = prefillServerObserved
}

// setEstimatedPrefill records an estimated baseline (e.g. on resume/recompute).
// It will not overwrite a server-observed value.
func (w *autoCompactWindow) setEstimatedPrefill(tokens int64) {
	if w.prefillVia == prefillServerObserved {
		return
	}
	w.prefill = maxInt64(tokens, 0)
	w.prefillVia = prefillEstimated
}

// snapshot returns an immutable view of the window state.
func (w *autoCompactWindow) snapshot() AutoCompactWindowSnapshot {
	var prefill *int64
	if w.prefillVia != prefillNone {
		v := w.prefill
		prefill = &v
	}
	return AutoCompactWindowSnapshot{
		Ordinal:            w.windowNumber + 1,
		WindowNumber:       w.windowNumber,
		IDs:                w.ids,
		PrefillInputTokens: prefill,
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
