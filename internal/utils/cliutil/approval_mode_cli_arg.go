package cliutil

import "fmt"

// ApprovalModeCliArg is the value type for the --approval-mode CLI option.
//
// It mirrors codex_utils_cli::ApprovalModeCliArg. The string form of each
// variant is the kebab-case spelling clap accepts on the command line; use
// [ParseApprovalModeCliArg] to validate user input and [ApprovalModeCliArg.AsCliArg]
// to render the canonical spelling.
type ApprovalModeCliArg string

const (
	// ApprovalModeUntrusted runs only "trusted" commands without asking and
	// escalates anything else to the user. Spelled "untrusted".
	ApprovalModeUntrusted ApprovalModeCliArg = "untrusted"

	// ApprovalModeOnFailure (DEPRECATED) runs all commands without asking and
	// only escalates on failure. Spelled "on-failure".
	ApprovalModeOnFailure ApprovalModeCliArg = "on-failure"

	// ApprovalModeOnRequest lets the model decide when to ask. Spelled
	// "on-request".
	ApprovalModeOnRequest ApprovalModeCliArg = "on-request"

	// ApprovalModeNever never asks for approval. Spelled "never".
	ApprovalModeNever ApprovalModeCliArg = "never"
)

// ApprovalModeCliArgVariants returns the accepted --approval-mode values in
// declaration order. The returned slice is freshly allocated so callers may
// retain or modify it without affecting package state.
func ApprovalModeCliArgVariants() []ApprovalModeCliArg {
	return []ApprovalModeCliArg{
		ApprovalModeUntrusted,
		ApprovalModeOnFailure,
		ApprovalModeOnRequest,
		ApprovalModeNever,
	}
}

// ParseApprovalModeCliArg validates a raw --approval-mode value and returns the
// matching variant. It mirrors clap's ValueEnum parsing (exact, case-sensitive
// match against the kebab-case spellings) and returns an error for any other
// input.
func ParseApprovalModeCliArg(s string) (ApprovalModeCliArg, error) {
	for _, v := range ApprovalModeCliArgVariants() {
		if string(v) == s {
			return v, nil
		}
	}
	return "", fmt.Errorf("invalid approval mode %q", s)
}

// AsCliArg returns the canonical kebab-case spelling of the variant.
func (a ApprovalModeCliArg) AsCliArg() string {
	return string(a)
}

// ToAskForApproval converts the CLI flag value into the corresponding
// [AskForApproval] protocol value, mirroring the upstream From<ApprovalModeCliArg>
// implementation.
func (a ApprovalModeCliArg) ToAskForApproval() AskForApproval {
	switch a {
	case ApprovalModeUntrusted:
		return AskForApprovalUnlessTrusted
	case ApprovalModeOnFailure:
		return AskForApprovalOnFailure
	case ApprovalModeOnRequest:
		return AskForApprovalOnRequest
	case ApprovalModeNever:
		return AskForApprovalNever
	default:
		// Mirror Rust's exhaustive match: an out-of-range value is a
		// programming error. Fall back to the safest policy.
		return AskForApprovalOnRequest
	}
}
