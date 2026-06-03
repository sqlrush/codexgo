package execpolicy

import "strings"

// This file provides a self-contained port of the two functions of the Rust
// `shlex` crate that the execpolicy crate relies on: `split` (used to tokenize
// string-form examples) and `try_join` (used to render examples in error
// messages). It is kept inside the package so execpolicy has no third-party
// dependency beyond go.starlark.net.

// shlexSplit tokenizes a POSIX-shell-style string into words, mirroring
// `shlex::split`. It returns ok=false when the input ends inside an unterminated
// quote or trailing backslash (matching the Rust function returning `None`).
//
// Supported syntax: whitespace separates words; single quotes preserve their
// contents verbatim; double quotes preserve contents except for a backslash
// that escapes one of `$`, “ ` “, `"`, or `\`; an unquoted backslash escapes
// the next character (a line continuation `\\\n` is dropped). Comments are not
// recognized, matching shlex's default.
func shlexSplit(input string) ([]string, bool) {
	var words []string
	var current strings.Builder
	// hasWord tracks whether the current builder represents a (possibly empty)
	// token, so that constructs like "" produce an empty word.
	hasWord := false

	runes := []rune(input)
	i := 0
	for i < len(runes) {
		c := runes[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if hasWord {
				words = append(words, current.String())
				current.Reset()
				hasWord = false
			}
			i++
		case c == '\'':
			hasWord = true
			i++
			closed := false
			for i < len(runes) {
				if runes[i] == '\'' {
					closed = true
					i++
					break
				}
				current.WriteRune(runes[i])
				i++
			}
			if !closed {
				return nil, false
			}
		case c == '"':
			hasWord = true
			i++
			closed := false
			for i < len(runes) {
				ch := runes[i]
				if ch == '"' {
					closed = true
					i++
					break
				}
				if ch == '\\' && i+1 < len(runes) {
					next := runes[i+1]
					switch next {
					case '$', '`', '"', '\\':
						current.WriteRune(next)
						i += 2
						continue
					case '\n':
						// Line continuation inside double quotes is dropped.
						i += 2
						continue
					}
				}
				current.WriteRune(ch)
				i++
			}
			if !closed {
				return nil, false
			}
		case c == '\\':
			if i+1 >= len(runes) {
				return nil, false
			}
			next := runes[i+1]
			if next == '\n' {
				// Line continuation outside quotes is dropped.
				i += 2
				continue
			}
			hasWord = true
			current.WriteRune(next)
			i += 2
		default:
			hasWord = true
			current.WriteRune(c)
			i++
		}
	}

	if hasWord {
		words = append(words, current.String())
	}
	return words, true
}

// shlexTryJoin joins tokens into a single POSIX-shell-quoted string, mirroring
// `shlex::try_join`. It returns ok=false when any token contains a NUL byte,
// which the shell cannot represent (matching the Rust function returning an
// error). The quoting algorithm matches `shlex::try_quote`.
func shlexTryJoin(tokens []string) (string, bool) {
	quoted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		q, ok := shlexTryQuote(tok)
		if !ok {
			return "", false
		}
		quoted = append(quoted, q)
	}
	return strings.Join(quoted, " "), true
}

// shlexTryQuote returns a POSIX-shell-safe rendering of one token, mirroring
// `shlex::try_quote`. It returns ok=false when the token contains a NUL byte.
func shlexTryQuote(s string) (string, bool) {
	if s == "" {
		return "''", true
	}
	if strings.IndexByte(s, 0) >= 0 {
		return "", false
	}
	if isAllShlexSafe(s) {
		return s, true
	}
	if !strings.ContainsRune(s, '\'') {
		return "'" + s + "'", true
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
	return b.String(), true
}

// shlexSafePunct is the set of punctuation bytes shlex treats as not requiring
// quoting (in addition to ASCII alphanumerics).
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
