package shellcmd

import (
	"errors"
	"strings"
)

// errNulByte is returned when a token contains a NUL byte, which cannot be
// represented on a POSIX command line.
var errNulByte = errors.New("token contains a NUL byte")

// ShlexJoin joins tokens into a single POSIX-shell-quoted command string.
//
// It mirrors codex_shell_command::parse_command::shlex_join, which delegates to
// the shlex crate's try_join and falls back to a fixed sentinel string when a
// token contains a NUL byte (which the shell cannot represent). This is the
// rendering the app-server uses for the command_execution item's `command`
// field, so matching it byte-for-byte keeps the exec JSONL a drop-in.
func ShlexJoin(tokens []string) string {
	quoted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		q, err := shlexQuote(tok)
		if err != nil {
			return "<command included NUL byte>"
		}
		quoted = append(quoted, q)
	}
	return strings.Join(quoted, " ")
}

// shlexQuote returns a POSIX-shell-safe rendering of a single token, matching
// the shlex crate's try_quote:
//
//   - An empty token becomes "''".
//   - A token containing a NUL byte is rejected.
//   - A token of only "safe" bytes (ASCII alphanumerics plus shlexSafePunct) is
//     returned verbatim.
//   - A token with no single-quote characters is wrapped in single quotes.
//   - Otherwise it is wrapped in double quotes, with $, `, ", \, and !
//     backslash-escaped.
func shlexQuote(s string) (string, error) {
	if s == "" {
		return "''", nil
	}
	if strings.IndexByte(s, 0) >= 0 {
		return "", errNulByte
	}

	if isAllShlexSafe(s) {
		return s, nil
	}

	if !strings.ContainsRune(s, '\'') {
		return "'" + s + "'", nil
	}

	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '$', '`', '"', '\\', '!':
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String(), nil
}

// shlexSafePunct is the set of punctuation bytes shlex leaves unquoted (in
// addition to ASCII alphanumerics).
const shlexSafePunct = ",._+:@%/-"

// isAllShlexSafe reports whether every byte of s is a shlex "safe" byte.
func isAllShlexSafe(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isShlexSafeByte(s[i]) {
			return false
		}
	}
	return true
}

// isShlexSafeByte reports whether a single byte is safe to leave unquoted.
func isShlexSafeByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	default:
		return strings.IndexByte(shlexSafePunct, c) >= 0
	}
}
