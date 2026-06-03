package memories

import (
	"math"
	"os"
	"strconv"
)

// modeSymlink aliases os.ModeSymlink for terse symlink checks.
const modeSymlink = os.ModeSymlink

// parseCursor parses an optional pagination cursor as a non-negative integer,
// mirroring the cursor parsing shared by list and search. A nil cursor yields 0.
func parseCursor(cursor *string) (int, error) {
	if cursor == nil {
		return 0, nil
	}
	value, err := strconv.Atoi(*cursor)
	if err != nil || value < 0 {
		return 0, errInvalidCursor(*cursor, "must be a non-negative integer")
	}
	return value, nil
}

// derefOr returns *p or fallback when p is nil.
func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

// saturatingAdd returns a+b, saturating at math.MaxInt instead of overflowing,
// mirroring Rust's usize::saturating_add.
func saturatingAdd(a, b int) int {
	if b > 0 && a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}
