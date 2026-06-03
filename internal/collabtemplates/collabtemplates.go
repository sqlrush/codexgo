// Package collabtemplates exposes the built-in collaboration-mode prompt
// templates as byte-for-byte copies of the codex collaboration-mode-templates
// crate. The Rust crate publishes each template via include_str! over a markdown
// file under templates/; this package embeds the same files so the rendered
// prompt text is identical.
package collabtemplates

import _ "embed"

// Plan is the Plan-mode collaboration template. Rust: PLAN.
//
//go:embed templates/plan.md
var Plan string

// Default is the Default-mode collaboration template. Rust: DEFAULT.
//
//go:embed templates/default.md
var Default string

// Execute is the Execute-mode collaboration template. Rust: EXECUTE.
//
//go:embed templates/execute.md
var Execute string

// PairProgramming is the Pair-Programming-mode collaboration template. Rust:
// PAIR_PROGRAMMING.
//
//go:embed templates/pair_programming.md
var PairProgramming string
