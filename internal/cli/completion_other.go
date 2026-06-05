package cli

import _ "embed"

// elvishCompletionData and powershellCompletionData are the elvish and
// powershell completion scripts for `codex`, produced by clap_complete v4.5.65.
// Like the zsh/fish scripts they are deterministic for codex's fixed clap
// command tree and vendored verbatim as parity assets. See completion_zsh.go for
// the rationale and DEVIATIONS.md (completion row).
//
//go:embed completion_elvish.txt
var elvishCompletionData string

//go:embed completion_powershell.txt
var powershellCompletionData string

// elvishCompletionScript returns the elvish completion script for `codex`.
func elvishCompletionScript() string {
	return elvishCompletionData
}

// powershellCompletionScript returns the powershell completion script for `codex`.
func powershellCompletionScript() string {
	return powershellCompletionData
}
