package plugins

import "testing"

func TestComparePluginVersions(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  int // sign
	}{
		{"equal", "1.2.3", "1.2.3", 0},
		{"major", "2.0.0", "1.9.9", 1},
		{"minor", "1.3.0", "1.2.9", 1},
		{"patch", "1.2.4", "1.2.3", 1},
		{"prerelease lower than release", "1.0.0-alpha", "1.0.0", -1},
		{"prerelease numeric vs alpha", "1.0.0-1", "1.0.0-alpha", -1},
		{"prerelease alpha order", "1.0.0-alpha", "1.0.0-beta", -1},
		{"prerelease length", "1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"build metadata ignored", "1.0.0+build1", "1.0.0+build2", 0},
		{"prerelease numeric ordering", "1.0.0-2", "1.0.0-10", -1},
		{"non-semver byte fallback equal", "local", "local", 0},
		{"non-semver byte fallback order", "abc", "abd", -1},
		{"semver vs non-semver byte fallback", "1.0.0", "local", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comparePluginVersions(tt.left, tt.right)
			if sign(got) != tt.want {
				t.Fatalf("compare(%q,%q)=%d, want sign %d", tt.left, tt.right, got, tt.want)
			}
			// Anti-symmetry.
			rev := comparePluginVersions(tt.right, tt.left)
			if sign(rev) != -tt.want {
				t.Fatalf("reverse compare not anti-symmetric: %d vs %d", got, rev)
			}
		})
	}
}

func TestParseSemverRejectsInvalid(t *testing.T) {
	invalid := []string{"1.2", "1.2.3.4", "01.2.3", "1.2.x", "", "v1.2.3", "1.2.3-"}
	for _, v := range invalid {
		if _, ok := parseSemver(v); ok {
			t.Errorf("expected %q to be invalid semver", v)
		}
	}
}

func TestParseSemverAcceptsValid(t *testing.T) {
	valid := []string{"0.0.0", "1.2.3", "1.2.3-beta", "1.2.3-beta.1", "1.2.3+build", "1.2.3-rc.1+build.7"}
	for _, v := range valid {
		if _, ok := parseSemver(v); !ok {
			t.Errorf("expected %q to be valid semver", v)
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
