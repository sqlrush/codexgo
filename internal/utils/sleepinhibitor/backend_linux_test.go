//go:build linux

package sleepinhibitor

import (
	"math"
	"strconv"
	"testing"
)

func TestBlockerSleepSecondsIsInt32Max(t *testing.T) {
	t.Parallel()
	want := strconv.Itoa(math.MaxInt32)
	if blockerSleepSeconds != want {
		t.Errorf("blockerSleepSeconds = %q, want %q", blockerSleepSeconds, want)
	}
}

func TestBackendOrderPreference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		preferred *linuxBackendKind
		want      []linuxBackendKind
	}{
		{
			name:      "no preference defaults to systemd first",
			preferred: nil,
			want:      []linuxBackendKind{backendSystemdInhibit, backendGnomeSessionInhibit},
		},
		{
			name:      "systemd preference keeps systemd first",
			preferred: kindPtr(backendSystemdInhibit),
			want:      []linuxBackendKind{backendSystemdInhibit, backendGnomeSessionInhibit},
		},
		{
			name:      "gnome preference puts gnome first",
			preferred: kindPtr(backendGnomeSessionInhibit),
			want:      []linuxBackendKind{backendGnomeSessionInhibit, backendSystemdInhibit},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := &linuxBackend{preferredBackend: tc.preferred}
			got := b.backendOrder()
			if len(got) != len(tc.want) {
				t.Fatalf("backendOrder() len = %d, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("backendOrder()[%d] = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func kindPtr(k linuxBackendKind) *linuxBackendKind {
	return &k
}
