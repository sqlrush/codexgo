package streamparser

// utf8DecodeResult mirrors the information Rust's std::str::from_utf8 exposes via
// its Utf8Error: how many leading bytes form valid UTF-8, and the length of the
// invalid sequence (0 meaning the input merely ends with an incomplete code
// point rather than containing an outright invalid byte sequence).
type utf8DecodeResult struct {
	// ok is true when the entire input is valid UTF-8.
	ok bool
	// validUpTo is the byte offset up to which the input is valid UTF-8.
	validUpTo int
	// errorLen is the length of the invalid sequence. It is 0 when the input
	// ends with an incomplete (but not invalid) trailing code point.
	errorLen int
}

// validateUTF8 inspects b and reports validity using the same boundaries as
// Rust's UTF-8 decoder.
//
// The classification of errorLen follows Rust precisely:
//   - A byte that cannot start or continue a valid sequence yields errorLen > 0
//     (the number of bytes consumed before the error was detected, at least 1).
//   - A truncated-but-otherwise-valid trailing sequence yields errorLen == 0,
//     signalling "incomplete" so callers can buffer and wait for more bytes.
func validateUTF8(b []byte) utf8DecodeResult {
	i := 0
	n := len(b)
	for i < n {
		first := b[i]

		if first < 0x80 {
			i++
			continue
		}

		var size int
		switch {
		case first >= 0xC2 && first <= 0xDF:
			size = 2
		case first >= 0xE0 && first <= 0xEF:
			size = 3
		case first >= 0xF0 && first <= 0xF4:
			size = 4
		default:
			// Invalid leading byte (continuation byte, overlong 0xC0/0xC1, or
			// out-of-range 0xF5..0xFF). Rust reports error_len == 1 here.
			return utf8DecodeResult{ok: false, validUpTo: i, errorLen: 1}
		}

		// Validate the continuation bytes, applying the range restrictions Rust
		// enforces on the second byte to reject overlong and surrogate
		// encodings.
		for j := 1; j < size; j++ {
			if i+j >= n {
				// Truncated trailing sequence: incomplete, not invalid.
				return utf8DecodeResult{ok: false, validUpTo: i, errorLen: 0}
			}
			cont := b[i+j]
			if j == 1 {
				lo, hi := byte(0x80), byte(0xBF)
				switch first {
				case 0xE0:
					lo = 0xA0
				case 0xED:
					hi = 0x9F
				case 0xF0:
					lo = 0x90
				case 0xF4:
					hi = 0x8F
				}
				if cont < lo || cont > hi {
					// The bad byte is the second byte; Rust reports the number
					// of valid bytes consumed so far in this sequence (1).
					return utf8DecodeResult{ok: false, validUpTo: i, errorLen: 1}
				}
			} else if cont < 0x80 || cont > 0xBF {
				// A later continuation byte is invalid; error_len is the count
				// of bytes accepted in this sequence before the bad one.
				return utf8DecodeResult{ok: false, validUpTo: i, errorLen: j}
			}
		}

		i += size
	}

	return utf8DecodeResult{ok: true, validUpTo: n, errorLen: 0}
}
