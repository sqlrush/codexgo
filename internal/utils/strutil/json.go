// JSON serialization helpers for output that must remain parseable as JSON
// while staying safe for ASCII-only transports such as HTTP headers.

package strutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ToASCIIJSONString serializes value as compact JSON while escaping every
// non-ASCII string character as one or more \uXXXX escapes (using UTF-16 code
// units, so characters outside the Basic Multilingual Plane become surrogate
// pairs). All ASCII bytes — including JSON structural characters, quotes,
// backslashes, and control-character escapes — are emitted exactly as Go's
// encoding/json would, which matches serde_json's compact output.
//
// The result is guaranteed to be pure ASCII and to round-trip back to the same
// JSON value.
func ToASCIIJSONString(value any) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	// serde_json does not HTML-escape '<', '>', or '&'; disable Go's default
	// HTML escaping to match its output byte-for-byte.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("strutil: serialize ascii json: %w", err)
	}

	// json.Encoder.Encode appends a trailing newline; drop it so the result
	// matches serde_json's newline-free compact output.
	compact := strings.TrimSuffix(buf.String(), "\n")

	return escapeNonASCII(compact), nil
}

// escapeNonASCII replaces every non-ASCII rune in s with its \uXXXX escape
// sequence(s). Because non-ASCII bytes in valid compact JSON only ever occur
// inside string literals, escaping them here is always safe. Invalid UTF-8
// sequences (which encoding/json never emits for valid input) are passed
// through unchanged on a per-byte basis.
func escapeNonASCII(s string) string {
	// Fast path: already ASCII.
	if isASCIIString(s) {
		return s
	}

	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			out.WriteByte(b)
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Not valid UTF-8; emit the raw byte to avoid corrupting input.
			out.WriteByte(b)
			i++
			continue
		}
		writeUnicodeEscape(&out, r)
		i += size
	}
	return out.String()
}

// writeUnicodeEscape writes r as one or two \uXXXX escapes, using a UTF-16
// surrogate pair for code points beyond the Basic Multilingual Plane.
func writeUnicodeEscape(out *strings.Builder, r rune) {
	if r > 0xFFFF {
		hi, lo := utf16.EncodeRune(r)
		fmt.Fprintf(out, "\\u%04x\\u%04x", hi, lo)
		return
	}
	fmt.Fprintf(out, "\\u%04x", r)
}

// isASCIIString reports whether every byte in s is within the ASCII range.
func isASCIIString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
