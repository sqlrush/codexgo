package tui

import "math/rand"

// composerPlaceholders is the rotating pool of example prompts codex shows in the
// idle composer. It mirrors the PLACEHOLDERS array in
// codex-rs/tui/src/chatwidget.rs (8 entries). codex picks one at random at
// ChatWidget construction (chatwidget/constructor.rs:
// PLACEHOLDERS[rng.random_range(0..len)]); codexgo mirrors that selection so the
// idle composer reads identically. Because the choice is per-run random, the
// frame-parity harness masks the placeholder text row in both binaries.
var composerPlaceholders = [...]string{
	"Explain this codebase",
	"Summarize recent commits",
	"Implement {feature}",
	"Find and fix a bug in @filename",
	"Write tests for @filename",
	"Improve documentation in @filename",
	"Run /review on my current changes",
	"Use /skills to list available skills",
}

// pickComposerPlaceholder returns a random entry from the placeholder pool,
// matching codex's startup selection. The optional rng makes selection
// deterministic in tests; when nil the package default source is used.
func pickComposerPlaceholder(rng *rand.Rand) string {
	n := len(composerPlaceholders)
	var i int
	if rng != nil {
		i = rng.Intn(n)
	} else {
		i = rand.Intn(n)
	}
	return composerPlaceholders[i]
}
