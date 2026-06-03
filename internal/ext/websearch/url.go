package websearch

import "strings"

// isAbsoluteURL reports whether s parses as an absolute URL the way the Rust url
// crate's Url::parse does: it requires a valid scheme component. A bare
// reference id such as "turn0search0" has no scheme and is rejected, while
// "https://example.com/docs" is accepted.
//
// Per RFC 3986 a scheme is ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ) followed
// by ":". The url crate additionally rejects empty schemes and one-letter
// schemes that look like Windows drive letters, but for the connector ref-id
// inputs handled here, requiring a well-formed scheme suffices and matches the
// observed behavior.
func isAbsoluteURL(s string) bool {
	colon := strings.IndexByte(s, ':')
	if colon <= 0 {
		return false
	}
	scheme := s[:colon]
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false // scheme must start with a letter
			}
		case c == '+' || c == '-' || c == '.':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
