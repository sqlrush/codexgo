package core

import "testing"

// TestShouldAutoCompact locks the pre-sampling auto-compaction trigger: only a
// positive, configured limit that the running total has reached fires it.
func TestShouldAutoCompact(t *testing.T) {
	lim := func(v int64) *int64 { return &v }
	cases := []struct {
		name  string
		total int64
		limit *int64
		want  bool
	}{
		{"no limit configured", 1_000_000, nil, false},
		{"zero limit disables", 1_000_000, lim(0), false},
		{"negative limit disables", 1_000_000, lim(-1), false},
		{"under limit", 5_000, lim(10_000), false},
		{"exactly at limit", 10_000, lim(10_000), true},
		{"over limit", 12_000, lim(10_000), true},
		{"zero usage under limit", 0, lim(10_000), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoCompact(tc.total, tc.limit); got != tc.want {
				t.Errorf("shouldAutoCompact(%d, %v) = %v, want %v", tc.total, tc.limit, got, tc.want)
			}
		})
	}
}
