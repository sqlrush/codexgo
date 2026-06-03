package cache

import "crypto/sha1"

// Sha1DigestSize is the length in bytes of a SHA-1 digest.
const Sha1DigestSize = sha1.Size

// Sha1Digest computes the SHA-1 digest of bytes.
//
// It is the Go equivalent of the Rust `sha1_digest`, returning the raw 20-byte
// digest. This is useful for content-based cache keys when you want to avoid
// staleness caused by path-only keys.
//
// The input slice is not mutated. Passing a nil slice hashes the empty input,
// matching the Rust function which accepts an empty byte slice.
func Sha1Digest(bytes []byte) [Sha1DigestSize]byte {
	return sha1.Sum(bytes)
}
