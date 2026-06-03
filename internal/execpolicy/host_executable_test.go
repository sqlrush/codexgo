package execpolicy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func TestParsesHostExecutablePaths(t *testing.T) {
	homebrewGit := hostAbsolutePath("opt", "homebrew", "bin", "git")
	usrGit := hostAbsolutePath("usr", "bin", "git")
	src := fmt.Sprintf(`
host_executable(
    name = "git",
    paths = [
        "%s",
        "%s",
        "%s",
    ],
)
`, starlarkString(homebrewGit), starlarkString(usrGit), starlarkString(usrGit))

	policy := parsePolicy(t, "test.rules", src)
	paths, ok := policy.HostExecutables("git")
	if !ok {
		t.Fatal("missing git host executable")
	}
	want := []abspath.AbsolutePathBuf{absolutePath(t, homebrewGit), absolutePath(t, usrGit)}
	if len(paths) != len(want) {
		t.Fatalf("got %d paths, want %d: %v", len(paths), len(want), paths)
	}
	for i := range want {
		if paths[i].Path() != want[i].Path() {
			t.Fatalf("path %d = %q, want %q", i, paths[i].Path(), want[i].Path())
		}
	}
}

func TestHostExecutableRejectsNonAbsolutePath(t *testing.T) {
	parser := NewPolicyParser()
	err := parser.Parse("test.rules", `
host_executable(name = "git", paths = ["git"])
`)
	if err == nil || !strings.Contains(err.Error(), "host_executable paths must be absolute") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostExecutableRejectsNameWithPathSeparator(t *testing.T) {
	gitPath := hostAbsolutePath("usr", "bin", "git")
	src := fmt.Sprintf(`host_executable(name = "%s", paths = ["%s"])`,
		starlarkString(gitPath), starlarkString(gitPath))
	parser := NewPolicyParser()
	err := parser.Parse("test.rules", src)
	if err == nil || !strings.Contains(err.Error(), "host_executable name must be a bare executable name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostExecutableRejectsPathWithWrongBasename(t *testing.T) {
	rgPath := hostAbsolutePath("usr", "bin", "rg")
	src := fmt.Sprintf(`host_executable(name = "git", paths = ["%s"])`, starlarkString(rgPath))
	parser := NewPolicyParser()
	err := parser.Parse("test.rules", src)
	if err == nil || !strings.Contains(err.Error(), "must have basename `git`") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostExecutableLastDefinitionWins(t *testing.T) {
	usrGit := hostAbsolutePath("usr", "bin", "git")
	homebrewGit := hostAbsolutePath("opt", "homebrew", "bin", "git")
	parser := NewPolicyParser()
	if err := parser.Parse("shared.rules",
		fmt.Sprintf(`host_executable(name = "git", paths = ["%s"])`, starlarkString(usrGit))); err != nil {
		t.Fatalf("parse shared: %v", err)
	}
	if err := parser.Parse("user.rules",
		fmt.Sprintf(`host_executable(name = "git", paths = ["%s"])`, starlarkString(homebrewGit))); err != nil {
		t.Fatalf("parse user: %v", err)
	}
	policy := parser.Build()

	paths, ok := policy.HostExecutables("git")
	if !ok || len(paths) != 1 || paths[0].Path() != absolutePath(t, homebrewGit).Path() {
		t.Fatalf("expected last definition to win, got %v", paths)
	}
}

func TestHostExecutableResolutionUsesBasenameRuleWhenAllowed(t *testing.T) {
	gitName := hostExecutableName("git")
	gitPath := hostAbsolutePath("usr", "bin", gitName)
	src := fmt.Sprintf(`
prefix_rule(pattern = ["git", "status"], decision = "prompt")
host_executable(name = "git", paths = ["%s"])
`, starlarkString(gitPath))

	policy := parsePolicy(t, "test.rules", src)
	resolved := absolutePath(t, gitPath)
	ev := policy.CheckWithOptions(
		[]string{gitPath, "status"},
		allowAll,
		MatchOptions{ResolveHostExecutables: true},
	)
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionPrompt,
		MatchedRules: []RuleMatch{prefixMatch(tokens("git", "status"), DecisionPrompt, &resolved, nil)},
	})
}

func TestPrefixRuleExamplesHonorHostExecutableResolution(t *testing.T) {
	gitName := hostExecutableName("git")
	allowedGit := hostAbsolutePath("usr", "bin", gitName)
	otherGit := hostAbsolutePath("opt", "homebrew", "bin", gitName)
	src := fmt.Sprintf(`
prefix_rule(
    pattern = ["git", "status"],
    match = [["%s", "status"]],
    not_match = [["%s", "status"]],
)
host_executable(name = "git", paths = ["%s"])
`, starlarkString(allowedGit), starlarkString(otherGit), starlarkString(allowedGit))

	// This must parse without error: the match example resolves via the
	// allowlisted path and the not_match example does not.
	parser := NewPolicyParser()
	if err := parser.Parse("test.rules", src); err != nil {
		t.Fatalf("parse: %v", err)
	}
}

func TestHostExecutableResolutionRespectsExplicitEmptyAllowlist(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(pattern = ["git"], decision = "prompt")
host_executable(name = "git", paths = [])
`)
	gitPath := hostAbsolutePath("usr", "bin", "git")
	ev := policy.CheckWithOptions(
		[]string{gitPath, "status"},
		allowAll,
		MatchOptions{ResolveHostExecutables: true},
	)
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{heuristicsMatch([]string{gitPath, "status"}, DecisionAllow)},
	})
}

func TestHostExecutableResolutionIgnoresPathNotInAllowlist(t *testing.T) {
	allowedGit := hostAbsolutePath("usr", "bin", "git")
	otherGit := hostAbsolutePath("opt", "homebrew", "bin", "git")
	src := fmt.Sprintf(`
prefix_rule(pattern = ["git"], decision = "prompt")
host_executable(name = "git", paths = ["%s"])
`, starlarkString(allowedGit))

	policy := parsePolicy(t, "test.rules", src)
	ev := policy.CheckWithOptions(
		[]string{otherGit, "status"},
		allowAll,
		MatchOptions{ResolveHostExecutables: true},
	)
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{heuristicsMatch([]string{otherGit, "status"}, DecisionAllow)},
	})
}

func TestHostExecutableResolutionFallsBackWithoutMapping(t *testing.T) {
	policy := parsePolicy(t, "test.rules", `
prefix_rule(pattern = ["git"], decision = "prompt")
`)
	gitPath := hostAbsolutePath("usr", "bin", "git")
	resolved := absolutePath(t, gitPath)
	ev := policy.CheckWithOptions(
		[]string{gitPath, "status"},
		allowAll,
		MatchOptions{ResolveHostExecutables: true},
	)
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionPrompt,
		MatchedRules: []RuleMatch{prefixMatch(tokens("git"), DecisionPrompt, &resolved, nil)},
	})
}

func TestHostExecutableResolutionDoesNotOverrideExactMatch(t *testing.T) {
	gitPath := hostAbsolutePath("usr", "bin", "git")
	src := fmt.Sprintf(`
prefix_rule(pattern = ["%s"], decision = "allow")
prefix_rule(pattern = ["git"], decision = "prompt")
host_executable(name = "git", paths = ["%s"])
`, starlarkString(gitPath), starlarkString(gitPath))

	policy := parsePolicy(t, "test.rules", src)
	ev := policy.CheckWithOptions(
		[]string{gitPath, "status"},
		allowAll,
		MatchOptions{ResolveHostExecutables: true},
	)
	assertEvaluation(t, ev, Evaluation{
		Decision:     DecisionAllow,
		MatchedRules: []RuleMatch{prefixMatch([]string{gitPath}, DecisionAllow, nil, nil)},
	})
}
