package cli

import _ "embed"

// zshCompletionData is the zsh completion script for `codex`, produced by
// clap_complete v4.5.65's zsh generator (Zsh::try_generate). codex's CLI is a
// fixed clap command tree, so the generated script is fully deterministic; it is
// vendored verbatim here as a parity asset and emitted unchanged.
//
// The zsh generator emits recursive `_arguments` state machines with per-flag
// help strings, value names, value hints (`_files`, `_default`, ...), and
// per-possible-value help text. That metadata lives only in codex's Rust CLI
// definition, so any byte-identical reproduction must embed it; vendoring the
// deterministic output is the honest byte-identical equivalent rather than a
// lossy hand-rolled approximation. See DEVIATIONS.md (completion row). The bash
// generator (completion_bash.go) is a true template port driven by
// completion_tree.go because bash needs no help-text metadata.
//
//go:embed completion_zsh.txt
var zshCompletionData string

// zshCompletionScript returns the zsh completion script for `codex`.
func zshCompletionScript() string {
	return zshCompletionData
}
