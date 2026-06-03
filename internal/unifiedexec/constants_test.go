package unifiedexec

import (
	"reflect"
	"testing"
)

func TestApplyUnifiedExecEnv(t *testing.T) {
	t.Run("injects defaults", func(t *testing.T) {
		env := applyUnifiedExecEnv(map[string]string{})
		want := map[string]string{
			"NO_COLOR":  "1",
			"TERM":      "dumb",
			"LANG":      "C.UTF-8",
			"LC_CTYPE":  "C.UTF-8",
			"LC_ALL":    "C.UTF-8",
			"COLORTERM": "",
			"PAGER":     "cat",
			"GIT_PAGER": "cat",
			"GH_PAGER":  "cat",
			"CODEX_CI":  "1",
		}
		if !reflect.DeepEqual(env, want) {
			t.Fatalf("env = %v, want %v", env, want)
		}
	})

	t.Run("overrides existing and preserves unrelated", func(t *testing.T) {
		base := map[string]string{"NO_COLOR": "0", "PATH": "/usr/bin"}
		env := applyUnifiedExecEnv(base)
		if env["NO_COLOR"] != "1" {
			t.Fatalf("NO_COLOR = %q, want 1", env["NO_COLOR"])
		}
		if env["PATH"] != "/usr/bin" {
			t.Fatalf("PATH = %q, want /usr/bin", env["PATH"])
		}
		// Input map must not be mutated (immutability).
		if base["NO_COLOR"] != "0" {
			t.Fatalf("input mutated: NO_COLOR = %q, want 0", base["NO_COLOR"])
		}
	})
}

func TestClampYieldTime(t *testing.T) {
	tests := []struct {
		name string
		in   uint64
		want uint64
	}{
		{"below min", 10, MinYieldTimeMS},
		{"at min", MinYieldTimeMS, MinYieldTimeMS},
		{"middle", 5000, 5000},
		{"at max", MaxYieldTimeMS, MaxYieldTimeMS},
		{"above max", 100_000, MaxYieldTimeMS},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampYieldTime(tt.in); got != tt.want {
				t.Fatalf("clampYieldTime(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveMaxTokens(t *testing.T) {
	if got := resolveMaxTokens(nil); got != DefaultMaxOutputTokens {
		t.Fatalf("resolveMaxTokens(nil) = %d, want %d", got, DefaultMaxOutputTokens)
	}
	v := 42
	if got := resolveMaxTokens(&v); got != 42 {
		t.Fatalf("resolveMaxTokens(&42) = %d, want 42", got)
	}
}

func TestGenerateChunkID(t *testing.T) {
	id := generateChunkID()
	if len(id) != 6 {
		t.Fatalf("len(id) = %d, want 6", len(id))
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("id %q contains non-hex char %q", id, c)
		}
	}
	// Two calls should (almost always) differ; assert they are valid length at least.
	if id2 := generateChunkID(); len(id2) != 6 {
		t.Fatalf("second id length = %d, want 6", len(id2))
	}
}
