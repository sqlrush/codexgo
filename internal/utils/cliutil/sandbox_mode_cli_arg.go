package cliutil

import "fmt"

// SandboxModeCliArg is the value type for the --sandbox (-s) CLI option.
//
// It mirrors codex_utils_cli::SandboxModeCliArg, exposing the [SandboxMode]
// variants without their associated data so the choice can be made with a
// single flag. The string form of each variant is the kebab-case spelling clap
// accepts on the command line.
type SandboxModeCliArg string

const (
	// SandboxArgReadOnly selects read-only filesystem access. Spelled
	// "read-only".
	SandboxArgReadOnly SandboxModeCliArg = "read-only"

	// SandboxArgWorkspaceWrite selects workspace-write access. Spelled
	// "workspace-write".
	SandboxArgWorkspaceWrite SandboxModeCliArg = "workspace-write"

	// SandboxArgDangerFullAccess disables sandboxing. Spelled
	// "danger-full-access".
	SandboxArgDangerFullAccess SandboxModeCliArg = "danger-full-access"
)

// SandboxModeCliArgVariants returns the accepted --sandbox values in
// declaration order. The returned slice is freshly allocated so callers may
// retain or modify it without affecting package state.
func SandboxModeCliArgVariants() []SandboxModeCliArg {
	return []SandboxModeCliArg{
		SandboxArgReadOnly,
		SandboxArgWorkspaceWrite,
		SandboxArgDangerFullAccess,
	}
}

// ParseSandboxModeCliArg validates a raw --sandbox value and returns the
// matching variant. It mirrors clap's ValueEnum parsing (exact, case-sensitive
// match against the kebab-case spellings) and returns an error for any other
// input.
func ParseSandboxModeCliArg(s string) (SandboxModeCliArg, error) {
	for _, v := range SandboxModeCliArgVariants() {
		if string(v) == s {
			return v, nil
		}
	}
	return "", fmt.Errorf("invalid sandbox mode %q", s)
}

// AsCliArg returns the canonical kebab-case spelling of the variant.
func (s SandboxModeCliArg) AsCliArg() string {
	return string(s)
}

// ToSandboxMode converts the CLI flag value into the corresponding
// [SandboxMode] protocol value, mirroring the upstream
// From<SandboxModeCliArg> implementation.
func (s SandboxModeCliArg) ToSandboxMode() SandboxMode {
	switch s {
	case SandboxArgReadOnly:
		return SandboxModeReadOnly
	case SandboxArgWorkspaceWrite:
		return SandboxModeWorkspaceWrite
	case SandboxArgDangerFullAccess:
		return SandboxModeDangerFullAccess
	default:
		// Mirror Rust's exhaustive match: an out-of-range value is a
		// programming error. Fall back to the safest policy.
		return SandboxModeReadOnly
	}
}
