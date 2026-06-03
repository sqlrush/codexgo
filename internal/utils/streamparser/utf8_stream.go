package streamparser

import "fmt"

// Utf8StreamParserErrorKind discriminates the variants of
// [Utf8StreamParserError].
type Utf8StreamParserErrorKind int

const (
	// Utf8InvalidUtf8 indicates the provided bytes contain an invalid UTF-8
	// sequence.
	Utf8InvalidUtf8 Utf8StreamParserErrorKind = iota
	// Utf8IncompleteAtEof indicates EOF was reached with a buffered partial
	// UTF-8 code point.
	Utf8IncompleteAtEof
)

// Utf8StreamParserError is returned by [Utf8StreamParser] when streamed bytes
// are not valid UTF-8.
type Utf8StreamParserError struct {
	// Kind is the error variant.
	Kind Utf8StreamParserErrorKind
	// ValidUpTo is the byte offset in the parser's buffered bytes where decoding
	// failed. Only meaningful when Kind is Utf8InvalidUtf8.
	ValidUpTo int
	// ErrorLen is the length in bytes of the invalid sequence. Only meaningful
	// when Kind is Utf8InvalidUtf8.
	ErrorLen int
}

// Error implements the error interface, matching the Rust Display output.
func (e *Utf8StreamParserError) Error() string {
	switch e.Kind {
	case Utf8InvalidUtf8:
		return fmt.Sprintf("invalid UTF-8 in streamed bytes at offset %d (error length %d)", e.ValidUpTo, e.ErrorLen)
	case Utf8IncompleteAtEof:
		return "incomplete UTF-8 code point at end of stream"
	default:
		return "unknown UTF-8 stream parser error"
	}
}

func newInvalidUtf8Error(validUpTo, errorLen int) *Utf8StreamParserError {
	return &Utf8StreamParserError{Kind: Utf8InvalidUtf8, ValidUpTo: validUpTo, ErrorLen: errorLen}
}

func newIncompleteUtf8Error() *Utf8StreamParserError {
	return &Utf8StreamParserError{Kind: Utf8IncompleteAtEof}
}

// Utf8StreamParser wraps a [StreamTextParser] and accepts raw bytes, buffering
// partial UTF-8 code points.
//
// This is useful when upstream data arrives as bytes and a code point may be
// split across chunk boundaries (for example 0xC3 followed by 0xA9 for "é").
type Utf8StreamParser[T any] struct {
	inner       StreamTextParser[T]
	pendingUTF8 []byte
}

// NewUtf8StreamParser wraps inner so it can be fed raw byte chunks.
func NewUtf8StreamParser[T any](inner StreamTextParser[T]) *Utf8StreamParser[T] {
	return &Utf8StreamParser[T]{inner: inner}
}

// PushBytes feeds a raw byte chunk.
//
// If the chunk contains invalid UTF-8, this returns an error and rolls back the
// entire pushed chunk so callers can decide how to recover without the inner
// parser seeing a partial prefix from that chunk. The provided slice is never
// mutated.
func (u *Utf8StreamParser[T]) PushBytes(chunk []byte) (StreamTextChunk[T], error) {
	oldLen := len(u.pendingUTF8)
	u.pendingUTF8 = append(u.pendingUTF8, chunk...)

	res := validateUTF8(u.pendingUTF8)
	if res.ok {
		out := u.inner.PushStr(string(u.pendingUTF8))
		u.pendingUTF8 = u.pendingUTF8[:0]
		return out, nil
	}

	if res.errorLen > 0 {
		u.pendingUTF8 = u.pendingUTF8[:oldLen]
		return StreamTextChunk[T]{}, newInvalidUtf8Error(res.validUpTo, res.errorLen)
	}

	validUpTo := res.validUpTo
	if validUpTo == 0 {
		return StreamTextChunk[T]{}, nil
	}

	prefix := u.pendingUTF8[:validUpTo]
	if pres := validateUTF8(prefix); !pres.ok {
		u.pendingUTF8 = u.pendingUTF8[:oldLen]
		return StreamTextChunk[T]{}, newInvalidUtf8Error(pres.validUpTo, pres.errorLen)
	}
	out := u.inner.PushStr(string(prefix))
	u.pendingUTF8 = u.pendingUTF8[validUpTo:]
	return out, nil
}

// Finish flushes buffered bytes into the wrapped parser and then flushes the
// wrapped parser itself. It errors if buffered bytes are invalid or form an
// incomplete trailing code point.
func (u *Utf8StreamParser[T]) Finish() (StreamTextChunk[T], error) {
	if len(u.pendingUTF8) > 0 {
		res := validateUTF8(u.pendingUTF8)
		if !res.ok {
			if res.errorLen > 0 {
				return StreamTextChunk[T]{}, newInvalidUtf8Error(res.validUpTo, res.errorLen)
			}
			return StreamTextChunk[T]{}, newIncompleteUtf8Error()
		}
	}

	var out StreamTextChunk[T]
	if len(u.pendingUTF8) > 0 {
		out = u.inner.PushStr(string(u.pendingUTF8))
		u.pendingUTF8 = u.pendingUTF8[:0]
	}

	tail := u.inner.Finish()
	out.VisibleText += tail.VisibleText
	out.Extracted = append(out.Extracted, tail.Extracted...)
	return out, nil
}

// IntoInner returns the wrapped parser if no undecoded UTF-8 bytes are buffered.
//
// Call [Utf8StreamParser.Finish] first if you want to flush buffered text into
// the wrapped parser.
func (u *Utf8StreamParser[T]) IntoInner() (StreamTextParser[T], error) {
	if len(u.pendingUTF8) == 0 {
		return u.inner, nil
	}
	res := validateUTF8(u.pendingUTF8)
	if res.ok {
		return u.inner, nil
	}
	if res.errorLen > 0 {
		return nil, newInvalidUtf8Error(res.validUpTo, res.errorLen)
	}
	return nil, newIncompleteUtf8Error()
}

// IntoInnerLossy returns the wrapped parser without validating or flushing
// buffered undecoded bytes. This may drop a partial UTF-8 code point that was
// buffered across chunk boundaries.
func (u *Utf8StreamParser[T]) IntoInnerLossy() StreamTextParser[T] {
	return u.inner
}
