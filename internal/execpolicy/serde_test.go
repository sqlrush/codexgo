package execpolicy

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func TestDecisionParse(t *testing.T) {
	cases := []struct {
		raw     string
		want    Decision
		wantErr bool
	}{
		{"allow", DecisionAllow, false},
		{"prompt", DecisionPrompt, false},
		{"forbidden", DecisionForbidden, false},
		{"deny", 0, true},
		{"", 0, true},
	}
	for _, tc := range cases {
		got, err := ParseDecision(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseDecision(%q): expected error", tc.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDecision(%q): unexpected error %v", tc.raw, err)
		}
		if got != tc.want {
			t.Errorf("ParseDecision(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestDecisionInvalidError(t *testing.T) {
	_, err := ParseDecision("deny")
	var pe *Error
	if !asPolicyError(err, &pe) || pe.Kind != ErrInvalidDecision {
		t.Fatalf("expected ErrInvalidDecision, got %v", err)
	}
	if err.Error() != "invalid decision: deny" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestDecisionStrictestOrdering(t *testing.T) {
	if DecisionForbidden.max(DecisionPrompt) != DecisionForbidden {
		t.Error("forbidden should beat prompt")
	}
	if DecisionPrompt.max(DecisionAllow) != DecisionPrompt {
		t.Error("prompt should beat allow")
	}
	if DecisionAllow.max(DecisionAllow) != DecisionAllow {
		t.Error("allow should equal allow")
	}
}

func TestDecisionJSONRoundTrip(t *testing.T) {
	for _, d := range []Decision{DecisionAllow, DecisionPrompt, DecisionForbidden} {
		data, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal %v: %v", d, err)
		}
		var back Decision
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if back != d {
			t.Fatalf("round trip %v -> %s -> %v", d, data, back)
		}
	}
}

// TestRuleMatchJSONShape verifies the externally-tagged JSON shape and field
// names/casing match the Rust serde derives exactly.
func TestRuleMatchJSONShape(t *testing.T) {
	just := "destructive command"
	resolved := mustAbs(t, "/usr/bin/git")

	cases := []struct {
		name  string
		match RuleMatch
		want  string
	}{
		{
			name:  "prefix minimal",
			match: prefixMatch([]string{"git", "status"}, DecisionAllow, nil, nil),
			want:  `{"prefixRuleMatch":{"matchedPrefix":["git","status"],"decision":"allow"}}`,
		},
		{
			name:  "prefix with justification",
			match: prefixMatch([]string{"rm"}, DecisionForbidden, nil, &just),
			want:  `{"prefixRuleMatch":{"matchedPrefix":["rm"],"decision":"forbidden","justification":"destructive command"}}`,
		},
		{
			name:  "prefix with resolved program",
			match: prefixMatch([]string{"git", "status"}, DecisionPrompt, &resolved, nil),
			want:  `{"prefixRuleMatch":{"matchedPrefix":["git","status"],"decision":"prompt","resolvedProgram":"/usr/bin/git"}}`,
		},
		{
			name:  "heuristics",
			match: heuristicsMatch([]string{"python"}, DecisionPrompt),
			want:  `{"heuristicsRuleMatch":{"command":["python"],"decision":"prompt"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.match)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Fatalf("\n got: %s\nwant: %s", data, tc.want)
			}
		})
	}
}

// TestFormatMatchesJSONNoMatchOmitsDecision verifies the `check` output omits
// `decision` when there are no matches, matching Rust's
// `skip_serializing_if = "Option::is_none"`.
func TestFormatMatchesJSONNoMatchOmitsDecision(t *testing.T) {
	got, err := FormatMatchesJSON(nil, false)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	want := `{"matchedRules":[]}`
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestFormatMatchesJSONIncludesStrictestDecision(t *testing.T) {
	matches := []RuleMatch{
		prefixMatch([]string{"git"}, DecisionPrompt, nil, nil),
		prefixMatch([]string{"git", "commit"}, DecisionForbidden, nil, nil),
	}
	got, err := FormatMatchesJSON(matches, false)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	want := `{"matchedRules":[{"prefixRuleMatch":{"matchedPrefix":["git"],"decision":"prompt"}},` +
		`{"prefixRuleMatch":{"matchedPrefix":["git","commit"],"decision":"forbidden"}}],"decision":"forbidden"}`
	if got != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestEvaluationJSONShape(t *testing.T) {
	ev := Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch([]string{"git", "status"}, DecisionAllow, nil, nil)},
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"decision":"allow","matchedRules":[{"prefixRuleMatch":{"matchedPrefix":["git","status"],"decision":"allow"}}]}`
	if string(data) != want {
		t.Fatalf("\n got: %s\nwant: %s", data, want)
	}
}

func mustAbs(t *testing.T, path string) abspath.AbsolutePathBuf {
	t.Helper()
	p, err := abspath.FromAbsolutePathChecked(path)
	if err != nil {
		t.Fatalf("abs %q: %v", path, err)
	}
	return p
}
