//go:build unix

package ptycap

// ConPTYSupported reports whether a console PTY is available. On Unix this is
// always true. Mirrors conpty_supported.
func ConPTYSupported() bool { return true }
