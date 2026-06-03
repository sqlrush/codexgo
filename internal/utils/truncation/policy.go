package truncation

// PolicyMode identifies which kind of budget a TruncationPolicy enforces.
type PolicyMode int

const (
	// ModeBytes truncates based on a raw byte budget.
	ModeBytes PolicyMode = iota
	// ModeTokens truncates based on an approximate token budget.
	ModeTokens
)

// TruncationPolicy describes how output should be truncated: either by a byte
// budget or by an approximate token budget. It mirrors
// codex_protocol::protocol::TruncationPolicy.
//
// Construct values with BytesPolicy or TokensPolicy. The zero value is a Bytes
// policy with a limit of 0, which truncates all content to the truncation
// marker.
type TruncationPolicy struct {
	mode  PolicyMode
	limit int
}

// BytesPolicy returns a TruncationPolicy that enforces a byte budget.
//
// Negative limits are clamped to 0 to match the unsigned (usize) semantics of
// the upstream Rust implementation, which never accepts negative budgets.
func BytesPolicy(bytes int) TruncationPolicy {
	if bytes < 0 {
		bytes = 0
	}
	return TruncationPolicy{mode: ModeBytes, limit: bytes}
}

// TokensPolicy returns a TruncationPolicy that enforces an approximate token
// budget.
//
// Negative limits are clamped to 0 to match the unsigned (usize) semantics of
// the upstream Rust implementation.
func TokensPolicy(tokens int) TruncationPolicy {
	if tokens < 0 {
		tokens = 0
	}
	return TruncationPolicy{mode: ModeTokens, limit: tokens}
}

// Mode reports whether the policy is byte- or token-based.
func (p TruncationPolicy) Mode() PolicyMode {
	return p.mode
}

// Limit returns the raw limit value (bytes or tokens, depending on Mode).
func (p TruncationPolicy) Limit() int {
	return p.limit
}

// TokenBudget returns the approximate token budget for the policy. For a Bytes
// policy it converts the byte limit into an approximate token count; for a
// Tokens policy it returns the token limit directly.
//
// This mirrors TruncationPolicy::token_budget.
func (p TruncationPolicy) TokenBudget() int {
	switch p.mode {
	case ModeBytes:
		tokens := ApproxTokensFromByteCount(uint64(p.limit))
		const maxInt = uint64(^uint(0) >> 1)
		if tokens > maxInt {
			return int(^uint(0) >> 1)
		}
		return int(tokens)
	default: // ModeTokens
		return p.limit
	}
}

// ByteBudget returns the byte budget for the policy. For a Bytes policy it
// returns the byte limit directly; for a Tokens policy it converts the token
// limit into an approximate byte count.
//
// This mirrors TruncationPolicy::byte_budget.
func (p TruncationPolicy) ByteBudget() int {
	switch p.mode {
	case ModeBytes:
		return p.limit
	default: // ModeTokens
		return ApproxBytesForTokens(p.limit)
	}
}
