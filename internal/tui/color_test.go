package tui

import (
	"math"
	"testing"
)

func TestIsLight(t *testing.T) {
	tests := []struct {
		name string
		bg   RGB
		want bool
	}{
		{"black", RGB{0, 0, 0}, false},
		{"white", RGB{255, 255, 255}, true},
		{"midgray below threshold", RGB{128, 128, 128}, false},
		{"bright yellow", RGB{255, 255, 0}, true},
		{"dark blue", RGB{0, 0, 128}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLight(tt.bg); got != tt.want {
				t.Fatalf("IsLight(%v) = %v, want %v", tt.bg, got, tt.want)
			}
		})
	}
}

func TestBlend(t *testing.T) {
	fg := RGB{200, 100, 50}
	bg := RGB{0, 0, 0}
	if got := Blend(fg, bg, 1.0); got != fg {
		t.Fatalf("Blend alpha=1 = %v, want fg %v", got, fg)
	}
	if got := Blend(fg, bg, 0.0); got != bg {
		t.Fatalf("Blend alpha=0 = %v, want bg %v", got, bg)
	}
	half := Blend(RGB{100, 100, 100}, RGB{0, 0, 0}, 0.5)
	if half != (RGB{50, 50, 50}) {
		t.Fatalf("Blend alpha=0.5 = %v, want {50 50 50}", half)
	}
}

func TestPerceptualDistance(t *testing.T) {
	// Identical colors have zero distance.
	if d := PerceptualDistance(RGB{10, 20, 30}, RGB{10, 20, 30}); d != 0 {
		t.Fatalf("distance to self = %v, want 0", d)
	}
	// Black vs white is the largest lightness gap; distance should be large.
	dWhite := PerceptualDistance(RGB{0, 0, 0}, RGB{255, 255, 255})
	dGray := PerceptualDistance(RGB{0, 0, 0}, RGB{128, 128, 128})
	if dWhite <= dGray {
		t.Fatalf("expected black-white (%v) > black-gray (%v)", dWhite, dGray)
	}
	if math.IsNaN(dWhite) {
		t.Fatalf("distance is NaN")
	}
}
