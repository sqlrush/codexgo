package cliutil

import (
	"fmt"
	"strings"
)

// ThreadID is a minimal local model of codex_protocol::ThreadId sufficient for
// rendering `codex resume` hints.
//
// Upstream the type wraps a UUID and renders via its Display implementation as
// the canonical lowercase, hyphenated UUID string. The resume helpers only need
// that string form, so this type stores the rendered string and validates it on
// construction via [ParseThreadID].
//
// The zero ThreadID is the absent value; pass a *ThreadID to the resume helpers
// to express the upstream Option<ThreadId> argument.
type ThreadID struct {
	uuid string
}

// ParseThreadID validates a UUID string and returns the corresponding
// [ThreadID]. It mirrors ThreadId::from_string, which parses the string as a
// UUID and rejects malformed input. The canonical lowercase, hyphenated form is
// stored so [ThreadID.String] reproduces the upstream Display output.
func ParseThreadID(s string) (ThreadID, error) {
	canonical, ok := canonicalizeUUID(s)
	if !ok {
		return ThreadID{}, fmt.Errorf("invalid thread id %q: not a valid UUID", s)
	}
	return ThreadID{uuid: canonical}, nil
}

// NewThreadID constructs a ThreadID from an already-canonical UUID string
// without validation. It exists for callers that have obtained a ThreadID from
// a trusted source. Prefer [ParseThreadID] for untrusted input.
func NewThreadID(uuid string) ThreadID {
	return ThreadID{uuid: uuid}
}

// String returns the canonical UUID string, mirroring the upstream Display
// implementation. The zero ThreadID renders as the empty string.
func (t ThreadID) String() string {
	return t.uuid
}

// IsZero reports whether the value is the zero ThreadID (no id set).
func (t ThreadID) IsZero() bool {
	return t.uuid == ""
}

// canonicalizeUUID validates that s is a hyphenated UUID (8-4-4-4-12 hex
// digits) and returns its lowercase form. It mirrors the subset of
// uuid::Uuid::parse_str behavior that the resume helpers depend on.
func canonicalizeUUID(s string) (string, bool) {
	const expectedLen = 36
	if len(s) != expectedLen {
		return "", false
	}
	// Positions of the hyphens in 8-4-4-4-12 layout.
	for _, pos := range [...]int{8, 13, 18, 23} {
		if s[pos] != '-' {
			return "", false
		}
	}
	var b strings.Builder
	b.Grow(expectedLen)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			// Hyphen already validated above.
			b.WriteByte('-')
			continue
		}
		lc, ok := lowerHexDigit(c)
		if !ok {
			return "", false
		}
		b.WriteByte(lc)
	}
	return b.String(), true
}

// lowerHexDigit reports whether c is a hex digit and, if so, returns its
// lowercase form.
func lowerHexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c, true
	case c >= 'a' && c <= 'f':
		return c, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 'a', true
	default:
		return 0, false
	}
}
