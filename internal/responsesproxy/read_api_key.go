package responsesproxy

import (
	"errors"
	"fmt"
	"io"
)

// bufferSize is a generous buffer for reading the API key from stdin, matching
// codex's BUFFER_SIZE. It allows for longer keys in the future.
const bufferSize = 1024

// authHeaderPrefix is prepended to the token to form the Authorization header
// value, matching codex's AUTH_HEADER_PREFIX.
const authHeaderPrefix = "Bearer "

// ReadAuthHeaderFromStdin reads the API token from stdin and returns a complete
// Authorization header value ("Bearer <token>"). It mirrors codex's
// `read_auth_header_from_stdin`.
//
// Unlike codex, this Go port does not mlock the resulting bytes (the Go runtime
// manages and may copy the backing string), but it preserves the same parsing,
// trimming, validation, and error semantics.
func ReadAuthHeaderFromStdin(r io.Reader) (string, error) {
	return readAuthHeaderWith(r.Read)
}

// readAuthHeaderWith reads the token using readFn (a single-read function with
// io.Reader semantics) and assembles the header value. It mirrors codex's
// `read_auth_header_with`.
func readAuthHeaderWith(readFn func([]byte) (int, error)) (string, error) {
	buf := make([]byte, bufferSize)
	copy(buf, authHeaderPrefix)

	prefixLen := len(authHeaderPrefix)
	capacity := len(buf) - prefixLen
	totalRead := 0 // bytes read into the token region
	sawNewline := false
	sawEOF := false

	for totalRead < capacity {
		slice := buf[prefixLen+totalRead:]
		n, err := readFn(slice)
		// Mirror Rust's io::Read where a non-zero count with EOF is delivered
		// before the zero-count read; treat n first, then the error.
		if n > 0 {
			newlyWritten := slice[:n]
			if pos := indexByte(newlyWritten, '\n'); pos >= 0 {
				totalRead += pos + 1 // include the newline for trimming below
				sawNewline = true
				break
			}
			totalRead += n
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				sawEOF = true
				break
			}
			zeroize(buf)
			return "", fmt.Errorf("reading Authorization header from stdin: %w", err)
		}
		if n == 0 {
			sawEOF = true
			break
		}
	}

	if totalRead == capacity && !sawNewline && !sawEOF {
		zeroize(buf)
		return "", fmt.Errorf("API key is too large to fit in the %d-byte buffer", bufferSize)
	}

	total := prefixLen + totalRead
	for total > prefixLen && (buf[total-1] == '\n' || buf[total-1] == '\r') {
		total--
	}

	if total == prefixLen {
		zeroize(buf)
		return "", errors.New(
			"API key must be provided via stdin (e.g. printenv OPENAI_API_KEY | codex responses-api-proxy)")
	}

	if err := validateAuthHeaderBytes(buf[prefixLen:total]); err != nil {
		zeroize(buf)
		return "", err
	}

	header := string(buf[:total])
	zeroize(buf)
	return header, nil
}

// validateAuthHeaderBytes ensures the key matches /^[A-Za-z0-9\-_]+$/, guarding
// against NUL bytes and other funny business. It mirrors codex's
// `validate_auth_header_bytes`.
func validateAuthHeaderBytes(keyBytes []byte) error {
	for _, b := range keyBytes {
		if !isAsciiAlphanumeric(b) && b != '-' && b != '_' {
			return errors.New("API key may only contain ASCII letters, numbers, '-' or '_'")
		}
	}
	return nil
}

// isAsciiAlphanumeric reports whether b is an ASCII letter or digit.
func isAsciiAlphanumeric(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// indexByte returns the index of the first occurrence of c in b, or -1.
func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// zeroize overwrites b with zeros to limit how long secret material lingers in
// memory. It mirrors codex's use of `Zeroize` on the read buffer.
func zeroize(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
