package tui

import (
	"math"

	"github.com/charmbracelet/lipgloss"
)

// StdoutColorLevel is the detected color depth of the output terminal.
//
// Port of codex-rs/tui/src/terminal_palette.rs `StdoutColorLevel`.
type StdoutColorLevel int

const (
	// ColorLevelUnknown means no color support could be detected.
	ColorLevelUnknown StdoutColorLevel = iota
	// ColorLevelAnsi16 supports the 16 standard ANSI colors.
	ColorLevelAnsi16
	// ColorLevelAnsi256 supports the 256-color indexed palette.
	ColorLevelAnsi256
	// ColorLevelTrueColor supports 24-bit RGB color.
	ColorLevelTrueColor
)

// String renders the color level for logging.
func (l StdoutColorLevel) String() string {
	switch l {
	case ColorLevelTrueColor:
		return "truecolor"
	case ColorLevelAnsi256:
		return "ansi256"
	case ColorLevelAnsi16:
		return "ansi16"
	default:
		return "unknown"
	}
}

// BestColor returns the closest color the terminal can display for the supplied
// truecolor target, given the detected color level. On a truecolor terminal the
// target is returned verbatim; on a 256-color terminal the nearest xterm fixed
// color index is chosen by perceptual distance; otherwise no color is applied.
//
// Port of codex-rs/tui/src/terminal_palette.rs `best_color`, parameterized on
// the color level so callers (and tests) can supply a fixed capability.
func BestColor(target RGB, level StdoutColorLevel) lipgloss.TerminalColor {
	switch level {
	case ColorLevelTrueColor:
		return RGBColor(target)
	case ColorLevelAnsi256:
		idx := nearestXtermIndex(target)
		return lipgloss.Color(itoa(idx))
	default:
		return lipgloss.NoColor{}
	}
}

// RGBColor converts an RGB to a lipgloss truecolor value (hex form).
func RGBColor(c RGB) lipgloss.Color {
	return lipgloss.Color(hexColor(c))
}

// IndexedColor converts a palette index to a lipgloss indexed color.
func IndexedColor(index uint8) lipgloss.Color {
	return lipgloss.Color(itoa(int(index)))
}

// nearestXtermIndex returns the xterm fixed-color index (16..=255) nearest to
// target by perceptual distance, mirroring the Rust `best_color` Ansi256 path
// which only considers indices >= 16 (the first 16 vary per theme).
func nearestXtermIndex(target RGB) int {
	best := 16
	bestDist := math.MaxFloat64
	for i := 16; i < len(XtermColors); i++ {
		d := PerceptualDistance(XtermColors[i], target)
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

// hexColor renders an RGB as a "#rrggbb" hex string.
func hexColor(c RGB) string {
	const hexDigits = "0123456789abcdef"
	out := []byte{'#', 0, 0, 0, 0, 0, 0}
	out[1] = hexDigits[c.R>>4]
	out[2] = hexDigits[c.R&0xf]
	out[3] = hexDigits[c.G>>4]
	out[4] = hexDigits[c.G&0xf]
	out[5] = hexDigits[c.B>>4]
	out[6] = hexDigits[c.B&0xf]
	return string(out)
}

// itoa renders a small non-negative int without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// XtermColors is the canonical xterm 256-color palette. The first 16 entries
// vary by terminal theme; entries 16..=255 are consistent across most
// terminals and are the only ones used by [BestColor].
//
// Port of codex-rs/tui/src/terminal_palette.rs `XTERM_COLORS`.
var XtermColors = [256]RGB{
	{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
	{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
	{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
	{0, 0, 0}, {0, 0, 95}, {0, 0, 135}, {0, 0, 175},
	{0, 0, 215}, {0, 0, 255}, {0, 95, 0}, {0, 95, 95},
	{0, 95, 135}, {0, 95, 175}, {0, 95, 215}, {0, 95, 255},
	{0, 135, 0}, {0, 135, 95}, {0, 135, 135}, {0, 135, 175},
	{0, 135, 215}, {0, 135, 255}, {0, 175, 0}, {0, 175, 95},
	{0, 175, 135}, {0, 175, 175}, {0, 175, 215}, {0, 175, 255},
	{0, 215, 0}, {0, 215, 95}, {0, 215, 135}, {0, 215, 175},
	{0, 215, 215}, {0, 215, 255}, {0, 255, 0}, {0, 255, 95},
	{0, 255, 135}, {0, 255, 175}, {0, 255, 215}, {0, 255, 255},
	{95, 0, 0}, {95, 0, 95}, {95, 0, 135}, {95, 0, 175},
	{95, 0, 215}, {95, 0, 255}, {95, 95, 0}, {95, 95, 95},
	{95, 95, 135}, {95, 95, 175}, {95, 95, 215}, {95, 95, 255},
	{95, 135, 0}, {95, 135, 95}, {95, 135, 135}, {95, 135, 175},
	{95, 135, 215}, {95, 135, 255}, {95, 175, 0}, {95, 175, 95},
	{95, 175, 135}, {95, 175, 175}, {95, 175, 215}, {95, 175, 255},
	{95, 215, 0}, {95, 215, 95}, {95, 215, 135}, {95, 215, 175},
	{95, 215, 215}, {95, 215, 255}, {95, 255, 0}, {95, 255, 95},
	{95, 255, 135}, {95, 255, 175}, {95, 255, 215}, {95, 255, 255},
	{135, 0, 0}, {135, 0, 95}, {135, 0, 135}, {135, 0, 175},
	{135, 0, 215}, {135, 0, 255}, {135, 95, 0}, {135, 95, 95},
	{135, 95, 135}, {135, 95, 175}, {135, 95, 215}, {135, 95, 255},
	{135, 135, 0}, {135, 135, 95}, {135, 135, 135}, {135, 135, 175},
	{135, 135, 215}, {135, 135, 255}, {135, 175, 0}, {135, 175, 95},
	{135, 175, 135}, {135, 175, 175}, {135, 175, 215}, {135, 175, 255},
	{135, 215, 0}, {135, 215, 95}, {135, 215, 135}, {135, 215, 175},
	{135, 215, 215}, {135, 215, 255}, {135, 255, 0}, {135, 255, 95},
	{135, 255, 135}, {135, 255, 175}, {135, 255, 215}, {135, 255, 255},
	{175, 0, 0}, {175, 0, 95}, {175, 0, 135}, {175, 0, 175},
	{175, 0, 215}, {175, 0, 255}, {175, 95, 0}, {175, 95, 95},
	{175, 95, 135}, {175, 95, 175}, {175, 95, 215}, {175, 95, 255},
	{175, 135, 0}, {175, 135, 95}, {175, 135, 135}, {175, 135, 175},
	{175, 135, 215}, {175, 135, 255}, {175, 175, 0}, {175, 175, 95},
	{175, 175, 135}, {175, 175, 175}, {175, 175, 215}, {175, 175, 255},
	{175, 215, 0}, {175, 215, 95}, {175, 215, 135}, {175, 215, 175},
	{175, 215, 215}, {175, 215, 255}, {175, 255, 0}, {175, 255, 95},
	{175, 255, 135}, {175, 255, 175}, {175, 255, 215}, {175, 255, 255},
	{215, 0, 0}, {215, 0, 95}, {215, 0, 135}, {215, 0, 175},
	{215, 0, 215}, {215, 0, 255}, {215, 95, 0}, {215, 95, 95},
	{215, 95, 135}, {215, 95, 175}, {215, 95, 215}, {215, 95, 255},
	{215, 135, 0}, {215, 135, 95}, {215, 135, 135}, {215, 135, 175},
	{215, 135, 215}, {215, 135, 255}, {215, 175, 0}, {215, 175, 95},
	{215, 175, 135}, {215, 175, 175}, {215, 175, 215}, {215, 175, 255},
	{215, 215, 0}, {215, 215, 95}, {215, 215, 135}, {215, 215, 175},
	{215, 215, 215}, {215, 215, 255}, {215, 255, 0}, {215, 255, 95},
	{215, 255, 135}, {215, 255, 175}, {215, 255, 215}, {215, 255, 255},
	{255, 0, 0}, {255, 0, 95}, {255, 0, 135}, {255, 0, 175},
	{255, 0, 215}, {255, 0, 255}, {255, 95, 0}, {255, 95, 95},
	{255, 95, 135}, {255, 95, 175}, {255, 95, 215}, {255, 95, 255},
	{255, 135, 0}, {255, 135, 95}, {255, 135, 135}, {255, 135, 175},
	{255, 135, 215}, {255, 135, 255}, {255, 175, 0}, {255, 175, 95},
	{255, 175, 135}, {255, 175, 175}, {255, 175, 215}, {255, 175, 255},
	{255, 215, 0}, {255, 215, 95}, {255, 215, 135}, {255, 215, 175},
	{255, 215, 215}, {255, 215, 255}, {255, 255, 0}, {255, 255, 95},
	{255, 255, 135}, {255, 255, 175}, {255, 255, 215}, {255, 255, 255},
	{8, 8, 8}, {18, 18, 18}, {28, 28, 28}, {38, 38, 38},
	{48, 48, 48}, {58, 58, 58}, {68, 68, 68}, {78, 78, 78},
	{88, 88, 88}, {98, 98, 98}, {108, 108, 108}, {118, 118, 118},
	{128, 128, 128}, {138, 138, 138}, {148, 148, 148}, {158, 158, 158},
	{168, 168, 168}, {178, 178, 178}, {188, 188, 188}, {198, 198, 198},
	{208, 208, 208}, {218, 218, 218}, {228, 228, 228}, {238, 238, 238},
}
