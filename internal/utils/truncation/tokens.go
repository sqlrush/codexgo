// Package truncation provides helpers for truncating tool and exec output
// using a TruncationPolicy. It is a faithful, drop-in-compatible Go
// reimplementation of the OpenAI Codex Rust crate
// codex-utils-output-truncation (codex 0.136.0), preserving the same
// externally observable behavior and output formats byte-for-byte.
//
// The package is self-contained and depends only on the Go standard library.
// The token/byte approximation and middle-truncation helpers mirror the
// behavior of the upstream codex-utils-string crate that the Rust crate
// re-exports.
package truncation

// approxBytesPerToken is the heuristic used to convert between approximate
// token counts and byte counts. It mirrors APPROX_BYTES_PER_TOKEN in the Rust
// codex-utils-string crate.
const approxBytesPerToken = 4

// ApproxTokenCount returns the approximate number of tokens represented by the
// given text, computed as ceil(len(text) / approxBytesPerToken) using the raw
// UTF-8 byte length.
//
// This mirrors codex_utils_string::approx_token_count and uses saturating
// arithmetic to avoid overflow on extremely large inputs.
func ApproxTokenCount(text string) int {
	return ceilDivInt(len(text), approxBytesPerToken)
}

// ApproxBytesForTokens returns the approximate number of bytes corresponding to
// the given token count (tokens * approxBytesPerToken), saturating on overflow.
//
// This mirrors codex_utils_string::approx_bytes_for_tokens.
func ApproxBytesForTokens(tokens int) int {
	return saturatingMulInt(tokens, approxBytesPerToken)
}

// ApproxTokensFromByteCount returns the approximate number of tokens for a byte
// count, computed as ceil(bytes / approxBytesPerToken). It mirrors
// codex_utils_string::approx_tokens_from_byte_count, which operates on u64 and
// returns a u64. Here we model the value as uint64 to match the unsigned
// semantics of the upstream implementation.
func ApproxTokensFromByteCount(bytes uint64) uint64 {
	return ceilDivUint64(bytes, approxBytesPerToken)
}

// ApproxTokensFromByteCountI64 converts a signed byte count into an approximate
// token count, clamping non-positive inputs to 0. It mirrors
// codex-utils-output-truncation::approx_tokens_from_byte_count_i64.
//
// Negative or zero byte counts yield 0. Otherwise the result is the approximate
// token count, clamped to the maximum int64 value on overflow.
func ApproxTokensFromByteCountI64(bytes int64) int64 {
	if bytes <= 0 {
		return 0
	}

	tokens := ApproxTokensFromByteCount(uint64(bytes))
	const maxInt64 = uint64(1<<63 - 1)
	if tokens > maxInt64 {
		return 1<<63 - 1
	}
	return int64(tokens)
}

// ceilDivInt returns ceil(n / d) for non-negative n and positive d, using
// saturating addition so that the intermediate (n + d - 1) cannot overflow.
func ceilDivInt(n, d int) int {
	if n <= 0 {
		return 0
	}
	return saturatingAddInt(n, d-1) / d
}

// ceilDivUint64 returns ceil(n / d) for d > 0 using saturating addition to
// match the saturating_add semantics of the Rust implementation.
func ceilDivUint64(n uint64, d uint64) uint64 {
	const maxUint64 = ^uint64(0)
	add := d - 1
	if n > maxUint64-add {
		n = maxUint64
	} else {
		n += add
	}
	return n / d
}

// saturatingAddInt returns a + b, clamped to the int range on overflow.
func saturatingAddInt(a, b int) int {
	const maxInt = int(^uint(0) >> 1)
	const minInt = -maxInt - 1
	sum := a + b
	// Detect overflow: if a and b share a sign that differs from the sum's.
	if (a > 0 && b > 0 && sum < 0) || (a > maxInt-b && b > 0) {
		return maxInt
	}
	if (a < 0 && b < 0 && sum >= 0) || (a < minInt-b && b < 0) {
		return minInt
	}
	return sum
}

// saturatingMulInt returns a * b, clamped to the int range on overflow.
func saturatingMulInt(a, b int) int {
	const maxInt = int(^uint(0) >> 1)
	const minInt = -maxInt - 1
	if a == 0 || b == 0 {
		return 0
	}
	product := a * b
	// Verify by division; if it does not round-trip, an overflow occurred.
	if product/b != a {
		// Determine the sign of the true result to clamp correctly.
		if (a > 0) == (b > 0) {
			return maxInt
		}
		return minInt
	}
	return product
}
