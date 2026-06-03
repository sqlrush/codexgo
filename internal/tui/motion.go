package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// MotionMode selects between animated and reduced-motion rendering.
//
// Port of codex-rs/tui/src/motion.rs `MotionMode`. Callers choose an explicit
// reduced-motion fallback here instead of reaching for time-varying spinner or
// shimmer helpers directly.
type MotionMode int

const (
	// MotionAnimated renders time-varying animation.
	MotionAnimated MotionMode = iota
	// MotionReduced renders a static fallback.
	MotionReduced
)

// MotionModeFromAnimationsEnabled maps the [tui].animations flag to a mode.
//
// Port of MotionMode::from_animations_enabled.
func MotionModeFromAnimationsEnabled(animationsEnabled bool) MotionMode {
	if animationsEnabled {
		return MotionAnimated
	}
	return MotionReduced
}

// ReducedMotionIndicator is the explicit fallback an activity indicator uses
// when motion is reduced.
//
// Port of motion.rs `ReducedMotionIndicator`.
type ReducedMotionIndicator int

const (
	// ReducedMotionHidden shows nothing when motion is reduced.
	ReducedMotionHidden ReducedMotionIndicator = iota
	// ReducedMotionStaticBullet shows a dim static bullet when motion is reduced.
	ReducedMotionStaticBullet
)

// ActivityIndicator returns the bullet glyph for the current frame, or ("", false)
// when nothing should be shown.
//
// Port of motion.rs::activity_indicator. The truecolor shimmer path is rendered
// by the status area; this returns the blink fallback glyph ("•"/"◦") for the
// animated mode and the explicit fallback for reduced motion. The boolean
// reports whether the glyph should be considered dim (◦ or the static bullet).
func ActivityIndicator(start time.Time, mode MotionMode, reduced ReducedMotionIndicator) (glyph string, dim bool, show bool) {
	switch mode {
	case MotionAnimated:
		g, d := animatedActivityIndicator(start)
		return g, d, true
	case MotionReduced:
		switch reduced {
		case ReducedMotionHidden:
			return "", false, false
		case ReducedMotionStaticBullet:
			return "•", true, true
		}
	}
	return "", false, false
}

// animatedActivityIndicator returns the blink-path glyph. Port of the
// non-truecolor branch of motion.rs::animated_activity_indicator (600ms blink).
func animatedActivityIndicator(start time.Time) (string, bool) {
	elapsed := time.Duration(0)
	if !start.IsZero() {
		elapsed = time.Since(start)
	}
	blinkOn := (elapsed.Milliseconds()/600)%2 == 0
	if blinkOn {
		return "•", false
	}
	return "◦", true
}

// ShimmerText returns the spans for animated shimmer or the plain fallback.
//
// Port of motion.rs::shimmer_text. In reduced motion empty text yields no spans
// and non-empty text yields itself unchanged. The animated path is delegated to
// the status/shimmer renderer; this returns the plain text so non-animated
// callers stay correct without a shimmer dependency.
func ShimmerText(text string, mode MotionMode) []string {
	if mode == MotionReduced || text == "" {
		if text == "" {
			return nil
		}
		return []string{text}
	}
	return []string{text}
}

// --- Vim motion semantics ---------------------------------------------------
//
// The vim keymap contexts ([VimNormalKeymap], [VimOperatorKeymap],
// [VimTextObjectKeymap]) bind keys to actions; this layer names the resulting
// cursor/range semantics so the composer can apply them uniformly. Port of the
// motion concepts threaded through the chatwidget Vim handlers.

// VimMotion identifies a directional/word/line motion in Vim normal or
// operator-pending mode.
type VimMotion int

const (
	// VimMotionLeft moves one character left (h / ←).
	VimMotionLeft VimMotion = iota
	// VimMotionRight moves one character right (l / →).
	VimMotionRight
	// VimMotionUp moves one line up (k / ↑).
	VimMotionUp
	// VimMotionDown moves one line down (j / ↓).
	VimMotionDown
	// VimMotionWordForward moves to the next word start (w).
	VimMotionWordForward
	// VimMotionWordBackward moves to the previous word start (b).
	VimMotionWordBackward
	// VimMotionWordEnd moves to the next word end (e).
	VimMotionWordEnd
	// VimMotionLineStart moves to the first column (0).
	VimMotionLineStart
	// VimMotionLineEnd moves to the last column ($).
	VimMotionLineEnd
)

// VimTextObject identifies a text object for inner/around operators.
type VimTextObject int

const (
	// VimObjectWord is a word (w).
	VimObjectWord VimTextObject = iota
	// VimObjectBigWord is a WORD (W).
	VimObjectBigWord
	// VimObjectParentheses is a ( ) pair.
	VimObjectParentheses
	// VimObjectBrackets is a [ ] pair.
	VimObjectBrackets
	// VimObjectBraces is a { } pair.
	VimObjectBraces
	// VimObjectDoubleQuote is a " " pair.
	VimObjectDoubleQuote
	// VimObjectSingleQuote is a ' ' pair.
	VimObjectSingleQuote
	// VimObjectBacktick is a ` ` pair.
	VimObjectBacktick
)

// VimTextObjectScope selects inner (i) vs. around (a) for a text object.
type VimTextObjectScope int

const (
	// VimScopeInner selects the object contents only (i).
	VimScopeInner VimTextObjectScope = iota
	// VimScopeAround selects the object plus its delimiters/trailing space (a).
	VimScopeAround
)

// MotionForVimNormalKey returns the motion a normal-mode key triggers, if any.
//
// It resolves a key event against the resolved normal-mode bindings, returning
// the matched [VimMotion]. This is the data the composer needs to move the
// cursor; transitions into insert/operator modes are handled separately by the
// caller against the same keymap.
func (k VimNormalKeymap) MotionForKey(msg tea.KeyMsg) (VimMotion, bool) {
	switch {
	case k.MoveLeft.IsPressed(msg):
		return VimMotionLeft, true
	case k.MoveRight.IsPressed(msg):
		return VimMotionRight, true
	case k.MoveUp.IsPressed(msg):
		return VimMotionUp, true
	case k.MoveDown.IsPressed(msg):
		return VimMotionDown, true
	case k.MoveWordForward.IsPressed(msg):
		return VimMotionWordForward, true
	case k.MoveWordBackward.IsPressed(msg):
		return VimMotionWordBackward, true
	case k.MoveWordEnd.IsPressed(msg):
		return VimMotionWordEnd, true
	case k.MoveLineStart.IsPressed(msg):
		return VimMotionLineStart, true
	case k.MoveLineEnd.IsPressed(msg):
		return VimMotionLineEnd, true
	default:
		return 0, false
	}
}

// MotionForKey returns the motion an operator-pending key triggers, if any.
func (k VimOperatorKeymap) MotionForKey(msg tea.KeyMsg) (VimMotion, bool) {
	switch {
	case k.MotionLeft.IsPressed(msg):
		return VimMotionLeft, true
	case k.MotionRight.IsPressed(msg):
		return VimMotionRight, true
	case k.MotionUp.IsPressed(msg):
		return VimMotionUp, true
	case k.MotionDown.IsPressed(msg):
		return VimMotionDown, true
	case k.MotionWordForward.IsPressed(msg):
		return VimMotionWordForward, true
	case k.MotionWordBackward.IsPressed(msg):
		return VimMotionWordBackward, true
	case k.MotionWordEnd.IsPressed(msg):
		return VimMotionWordEnd, true
	case k.MotionLineStart.IsPressed(msg):
		return VimMotionLineStart, true
	case k.MotionLineEnd.IsPressed(msg):
		return VimMotionLineEnd, true
	default:
		return 0, false
	}
}

// TextObjectForKey returns the text object a key triggers, if any.
func (k VimTextObjectKeymap) TextObjectForKey(msg tea.KeyMsg) (VimTextObject, bool) {
	switch {
	case k.Word.IsPressed(msg):
		return VimObjectWord, true
	case k.BigWord.IsPressed(msg):
		return VimObjectBigWord, true
	case k.Parentheses.IsPressed(msg):
		return VimObjectParentheses, true
	case k.Brackets.IsPressed(msg):
		return VimObjectBrackets, true
	case k.Braces.IsPressed(msg):
		return VimObjectBraces, true
	case k.DoubleQuote.IsPressed(msg):
		return VimObjectDoubleQuote, true
	case k.SingleQuote.IsPressed(msg):
		return VimObjectSingleQuote, true
	case k.Backtick.IsPressed(msg):
		return VimObjectBacktick, true
	default:
		return 0, false
	}
}
