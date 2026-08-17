package modelproviderinfo

import "strings"

// EnvVarError reports a missing (or empty) environment variable that a provider
// requires for its API key.
//
// This mirrors codex_protocol::error::EnvVarError, which the Rust api_key()
// wraps in CodexErr::EnvVar. (Those error types are not yet available in the Go
// internal/protocol package, so EnvVarError is defined here; it can move once
// the error crate is ported.)
type EnvVarError struct {
	// Var is the name of the environment variable that is missing.
	Var string
	// Instructions optionally helps the user obtain and set a valid value.
	Instructions *string
}

// Error mirrors the Rust Display impl:
//
//	"Missing environment variable: `<var>`."
//
// followed by " <instructions>" when instructions are present.
func (e *EnvVarError) Error() string {
	var b strings.Builder
	b.WriteString("Missing environment variable: `")
	b.WriteString(e.Var)
	b.WriteString("`.")
	if e.Instructions != nil {
		b.WriteString(" ")
		b.WriteString(*e.Instructions)
	}
	return b.String()
}
