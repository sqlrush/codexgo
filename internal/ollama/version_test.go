package ollama

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Version
		wantErr bool
	}{
		{"plain", "0.14.1", NewVersion(0, 14, 1), false},
		{"leading v", "v0.14.1", NewVersion(0, 14, 1), false},
		{"surrounding space", "  0.13.4 ", NewVersion(0, 13, 4), false},
		{"prerelease dropped", "0.14.0-rc.1", NewVersion(0, 14, 0), false},
		{"build metadata dropped", "0.14.0+build.5", NewVersion(0, 14, 0), false},
		{"prerelease and build", "1.2.3-alpha+meta", NewVersion(1, 2, 3), false},
		{"two components", "0.14", Version{}, true},
		{"empty", "", Version{}, true},
		{"non-numeric", "a.b.c", Version{}, true},
		{"empty component", "0..1", Version{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseVersion(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseVersion(%q) = %v, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseVersion(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		name string
		a    Version
		b    Version
		want int
	}{
		{"equal", NewVersion(1, 2, 3), NewVersion(1, 2, 3), 0},
		{"major less", NewVersion(0, 9, 9), NewVersion(1, 0, 0), -1},
		{"major greater", NewVersion(2, 0, 0), NewVersion(1, 9, 9), 1},
		{"minor less", NewVersion(0, 13, 3), NewVersion(0, 13, 4), -1},
		{"patch greater", NewVersion(0, 13, 5), NewVersion(0, 13, 4), 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Compare(tc.b); got != tc.want {
				t.Errorf("%v.Compare(%v) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	if got := NewVersion(0, 13, 4).String(); got != "0.13.4" {
		t.Errorf("String() = %q, want %q", got, "0.13.4")
	}
}
