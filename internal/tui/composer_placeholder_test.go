package tui

import (
	"math/rand"
	"testing"
)

func TestComposerPlaceholderPoolMatchesCodex(t *testing.T) {
	// The pool must mirror PLACEHOLDERS in codex-rs/tui/src/chatwidget.rs.
	want := []string{
		"Explain this codebase",
		"Summarize recent commits",
		"Implement {feature}",
		"Find and fix a bug in @filename",
		"Write tests for @filename",
		"Improve documentation in @filename",
		"Run /review on my current changes",
		"Use /skills to list available skills",
	}
	if len(composerPlaceholders) != len(want) {
		t.Fatalf("pool size = %d, want %d", len(composerPlaceholders), len(want))
	}
	for i, w := range want {
		if composerPlaceholders[i] != w {
			t.Errorf("placeholder[%d] = %q, want %q", i, composerPlaceholders[i], w)
		}
	}
}

func TestPickComposerPlaceholderDeterministicWithRng(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	got := pickComposerPlaceholder(rng)
	found := false
	for _, p := range composerPlaceholders {
		if p == got {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("picked %q not in pool", got)
	}
}

func TestComposerPlaceholderIsFromPool(t *testing.T) {
	c := NewComposer(LoadTheme(nil, Capabilities{}), nil)
	got := c.Placeholder()
	for _, p := range composerPlaceholders {
		if p == got {
			return
		}
	}
	t.Fatalf("composer placeholder %q not from pool", got)
}

func TestComposerWithPlaceholderPins(t *testing.T) {
	c := NewComposer(LoadTheme(nil, Capabilities{}), nil).WithPlaceholder("PINNED")
	if c.Placeholder() != "PINNED" {
		t.Fatalf("placeholder = %q, want PINNED", c.Placeholder())
	}
}
