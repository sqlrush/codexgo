package read

// isUUID reports whether s is a canonical hyphenated UUID string
// (8-4-4-4-12 lowercase/uppercase hexadecimal). It mirrors the validation
// performed by codex_protocol::ThreadId::try_from, which rejects non-UUID
// strings such as "not-a-uuid".
func isUUID(s string) bool {
	const canonicalLen = 36
	if len(s) != canonicalLen {
		return false
	}
	for i := 0; i < canonicalLen; i++ {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !isHexDigit(c) {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'a' && c <= 'f':
		return true
	case c >= 'A' && c <= 'F':
		return true
	default:
		return false
	}
}
