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

// AutoCompactWindowSnapshot is an immutable view of the auto-compaction window
// accounting. Mirrors the Rust `AutoCompactWindowSnapshot`.
type AutoCompactWindowSnapshot struct {
	// Ordinal is the 1-based index of the current compaction window.
	Ordinal uint64
	// PrefillInputTokens is the absolute input-token baseline for the window, or
	// nil when no baseline has been observed yet.
	PrefillInputTokens *int64
}

// autoCompactWindow tracks the runtime accounting state for the active
// auto-compaction window. It is a faithful port of the Rust `AutoCompactWindow`:
// it records the request-input baseline (prefill) so that body-after-prefix
// scoping can subtract it from later usage. A server-observed prefill is sticky
// and cannot be overwritten by an estimate.
type autoCompactWindow struct {
	ordinal    uint64
	prefill    int64
	prefillVia autoCompactWindowPrefillKind
}

// newAutoCompactWindow returns a fresh window at ordinal 1 with no prefill.
func newAutoCompactWindow() autoCompactWindow {
	return autoCompactWindow{ordinal: 1, prefillVia: prefillNone}
}

// clearPrefill drops any recorded prefill baseline.
func (w *autoCompactWindow) clearPrefill() {
	w.prefill = 0
	w.prefillVia = prefillNone
}

// startNext advances to the next compaction window and clears the prefill.
func (w *autoCompactWindow) startNext() {
	if w.ordinal == ^uint64(0) {
		// saturating add
	} else {
		w.ordinal++
	}
	w.clearPrefill()
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
		Ordinal:            w.ordinal,
		PrefillInputTokens: prefill,
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
