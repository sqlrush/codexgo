package cli

import (
	"os"
	"strings"
)

// derefString returns the pointed-to string, or "" when the pointer is nil.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// orNone returns value, or the literal "none" when value is empty. It mirrors
// the "none"/"not set" placeholders the Rust doctor uses for absent details.
func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

// isBlank reports whether value is empty after trimming surrounding whitespace.
func isBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

// envVarPresent reports whether the named environment variable is set to a
// non-empty value, mirroring the Rust doctor's env_var_present helper.
func envVarPresent(name string) bool {
	value, ok := os.LookupEnv(name)
	return ok && value != ""
}

// isTerminal reports whether f is an interactive character device. It mirrors the
// detection used by the codex entry point without pulling in a terminal library.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
