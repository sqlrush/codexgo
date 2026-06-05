package cli

import _ "embed"

// fishCompletionData is the fish completion script for `codex`, produced by
// clap_complete v4.5.65's fish generator (Fish::try_generate). codex's CLI is a
// fixed clap command tree, so the generated script is fully deterministic; it is
// vendored verbatim here as a parity asset and emitted unchanged.
//
// Unlike the bash generator (completion_bash.go), which is a faithful port of
// clap_complete's template driven by completion_tree.go, the fish script carries
// per-flag help text, value names, and possible-value help entries that exist
// only in codex's Rust CLI definition. Reproducing those byte-for-byte means
// embedding the metadata regardless; vendoring the deterministic output is the
// honest byte-identical equivalent. See DEVIATIONS.md (completion row).
//
//go:embed completion_fish.txt
var fishCompletionData string

// fishCompletionScript returns the fish completion script for `codex`.
func fishCompletionScript() string {
	return fishCompletionData
}
