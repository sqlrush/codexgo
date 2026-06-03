// Package readiness provides a readiness-signaling primitive with token-based
// authorization and asynchronous waiting.
//
// It is a faithful Go port of the codex-utils-readiness Rust crate. The
// externally observable behavior is preserved: a flag starts not-ready,
// subscribers obtain opaque authorization tokens, and exactly one valid token
// (or the absence of any subscribers) transitions the flag to the ready state.
// Once ready, the flag is irreversible.
//
// The Rust original is built on Tokio primitives (a watch channel and an async
// Mutex with a lock-acquisition timeout). This port reproduces the same
// semantics using only the Go standard library: a closed channel broadcasts
// readiness to waiters, and a small channel-based mutex provides both
// non-blocking and timed lock acquisition so that the TokenLockFailed behavior
// is preserved.
package readiness

// Token is an opaque subscription token returned by Subscribe.
//
// Tokens are comparable value types and may be used as map keys, mirroring the
// Rust Token(i32) newtype which derives Hash and Eq.
type Token struct {
	id int32
}

// ID returns the underlying numeric identifier of the token.
//
// The zero token (id == 0) is reserved and is never handed out by Subscribe; it
// is therefore never authorized by MarkReady.
func (t Token) ID() int32 {
	return t.id
}
