package sandbox

import "strings"

// regexEscape mirrors regex_lite::escape (identical to regex::escape): it inserts
// a leading backslash before every regex meta character recognized by
// regex-syntax's is_meta_character, leaving all other characters untouched.
//
// The meta set is: \ . + * ? ( ) | [ ] { } ^ $ # & - ~
func regexEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isRegexMeta(r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isRegexMeta reports whether r is a regex meta character per regex-syntax's
// is_meta_character.
func isRegexMeta(r rune) bool {
	switch r {
	case '\\', '.', '+', '*', '?', '(', ')', '|', '[', ']', '{', '}', '^', '$', '#', '&', '-', '~':
		return true
	default:
		return false
	}
}
