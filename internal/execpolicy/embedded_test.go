package execpolicy

import (
	"strings"
	"testing"
)

// TestExamplePolicyLoadsAndMatches verifies the embedded baseline policy parses
// (its match/not_match self-tests pass) and behaves as declared.
func TestExamplePolicyLoadsAndMatches(t *testing.T) {
	policy, err := LoadExamplePolicy()
	if err != nil {
		t.Fatalf("embedded example policy failed to load: %v", err)
	}

	cases := []struct {
		cmd      []string
		decision Decision
		isMatch  bool
	}{
		{tokens("git", "reset", "--hard"), DecisionForbidden, true},
		{tokens("ls", "-l"), DecisionAllow, true},
		{tokens("cat", "file.txt"), DecisionAllow, true},
		{tokens("cp", "foo", "bar"), DecisionPrompt, true},
		{tokens("pwd"), DecisionAllow, true},
		{tokens("which", "python3"), DecisionAllow, true},
		// Not declared: falls back to heuristics (prompt below).
		{tokens("rm", "-rf", "/"), DecisionPrompt, false},
	}
	for _, tc := range cases {
		ev := policy.Check(tc.cmd, promptAll)
		if ev.Decision != tc.decision {
			t.Errorf("%v: decision = %v, want %v", tc.cmd, ev.Decision, tc.decision)
		}
		if ev.IsMatch() != tc.isMatch {
			t.Errorf("%v: IsMatch = %v, want %v", tc.cmd, ev.IsMatch(), tc.isMatch)
		}
	}
}

// TestExamplePolicyConstantNonEmpty guards against the embed directive silently
// producing an empty asset.
func TestExamplePolicyConstantNonEmpty(t *testing.T) {
	if !strings.Contains(ExamplePolicy, "prefix_rule(") {
		t.Fatalf("embedded example policy looks empty or malformed: %q", ExamplePolicy)
	}
}

// TestMatchExampleFailureRejectsLoad verifies that a `match` example that does
// not classify against the rule fails the load, mirroring codex's load-time
// self-tests.
func TestMatchExampleFailureRejectsLoad(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("bad.rules", `
prefix_rule(
    pattern = ["git", "status"],
    match = [["git", "log"]],
)
`)
	var pe *Error
	if !asPolicyError(err, &pe) || pe.Kind != ErrExampleDidNotMatch {
		t.Fatalf("expected ErrExampleDidNotMatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "expected every example to match at least one rule") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// TestNotMatchExampleFailureRejectsLoad verifies that a `not_match` example that
// does classify against the rule fails the load.
func TestNotMatchExampleFailureRejectsLoad(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("bad.rules", `
prefix_rule(
    pattern = ["git"],
    not_match = [["git", "status"]],
)
`)
	var pe *Error
	if !asPolicyError(err, &pe) || pe.Kind != ErrExampleDidMatch {
		t.Fatalf("expected ErrExampleDidMatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "expected example to not match rule") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// TestExampleValidationErrorCarriesLocation verifies failed self-tests are
// annotated with a source location, mirroring Rust's location attachment.
func TestExampleValidationErrorCarriesLocation(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("bad.rules", `prefix_rule(
    pattern = ["git", "status"],
    match = [["git", "log"]],
)`)
	var pe *Error
	if !asPolicyError(err, &pe) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if pe.Location == nil {
		t.Fatal("expected location to be attached")
	}
	if pe.Location.Path != "bad.rules" {
		t.Fatalf("unexpected location path: %q", pe.Location.Path)
	}
	if pe.Location.Range.Start.Line != 1 {
		t.Fatalf("unexpected location line: %d", pe.Location.Range.Start.Line)
	}
}

// TestInvalidStarlarkSyntaxReportsStarlarkError verifies syntax errors surface
// as the Starlark error variant.
func TestInvalidStarlarkSyntaxReportsStarlarkError(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("broken.rules", "prefix_rule(pattern=[")
	var pe *Error
	if !asPolicyError(err, &pe) || pe.Kind != ErrStarlark {
		t.Fatalf("expected ErrStarlark, got %v", err)
	}
	if !strings.Contains(err.Error(), "starlark error:") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// TestEmptyPatternRejected verifies an empty pattern is rejected.
func TestEmptyPatternRejected(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("bad.rules", `prefix_rule(pattern=[])`)
	var pe *Error
	if !asPolicyError(err, &pe) || pe.Kind != ErrInvalidPattern {
		t.Fatalf("expected ErrInvalidPattern, got %v", err)
	}
}

// TestInvalidDecisionRejected verifies an unknown decision string is rejected.
func TestInvalidDecisionRejected(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("bad.rules", `prefix_rule(pattern=["git"], decision="deny")`)
	var pe *Error
	if !asPolicyError(err, &pe) || pe.Kind != ErrInvalidDecision {
		t.Fatalf("expected ErrInvalidDecision, got %v", err)
	}
}

// TestStringExampleTokenization verifies string-form examples are tokenized with
// shlex, exercising the match path.
func TestStringExampleTokenization(t *testing.T) {
	policy := parsePolicy(t, "ok.rules", `
prefix_rule(
    pattern = ["cp"],
    decision = "prompt",
    match = ["cp -r src dest"],
)
`)
	ev := policy.Check(tokens("cp", "-r", "src", "dest"), allowAll)
	if ev.Decision != DecisionPrompt || !ev.IsMatch() {
		t.Fatalf("unexpected evaluation: %#v", ev)
	}
}
