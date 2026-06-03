package hooks

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func sp(s string) *string { return &s }

func TestMatchesMatcher(t *testing.T) {
	tests := []struct {
		name    string
		matcher *string
		input   *string
		want    bool
	}{
		{"nil matches all", nil, sp("Bash"), true},
		{"star matches all", sp("*"), sp("Bash"), true},
		{"empty matches all", sp(""), sp("Bash"), true},
		{"exact pipe edit", sp("Edit|Write"), sp("Edit"), true},
		{"exact pipe write", sp("Edit|Write"), sp("Write"), true},
		{"exact pipe miss", sp("Edit|Write"), sp("Bash"), false},
		{"literal exact", sp("Bash"), sp("Bash"), true},
		{"literal exact prefix miss", sp("Bash"), sp("BashOutput"), false},
		{"mcp exact", sp("mcp__memory__create_entities"), sp("mcp__memory__create_entities"), true},
		{"mcp exact partial miss", sp("mcp__memory"), sp("mcp__memory__create_entities"), false},
		{"regex prefix", sp("^Bash"), sp("BashOutput"), true},
		{"regex wildcard", sp("mcp__memory__.*"), sp("mcp__memory__create_entities"), true},
		{"regex anchored", sp("^Bash$"), sp("Bash"), true},
		{"regex anchored miss", sp("^Bash$"), sp("BashOutput"), false},
		{"invalid regex no match", sp("["), sp("Bash"), false},
		{"exact nil input", sp("Bash"), nil, false},
		{"regex nil input", sp("^Bash"), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesMatcher(tt.matcher, tt.input); got != tt.want {
				t.Fatalf("MatchesMatcher(%v, %v) = %v, want %v", tt.matcher, tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateMatcherPattern(t *testing.T) {
	valid := []string{"*", "", "Edit|Write", "mcp__memory", "^Bash", "mcp__memory__.*", "^Bash$"}
	for _, m := range valid {
		if err := ValidateMatcherPattern(m); err != nil {
			t.Errorf("ValidateMatcherPattern(%q) unexpected error: %v", m, err)
		}
	}
	if err := ValidateMatcherPattern("["); err == nil {
		t.Errorf("ValidateMatcherPattern(\"[\") expected error")
	}
}

func TestMatcherPatternForEvent(t *testing.T) {
	if got := MatcherPatternForEvent(protocol.HookEventNameUserPromptSubmit, sp("^hello")); got != nil {
		t.Errorf("UserPromptSubmit should ignore matcher, got %v", got)
	}
	if got := MatcherPatternForEvent(protocol.HookEventNameStop, sp("^done$")); got != nil {
		t.Errorf("Stop should ignore matcher, got %v", got)
	}
	if got := MatcherPatternForEvent(protocol.HookEventNamePreToolUse, sp("Bash")); got == nil || *got != "Bash" {
		t.Errorf("PreToolUse should keep matcher, got %v", got)
	}
	if got := MatcherPatternForEvent(protocol.HookEventNamePostCompact, sp("manual|auto")); got == nil || *got != "manual|auto" {
		t.Errorf("PostCompact should keep matcher, got %v", got)
	}
}

func TestMatcherInputs(t *testing.T) {
	got := MatcherInputs("Bash", []string{"sh", "shell"})
	want := []string{"Bash", "sh", "shell"}
	if len(got) != len(want) {
		t.Fatalf("MatcherInputs length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MatcherInputs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
