package protocol

import (
	"math"
	"strconv"
)

// AbsolutePath is a path that, in Rust, is guaranteed absolute by construction
// (`codex_utils_absolute_path::AbsolutePathBuf`). On the wire it is encoded as a
// bare path string, so for drop-in JSON compatibility we model it as a string.
//
// We deliberately do NOT re-validate absoluteness here: the Go port treats this
// as a transparent string alias so round-tripping never rejects values that the
// Rust side produced.
type AbsolutePath string

// String returns the path as a string.
func (p AbsolutePath) String() string { return string(p) }

// formatWithSeparators formats an int64 with en-US style grouping separators
// (e.g. 12345 -> "12,345").
//
// The Rust implementation is locale-aware via ICU; for determinism and because
// the wire protocol never carries pre-formatted numbers, this port implements
// the en-US fallback that the Rust code uses when no system locale is found.
func formatWithSeparators(n int64) string {
	neg := n < 0
	digits := strconv.FormatInt(n, 10)
	if neg {
		digits = digits[1:]
	}

	var out []byte
	rem := len(digits) % 3
	if rem > 0 {
		out = append(out, digits[:rem]...)
	}
	for i := rem; i < len(digits); i += 3 {
		if len(out) > 0 {
			out = append(out, ',')
		}
		out = append(out, digits[i:i+3]...)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// FormatWithSeparators formats an int64 with grouping separators (en-US).
func FormatWithSeparators(n int64) string { return formatWithSeparators(n) }

// FormatSISuffix formats token counts to 3 significant figures using base-10 SI
// suffixes (K, M, G), matching Rust `format_si_suffix`.
//
// Examples (en-US):
//   - 999 -> "999"
//   - 1200 -> "1.20K"
//   - 123456789 -> "123M"
func FormatSISuffix(n int64) string {
	if n < 0 {
		n = 0
	}
	if n < 1000 {
		return formatWithSeparators(n)
	}

	formatScaled := func(value int64, scale int64, fracDigits int) string {
		v := float64(value) / float64(scale)
		return strconv.FormatFloat(roundTo(v, fracDigits), 'f', fracDigits, 64)
	}

	type unit struct {
		scale  int64
		suffix string
	}
	units := []unit{
		{1_000, "K"},
		{1_000_000, "M"},
		{1_000_000_000, "G"},
	}

	f := float64(n)
	for _, u := range units {
		scale := float64(u.scale)
		switch {
		case math.Round(100.0*f/scale) < 1000.0:
			return formatScaled(n, u.scale, 2) + u.suffix
		case math.Round(10.0*f/scale) < 1000.0:
			return formatScaled(n, u.scale, 1) + u.suffix
		case math.Round(f/scale) < 1000.0:
			return formatScaled(n, u.scale, 0) + u.suffix
		}
	}

	// Above 1000G, keep whole-G precision.
	g := int64(math.Round(float64(n) / 1e9))
	return formatWithSeparators(g) + "G"
}

func roundTo(v float64, fracDigits int) float64 {
	pow := math.Pow(10, float64(fracDigits))
	return math.Round(v*pow) / pow
}
