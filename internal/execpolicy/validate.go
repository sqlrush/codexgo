package execpolicy

// validateMatchExamples checks that every positive example matches at least one
// rule, mirroring Rust's `validate_match_examples`. Example validation always
// runs with host-executable resolution enabled so that examples written as
// absolute paths exercise basename fallback.
func validateMatchExamples(policy *Policy, rules []Rule, matches [][]string) error {
	options := MatchOptions{ResolveHostExecutables: true}
	var unmatched []string

	for _, example := range matches {
		if len(policy.MatchesForCommand(example, nil, options)) != 0 {
			continue
		}
		unmatched = append(unmatched, renderExample(example))
	}

	if len(unmatched) == 0 {
		return nil
	}
	return &Error{
		Kind:     ErrExampleDidNotMatch,
		Rules:    renderRules(rules),
		Examples: unmatched,
	}
}

// validateNotMatchExamples checks that no negative example matches any rule,
// mirroring Rust's `validate_not_match_examples`.
func validateNotMatchExamples(policy *Policy, matches [][]string) error {
	options := MatchOptions{ResolveHostExecutables: true}

	for _, example := range matches {
		matched := policy.MatchesForCommand(example, nil, options)
		if len(matched) > 0 {
			return &Error{
				Kind:    ErrExampleDidMatch,
				Rule:    matched[0].debug(),
				Example: renderExample(example),
			}
		}
	}
	return nil
}

// renderExample formats an example command for error messages using shlex's
// try_join, falling back to a fixed message when the example contains a NUL
// byte. This mirrors Rust's use of `shlex::try_join(...).unwrap_or_else(...)`.
func renderExample(example []string) string {
	if rendered, ok := shlexTryJoin(example); ok {
		return rendered
	}
	return "unable to render example"
}

// renderRules formats rules for the ExampleDidNotMatch error message using each
// rule's debug representation, mirroring Rust's `format!("{rule:?}")`.
func renderRules(rules []Rule) []string {
	rendered := make([]string, len(rules))
	for i, rule := range rules {
		rendered[i] = rule.debug()
	}
	return rendered
}
