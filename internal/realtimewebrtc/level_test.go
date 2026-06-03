package realtimewebrtc

import "testing"

func TestAudioLevelToPeak(t *testing.T) {
	tests := []struct {
		name  string
		level float64
		want  uint16
	}{
		{name: "silence", level: 0.0, want: 0},
		{name: "max", level: 1.0, want: 32767},
		{name: "half", level: 0.5, want: 16384}, // round(0.5*32767)=16384
		{name: "clamp negative", level: -0.5, want: 0},
		{name: "clamp over one", level: 1.5, want: 32767},
		{name: "small", level: 0.001, want: 33}, // round(0.001*32767)=33
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audioLevelToPeak(tt.level); got != tt.want {
				t.Fatalf("audioLevelToPeak(%v) = %d, want %d", tt.level, got, tt.want)
			}
		})
	}
}
