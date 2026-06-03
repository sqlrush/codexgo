package tui

import (
	"testing"
	"time"
)

func TestMotionModeFromAnimationsEnabled(t *testing.T) {
	if MotionModeFromAnimationsEnabled(true) != MotionAnimated {
		t.Fatal("animations enabled -> animated")
	}
	if MotionModeFromAnimationsEnabled(false) != MotionReduced {
		t.Fatal("animations disabled -> reduced")
	}
}

func TestActivityIndicatorReducedFallback(t *testing.T) {
	if _, _, show := ActivityIndicator(time.Time{}, MotionReduced, ReducedMotionHidden); show {
		t.Fatal("hidden reduced motion should not show")
	}
	glyph, dim, show := ActivityIndicator(time.Time{}, MotionReduced, ReducedMotionStaticBullet)
	if !show || glyph != "•" || !dim {
		t.Fatalf("static bullet = %q dim=%v show=%v", glyph, dim, show)
	}
}

func TestActivityIndicatorAnimatedBlink(t *testing.T) {
	// At t=0 the blink is on (solid bullet).
	glyph, dim, show := ActivityIndicator(time.Now(), MotionAnimated, ReducedMotionHidden)
	if !show {
		t.Fatal("animated indicator should show")
	}
	if glyph != "•" || dim {
		t.Fatalf("blink-on glyph = %q dim=%v, want solid bullet", glyph, dim)
	}
}

func TestShimmerTextReduced(t *testing.T) {
	if got := ShimmerText("Loading", MotionReduced); len(got) != 1 || got[0] != "Loading" {
		t.Fatalf("reduced shimmer = %v", got)
	}
	if got := ShimmerText("", MotionReduced); got != nil {
		t.Fatalf("empty shimmer = %v, want nil", got)
	}
}

func TestVimMotionForNormalKey(t *testing.T) {
	km := DefaultRuntimeKeymap().VimNormal
	if m, ok := km.MotionForKey(runeKey('h')); !ok || m != VimMotionLeft {
		t.Fatalf("h -> %v ok=%v, want left", m, ok)
	}
	if m, ok := km.MotionForKey(runeKey('w')); !ok || m != VimMotionWordForward {
		t.Fatalf("w -> %v ok=%v, want word forward", m, ok)
	}
	if _, ok := km.MotionForKey(runeKey('z')); ok {
		t.Fatal("z should not be a motion")
	}
}

func TestVimTextObjectForKey(t *testing.T) {
	km := DefaultRuntimeKeymap().VimTextObject
	if o, ok := km.TextObjectForKey(runeKey('w')); !ok || o != VimObjectWord {
		t.Fatalf("w -> %v ok=%v, want word", o, ok)
	}
	if o, ok := km.TextObjectForKey(runeKey('(')); !ok || o != VimObjectParentheses {
		t.Fatalf("( -> %v ok=%v, want parens", o, ok)
	}
}
