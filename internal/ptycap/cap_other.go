//go:build !unix

package ptycap

// ConPTYSupported reports whether a console PTY is available. On platforms
// without PTY support in this build it returns false.
func ConPTYSupported() bool { return false }
