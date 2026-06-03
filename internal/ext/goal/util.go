package goal

import "math"

// maxInt64 is the maximum representable int64 value, mirroring Rust `i64::MAX`.
const maxInt64 = math.MaxInt64

// saturatingSubI64 returns a-b clamped to [0, maxInt64], mirroring Rust's
// `i64::saturating_sub` on values that the goal accounting treats as
// non-negative. Negative results saturate to 0.
func saturatingSubI64(a, b int64) int64 {
	diff := a - b
	// Detect signed overflow on subtraction.
	if (b < 0 && a > maxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		if b > 0 {
			return math.MinInt64
		}
		return maxInt64
	}
	return diff
}

// saturatingAddI64 returns a+b clamped to [MinInt64, maxInt64], mirroring Rust's
// `i64::saturating_add`.
func saturatingAddI64(a, b int64) int64 {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		if b > 0 {
			return maxInt64
		}
		return math.MinInt64
	}
	return sum
}

// maxI64 returns the larger of a and b.
func maxI64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ptrEqStr reports whether ptr points to a string equal to value.
func ptrEqStr(ptr *string, value string) bool {
	return ptr != nil && *ptr == value
}

// cloneStringPtr returns a copy of the pointed-to string, or nil.
func cloneStringPtr(ptr *string) *string {
	if ptr == nil {
		return nil
	}
	out := *ptr
	return &out
}
