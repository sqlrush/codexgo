package execpolicy

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// allowAll mirrors the Rust test helper `allow_all`.
func allowAll([]string) Decision { return DecisionAllow }

// promptAll mirrors the Rust test helper `prompt_all`.
func promptAll([]string) Decision { return DecisionPrompt }

// tokens mirrors the Rust test helper `tokens`.
func tokens(cmd ...string) []string { return cmd }

// hostAbsolutePath mirrors the Rust test helper `host_absolute_path`, building
// an OS-appropriate absolute path from segments.
func hostAbsolutePath(segments ...string) string {
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, segments...)...)
}

// hostExecutableName mirrors the Rust test helper `host_executable_name`.
func hostExecutableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// starlarkString mirrors the Rust test helper `starlark_string`, escaping a
// path so it can be embedded in a Starlark string literal.
func starlarkString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

// absolutePath mirrors the Rust test helper `absolute_path`.
func absolutePath(t *testing.T, path string) abspath.AbsolutePathBuf {
	t.Helper()
	p, err := abspath.FromAbsolutePathChecked(path)
	if err != nil {
		t.Fatalf("expected absolute path %q: %v", path, err)
	}
	return p
}

// parsePolicy parses a single policy file and fails the test on error.
func parsePolicy(t *testing.T, identifier, src string) *Policy {
	t.Helper()
	parser := NewPolicyParser()
	if err := parser.Parse(identifier, src); err != nil {
		t.Fatalf("parse %s: %v", identifier, err)
	}
	return parser.Build()
}

// prefixMatch builds a prefix RuleMatch for comparison in tests.
func prefixMatch(prefix []string, decision Decision, resolved *abspath.AbsolutePathBuf, justification *string) RuleMatch {
	m := RuleMatch{kind: ruleMatchPrefix, matchedPrefix: prefix, decision: decision}
	if resolved != nil {
		m.resolvedProgram = *resolved
		m.hasResolvedProgram = true
	}
	if justification != nil {
		m.justification = *justification
		m.hasJustification = true
	}
	return m
}

// heuristicsMatch builds a heuristics RuleMatch for comparison in tests.
func heuristicsMatch(command []string, decision Decision) RuleMatch {
	return RuleMatch{kind: ruleMatchHeuristics, command: command, decision: decision}
}

// assertEvaluation compares two evaluations using reflect.DeepEqual on their
// exported and unexported fields (RuleMatch is value-comparable via reflect).
func assertEvaluation(t *testing.T, got, want Evaluation) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evaluation mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBasicMatch(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(
    pattern = ["git", "status"],
)
`)
	ev := policy.Check(tokens("git", "status"), allowAll)
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch(tokens("git", "status"), DecisionAllow, nil, nil)},
	})
}

func TestJustificationAttachedToForbiddenMatches(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(
    pattern = ["rm"],
    decision = "forbidden",
    justification = "destructive command",
)
`)
	ev := policy.Check(tokens("rm", "-rf", "/some/important/folder"), allowAll)
	just := "destructive command"
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionForbidden,
		MatchedRules: []RuleMatch{prefixMatch(tokens("rm"), DecisionForbidden, nil, &just)},
	})
}

func TestJustificationCanBeUsedWithAllowDecision(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(
    pattern = ["ls"],
    decision = "allow",
    justification = "safe and commonly used",
)
`)
	ev := policy.Check(tokens("ls", "-l"), promptAll)
	just := "safe and commonly used"
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch(tokens("ls"), DecisionAllow, nil, &just)},
	})
}

func TestJustificationCannotBeEmpty(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("test.rules", `
prefix_rule(
    pattern = ["ls"],
    decision = "prompt",
    justification = "   ",
)
`)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "invalid rule: justification cannot be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddPrefixRuleExtendsPolicy(t *testing.T) {
	policy, err := EmptyPolicy().AddPrefixRule(tokens("ls", "-l"), DecisionPrompt)
	if err != nil {
		t.Fatalf("add_prefix_rule: %v", err)
	}

	rules, ok := policy.Rules("ls")
	if !ok || len(rules) != 1 {
		t.Fatalf("expected one ls rule, got %v", rules)
	}
	pr, ok := rules[0].(*PrefixRule)
	if !ok {
		t.Fatalf("expected *PrefixRule, got %T", rules[0])
	}
	if pr.Pattern.First() != "ls" || len(pr.Pattern.Rest()) != 1 ||
		!pr.Pattern.Rest()[0].equal(NewSinglePatternToken("-l")) ||
		pr.Decision != DecisionPrompt || pr.HasJustification {
		t.Fatalf("unexpected rule: %+v", pr)
	}

	ev := policy.Check(tokens("ls", "-l", "/some/important/folder"), allowAll)
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionPrompt,
		MatchedRules: []RuleMatch{prefixMatch(tokens("ls", "-l"), DecisionPrompt, nil, nil)},
	})
}

func TestAddPrefixRuleRejectsEmptyPrefix(t *testing.T) {
	_, err := EmptyPolicy().AddPrefixRule(nil, DecisionAllow)
	var policyErr *Error
	if !asPolicyError(err, &policyErr) || policyErr.Kind != ErrInvalidPattern {
		t.Fatalf("expected InvalidPattern error, got %v", err)
	}
	if policyErr.Message != "prefix cannot be empty" {
		t.Fatalf("unexpected message: %q", policyErr.Message)
	}
}

func TestParsesMultiplePolicyFiles(t *testing.T) {
	parser := NewPolicyParser()
	if err := parser.Parse("first.rules", `
prefix_rule(
    pattern = ["git"],
    decision = "prompt",
)
`); err != nil {
		t.Fatalf("parse first: %v", err)
	}
	if err := parser.Parse("second.rules", `
prefix_rule(
    pattern = ["git", "commit"],
    decision = "forbidden",
)
`); err != nil {
		t.Fatalf("parse second: %v", err)
	}
	policy := parser.Build()

	gitRules, ok := policy.Rules("git")
	if !ok || len(gitRules) != 2 {
		t.Fatalf("expected two git rules, got %v", gitRules)
	}

	statusEval := policy.Check(tokens("git", "status"), allowAll)
	assertEvaluation(t, statusEval, Evaluation{
		Decision:     DecisionPrompt,
		MatchedRules: []RuleMatch{prefixMatch(tokens("git"), DecisionPrompt, nil, nil)},
	})

	commitEval := policy.Check(tokens("git", "commit", "-m", "hi"), allowAll)
	assertEvaluation(t, commitEval, Evaluation{
		Decision: DecisionForbidden,
		MatchedRules: []RuleMatch{
			prefixMatch(tokens("git"), DecisionPrompt, nil, nil),
			prefixMatch(tokens("git", "commit"), DecisionForbidden, nil, nil),
		},
	})
}

func TestOnlyFirstTokenAliasExpandsToMultipleRules(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(
    pattern = [["bash", "sh"], ["-c", "-l"]],
)
`)

	bashRules, ok := policy.Rules("bash")
	if !ok || len(bashRules) != 1 {
		t.Fatalf("expected one bash rule, got %v", bashRules)
	}
	shRules, ok := policy.Rules("sh")
	if !ok || len(shRules) != 1 {
		t.Fatalf("expected one sh rule, got %v", shRules)
	}
	bashRule := bashRules[0].(*PrefixRule)
	if bashRule.Pattern.First() != "bash" || len(bashRule.Pattern.Rest()) != 1 ||
		!bashRule.Pattern.Rest()[0].equal(NewAltsPatternToken([]string{"-c", "-l"})) {
		t.Fatalf("unexpected bash rule: %+v", bashRule)
	}

	bashEval := policy.Check(tokens("bash", "-c", "echo", "hi"), allowAll)
	assertEvaluation(t, bashEval, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch(tokens("bash", "-c"), DecisionAllow, nil, nil)},
	})

	shEval := policy.Check(tokens("sh", "-l", "echo", "hi"), allowAll)
	assertEvaluation(t, shEval, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch(tokens("sh", "-l"), DecisionAllow, nil, nil)},
	})
}

func TestTailAliasesAreNotCartesianExpanded(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(
    pattern = ["npm", ["i", "install"], ["--legacy-peer-deps", "--no-save"]],
)
`)

	rules, ok := policy.Rules("npm")
	if !ok || len(rules) != 1 {
		t.Fatalf("expected one npm rule, got %v", rules)
	}
	rule := rules[0].(*PrefixRule)
	if len(rule.Pattern.Rest()) != 2 ||
		!rule.Pattern.Rest()[0].equal(NewAltsPatternToken([]string{"i", "install"})) ||
		!rule.Pattern.Rest()[1].equal(NewAltsPatternToken([]string{"--legacy-peer-deps", "--no-save"})) {
		t.Fatalf("unexpected npm rule: %+v", rule)
	}

	npmI := policy.Check(tokens("npm", "i", "--legacy-peer-deps"), allowAll)
	assertEvaluation(t, npmI, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch(tokens("npm", "i", "--legacy-peer-deps"), DecisionAllow, nil, nil)},
	})

	npmInstall := policy.Check(tokens("npm", "install", "--no-save", "leftpad"), allowAll)
	assertEvaluation(t, npmInstall, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch(tokens("npm", "install", "--no-save"), DecisionAllow, nil, nil)},
	})
}

func TestMatchAndNotMatchExamplesAreEnforced(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(
    pattern = ["git", "status"],
    match = [["git", "status"], "git status"],
    not_match = [
        ["git", "--config", "color.status=always", "status"],
        "git --config color.status=always status",
    ],
)
`)
	matchEval := policy.Check(tokens("git", "status"), allowAll)
	assertEvaluation(t, matchEval, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch(tokens("git", "status"), DecisionAllow, nil, nil)},
	})

	noMatchEval := policy.Check(tokens("git", "--config", "color.status=always", "status"), allowAll)
	assertEvaluation(t, noMatchEval, Evaluation{
		Decision: DecisionAllow,
		MatchedRules: []RuleMatch{
			heuristicsMatch(tokens("git", "--config", "color.status=always", "status"), DecisionAllow),
		},
	})
}

func TestStrictestDecisionWinsAcrossMatches(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(
    pattern = ["git"],
    decision = "prompt",
)
prefix_rule(
    pattern = ["git", "commit"],
    decision = "forbidden",
)
`)
	commit := policy.Check(tokens("git", "commit", "-m", "hi"), allowAll)
	assertEvaluation(t, commit, Evaluation{
		Decision: DecisionForbidden,
		MatchedRules: []RuleMatch{
			prefixMatch(tokens("git"), DecisionPrompt, nil, nil),
			prefixMatch(tokens("git", "commit"), DecisionForbidden, nil, nil),
		},
	})
}

func TestStrictestDecisionAcrossMultipleCommands(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(
    pattern = ["git"],
    decision = "prompt",
)
prefix_rule(
    pattern = ["git", "commit"],
    decision = "forbidden",
)
`)
	commands := [][]string{
		tokens("git", "status"),
		tokens("git", "commit", "-m", "hi"),
	}
	ev := policy.CheckMultiple(commands, allowAll)
	assertEvaluation(t, ev, Evaluation{
		Decision: DecisionForbidden,
		MatchedRules: []RuleMatch{
			prefixMatch(tokens("git"), DecisionPrompt, nil, nil),
			prefixMatch(tokens("git"), DecisionPrompt, nil, nil),
			prefixMatch(tokens("git", "commit"), DecisionForbidden, nil, nil),
		},
	})
}

func TestHeuristicsMatchIsReturnedWhenNoPolicyMatches(t *testing.T) {
	policy := EmptyPolicy()
	command := tokens("python")
	ev := policy.Check(command, promptAll)
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionPrompt,
		MatchedRules: []RuleMatch{heuristicsMatch(tokens("python"), DecisionPrompt)},
	})
}

// asPolicyError is a small errors.As helper for *Error in tests.
func asPolicyError(err error, target **Error) bool {
	for err != nil {
		if pe, ok := err.(*Error); ok {
			*target = pe
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
