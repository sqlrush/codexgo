package elapsed

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		dur  time.Duration
		want string
	}{
		// Sub-second: rendered in milliseconds with no decimals.
		{name: "subsecond 250ms", dur: 250 * time.Millisecond, want: "250ms"},
		{name: "zero", dur: 0, want: "0ms"},
		{name: "just under one second", dur: 999 * time.Millisecond, want: "999ms"},

		// Seconds (>= 1s, < 60s): 2-decimal-place seconds.
		{name: "one and a half seconds", dur: 1500 * time.Millisecond, want: "1.50s"},
		{name: "exactly one second", dur: 1000 * time.Millisecond, want: "1.00s"},
		{name: "59.999s rounds to 60.00s", dur: 59_999 * time.Millisecond, want: "60.00s"},
		{name: "30.005s rounds half to even", dur: 30_005 * time.Millisecond, want: "30.00s"},

		// Minutes (>= 60s): mm ss form with zero-padded seconds.
		{name: "exactly one minute", dur: 60_000 * time.Millisecond, want: "1m 00s"},
		{name: "one minute fifteen", dur: 75_000 * time.Millisecond, want: "1m 15s"},
		{name: "sixty minutes one second", dur: 3_601_000 * time.Millisecond, want: "60m 01s"},
		{name: "one hour has space", dur: 3_600_000 * time.Millisecond, want: "60m 00s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.dur)
			if got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.dur, got, tt.want)
			}
		})
	}
}

// TestFormatDurationDoesNotMutate is a sanity check that the input value is not
// altered (time.Duration is a value type, so this documents the immutability
// guarantee for callers).
func TestFormatDurationDoesNotMutate(t *testing.T) {
	dur := 1500 * time.Millisecond
	original := dur
	_ = FormatDuration(dur)
	if dur != original {
		t.Errorf("input duration was mutated: got %v, want %v", dur, original)
	}
}

func TestFormatDurationNegative(t *testing.T) {
	// Negative durations fall into the sub-second branch by magnitude and
	// render with a leading minus sign, matching the signed-millis cast in the
	// reference implementation.
	got := FormatDuration(-5 * time.Millisecond)
	if want := "-5ms"; got != want {
		t.Errorf("FormatDuration(-5ms) = %q, want %q", got, want)
	}
}
