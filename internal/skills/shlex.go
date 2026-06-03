package skills

import "strings"

// shlexSplit tokenizes a command string with POSIX shell word-splitting rules,
// mirroring the behavior of the Rust `shlex` crate's `Shlex::split` (which
// returns None on an unterminated quote or a trailing unescaped backslash).
//
// Supported: unquoted words with backslash escaping, single-quoted strings
// (literal), and double-quoted strings (with backslash escaping of $, `, ", and
// \). Whitespace separates words. Comments are not handled (shlex's split does
// not treat '#' specially mid-line).
func shlexSplit(input string) ([]string, bool) {
	var tokens []string
	var current strings.Builder
	hasToken := false

	runes := []rune(input)
	i := 0
	for i < len(runes) {
		c := runes[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if hasToken {
				tokens = append(tokens, current.String())
				current.Reset()
				hasToken = false
			}
			i++
		case c == '\\':
			if i+1 >= len(runes) {
				return nil, false // trailing backslash: invalid
			}
			current.WriteRune(runes[i+1])
			hasToken = true
			i += 2
		case c == '\'':
			hasToken = true
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
				return nil, false // unterminated single quote
			}
		case c == '"':
			hasToken = true
			i++
			closed := false
			for i < len(runes) {
				ch := runes[i]
				if ch == '"' {
					closed = true
					i++
					break
				}
				if ch == '\\' {
					if i+1 >= len(runes) {
						return nil, false // trailing backslash in double quote
					}
					next := runes[i+1]
					// Inside double quotes only $, `, ", and \ are escapable;
					// other backslashes are literal.
					switch next {
					case '$', '`', '"', '\\':
						current.WriteRune(next)
						i += 2
					default:
						current.WriteRune('\\')
						i++
					}
					continue
				}
				current.WriteRune(ch)
				i++
			}
			if !closed {
				return nil, false // unterminated double quote
			}
		default:
			current.WriteRune(c)
			hasToken = true
			i++
		}
	}

	if hasToken {
		tokens = append(tokens, current.String())
	}
	return tokens, true
}
