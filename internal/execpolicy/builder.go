package execpolicy

import (
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// policyBuilder accumulates rules, host-executable allowlists, and pending
// example validations while one or more policy files are parsed. It mirrors the
// Rust `PolicyBuilder`. It is mutated only during parsing; the resulting
// [Policy] is immutable.
type policyBuilder struct {
	rulesByProgram            orderedRuleMap
	hostExecutablesByName     map[string][]abspath.AbsolutePathBuf
	pendingExampleValidations []pendingExampleValidation
}

// pendingExampleValidation records the rules and examples declared by a single
// prefix_rule() call, to be validated after the file is fully evaluated. It
// mirrors the Rust `PendingExampleValidation`.
type pendingExampleValidation struct {
	rules      []Rule
	matches    [][]string
	notMatches [][]string
	location   *ErrorLocation
}

func newPolicyBuilder() *policyBuilder {
	return &policyBuilder{
		rulesByProgram:        newOrderedRuleMap(),
		hostExecutablesByName: make(map[string][]abspath.AbsolutePathBuf),
	}
}

// addRule indexes a rule by its program token, mirroring Rust's
// `PolicyBuilder::add_rule`.
func (b *policyBuilder) addRule(rule Rule) {
	b.rulesByProgram.insert(rule.Program(), rule)
}

// addHostExecutable records (last-definition-wins) the allowlist for a
// normalized basename, mirroring Rust's `PolicyBuilder::add_host_executable`.
func (b *policyBuilder) addHostExecutable(name string, paths []abspath.AbsolutePathBuf) {
	b.hostExecutablesByName[name] = paths
}

// addPendingExampleValidation queues a rule's example self-tests for validation,
// mirroring Rust's `PolicyBuilder::add_pending_example_validation`.
func (b *policyBuilder) addPendingExampleValidation(rules []Rule, matches, notMatches [][]string, location *ErrorLocation) {
	b.pendingExampleValidations = append(b.pendingExampleValidations, pendingExampleValidation{
		rules:      rules,
		matches:    matches,
		notMatches: notMatches,
		location:   location,
	})
}

// validatePendingExamplesFrom validates every queued example self-test starting
// at index start, mirroring Rust's
// `PolicyBuilder::validate_pending_examples_from`.
//
// Each validation runs against a scratch policy holding only that rule's own
// rules (so examples test the rule in isolation) plus the full set of
// host-executable allowlists accumulated so far (so basename fallback is gated
// identically to the built policy). not_match examples are checked before match
// examples, matching the Rust ordering.
func (b *policyBuilder) validatePendingExamplesFrom(start int) error {
	for i := start; i < len(b.pendingExampleValidations); i++ {
		validation := b.pendingExampleValidations[i]

		scratch := &Policy{
			rulesByProgram:        newOrderedRuleMap(),
			hostExecutablesByName: cloneHostExecutables(b.hostExecutablesByName),
		}
		for _, rule := range validation.rules {
			scratch.rulesByProgram.insert(rule.Program(), rule)
		}

		if err := validateNotMatchExamples(scratch, validation.notMatches); err != nil {
			return attachValidationLocation(err, validation.location)
		}
		if err := validateMatchExamples(scratch, validation.rules, validation.matches); err != nil {
			return attachValidationLocation(err, validation.location)
		}
	}
	return nil
}

// build finalizes the accumulated state into an immutable [Policy], mirroring
// Rust's `PolicyBuilder::build`.
func (b *policyBuilder) build() *Policy {
	return &Policy{
		rulesByProgram:        b.rulesByProgram,
		hostExecutablesByName: b.hostExecutablesByName,
	}
}

// attachValidationLocation annotates an example-validation error with its source
// location, mirroring Rust's `attach_validation_location`.
func attachValidationLocation(err error, location *ErrorLocation) error {
	if location == nil {
		return err
	}
	policyErr, ok := err.(*Error)
	if !ok {
		return err
	}
	return policyErr.withLocation(*location)
}

// unwrapPolicyError extracts a typed policy [Error] from a Starlark evaluation
// error, if a builtin failed with one. go.starlark.net wraps a builtin's error
// in an [*starlark.EvalError]; we unwrap it to recover the original [*Error] so
// callers can match on its kind and message exactly as in Rust.
func unwrapPolicyError(err error) *Error {
	for err != nil {
		if policyErr, ok := err.(*Error); ok {
			return policyErr
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return nil
		}
		err = unwrapper.Unwrap()
	}
	return nil
}
