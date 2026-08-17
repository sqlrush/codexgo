package modelproviderinfo

// Header name/value validation mirrors the rules enforced by the Rust `http`
// crate's HeaderName::try_from and HeaderValue::try_from. Invalid headers are
// silently skipped during build_header_map, so these predicates only need to
// agree on which inputs are accepted.

// isValidHeaderName reports whether s is a valid HTTP header field name: a
// non-empty sequence of RFC 7230 "token" characters.
func isValidHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isTokenChar(s[i]) {
			return false
		}
	}
	return true
}

// isValidHeaderValue reports whether s is a valid HTTP header field value. The
// http crate accepts visible ASCII plus space and horizontal tab, and rejects
// other control characters (including CR/LF) and DEL.
func isValidHeaderValue(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\t' {
			continue
		}
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// isTokenChar reports whether c is an RFC 7230 token character.
func isTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}
