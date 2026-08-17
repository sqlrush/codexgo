package skills

import "testing"

func TestBuildSkillNameCounts(t *testing.T) {
	skills := []SkillMetadata{
		makeSkill("Alpha", "/tmp/a"),
		makeSkill("alpha", "/tmp/b"),
		makeSkill("Beta", "/tmp/c"),
		makeSkill("Gamma", "/tmp/d"),
	}

	t.Run("no disabled", func(t *testing.T) {
		exact, lower := BuildSkillNameCounts(skills, nil)
		if exact["Alpha"] != 1 || exact["alpha"] != 1 || exact["Beta"] != 1 {
			t.Fatalf("exact counts wrong: %v", exact)
		}
		if lower["alpha"] != 2 {
			t.Fatalf("lower[alpha] = %d, want 2", lower["alpha"])
		}
		if lower["beta"] != 1 || lower["gamma"] != 1 {
			t.Fatalf("lower counts wrong: %v", lower)
		}
	})

	t.Run("excludes disabled", func(t *testing.T) {
		disabled := disabledSet("/tmp/d")
		exact, lower := BuildSkillNameCounts(skills, disabled)
		if _, ok := exact["Gamma"]; ok {
			t.Fatalf("disabled skill counted in exact: %v", exact)
		}
		if _, ok := lower["gamma"]; ok {
			t.Fatalf("disabled skill counted in lower: %v", lower)
		}
		if lower["alpha"] != 2 {
			t.Fatalf("lower[alpha] = %d, want 2", lower["alpha"])
		}
	})
}

func TestAsciiLower(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ABC", "abc"},
		{"MixedCase123", "mixedcase123"},
		{"already-lower", "already-lower"},
		{"WITH:COLON", "with:colon"},
	}
	for _, tt := range tests {
		if got := asciiLower(tt.in); got != tt.want {
			t.Errorf("asciiLower(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
