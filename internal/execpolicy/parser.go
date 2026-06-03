package execpolicy

import (
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// builderLocalKey is the [starlark.Thread] local-storage key under which the
// active [policyBuilder] is stored while a policy is evaluated. This mirrors the
// Rust parser stashing the builder in `Evaluator.extra`.
const builderLocalKey = "execpolicy.builder"

// PolicyParser accumulates rules from one or more policy files and builds an
// immutable [Policy]. It mirrors the Rust `PolicyParser`.
//
// Multiple files may be parsed in sequence (see [PolicyParser.Parse]); rules
// merge in the order provided and host_executable definitions follow
// last-definition-wins semantics. Example self-tests are validated incrementally
// after each file, against the rules accumulated so far.
type PolicyParser struct {
	builder *policyBuilder
}

// NewPolicyParser returns an empty parser, mirroring Rust's `PolicyParser::new`.
func NewPolicyParser() *PolicyParser {
	return &PolicyParser{builder: newPolicyBuilder()}
}

// Parse evaluates one policy file's contents, tagging Starlark errors with
// policyIdentifier (used in error locations and messages). It mirrors Rust's
// `PolicyParser::parse`.
//
// After evaluation it validates the example self-tests declared by the rules in
// this file: every `match` example must classify against at least one rule and
// every `not_match` example must classify against none. A failed self-test
// returns an [Error] of kind [ErrExampleDidNotMatch] or [ErrExampleDidMatch],
// matching codex's load-time validation.
func (p *PolicyParser) Parse(policyIdentifier, policyFileContents string) error {
	pendingValidationCount := len(p.builder.pendingExampleValidations)

	options := dialectExtended()
	thread := &starlark.Thread{Name: policyIdentifier}
	thread.SetLocal(builderLocalKey, p.builder)

	predeclared := policyBuiltins()
	_, err := starlark.ExecFileOptions(options, thread, policyIdentifier, policyFileContents, predeclared)
	if err != nil {
		// A builtin may have failed with a typed policy [Error]; surface it
		// directly so callers can match on its kind. Otherwise wrap the
		// Starlark error, mirroring Rust's `Error::Starlark`.
		if policyErr := unwrapPolicyError(err); policyErr != nil {
			return policyErr
		}
		return &Error{Kind: ErrStarlark, Wrapped: err, Location: starlarkErrorLocation(err)}
	}

	return p.builder.validatePendingExamplesFrom(pendingValidationCount)
}

// Build consumes the parser and returns the accumulated [Policy], mirroring
// Rust's `PolicyParser::build`.
func (p *PolicyParser) Build() *Policy {
	return p.builder.build()
}

// dialectExtended returns the [syntax.FileOptions] that approximate Rust's
// `Dialect::Extended`: control-flow statements are permitted at the top level
// and globals may be reassigned, and the `set` and `while` constructs are
// enabled. F-strings (also enabled in the Rust dialect) have no equivalent in
// go.starlark.net and are not used by codex policies.
func dialectExtended() *syntax.FileOptions {
	return &syntax.FileOptions{
		Set:             true,
		While:           true,
		TopLevelControl: true,
		GlobalReassign:  true,
	}
}

// policyBuiltins returns the predeclared environment exposing the policy
// builtins (`prefix_rule` and `host_executable`).
func policyBuiltins() starlark.StringDict {
	return starlark.StringDict{
		"prefix_rule":     starlark.NewBuiltin("prefix_rule", prefixRuleBuiltin),
		"host_executable": starlark.NewBuiltin("host_executable", hostExecutableBuiltin),
	}
}

// builderFromThread retrieves the active [policyBuilder] from thread-local
// storage. It panics if absent, mirroring the Rust parser's expectation that
// `Evaluator.extra` is always populated (a programming error, not user input).
func builderFromThread(thread *starlark.Thread) *policyBuilder {
	builder, ok := thread.Local(builderLocalKey).(*policyBuilder)
	if !ok {
		panic("execpolicy: policy builder missing from thread-local storage")
	}
	return builder
}

// prefixRuleBuiltin implements the `prefix_rule(pattern, decision, match,
// not_match, justification)` builtin, mirroring the Rust `prefix_rule` builtin.
func prefixRuleBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		patternValue  *starlark.List
		decisionRaw   string
		matchValue    *starlark.List
		notMatchValue *starlark.List
		justification string
	)
	if err := starlark.UnpackArgs(
		fn.Name(), args, kwargs,
		"pattern", &patternValue,
		"decision?", &decisionRaw,
		"match?", &matchValue,
		"not_match?", &notMatchValue,
		"justification?", &justification,
	); err != nil {
		return nil, err
	}

	decision := DecisionAllow
	if hasKeyword(kwargs, "decision") {
		parsed, err := ParseDecision(decisionRaw)
		if err != nil {
			return nil, err
		}
		decision = parsed
	}

	justificationSet := hasKeyword(kwargs, "justification")
	if justificationSet && isBlank(justification) {
		return nil, &Error{Kind: ErrInvalidRule, Message: "justification cannot be empty"}
	}

	patternTokens, err := parsePattern(patternValue)
	if err != nil {
		return nil, err
	}

	matches, err := parseExamples(matchValue)
	if err != nil {
		return nil, err
	}
	notMatches, err := parseExamples(notMatchValue)
	if err != nil {
		return nil, err
	}

	location := callerLocation(thread)

	// The first pattern token's alternatives each become a separate rule,
	// since policies are keyed by their first token. Trailing alternatives are
	// kept as Alts tokens (not Cartesian-expanded).
	firstToken := patternTokens[0]
	rest := append([]PatternToken(nil), patternTokens[1:]...)

	heads := firstToken.Alternatives()
	rules := make([]Rule, 0, len(heads))
	for _, head := range heads {
		rules = append(rules, &PrefixRule{
			Pattern:          PrefixPattern{first: head, rest: rest},
			Decision:         decision,
			Justification:    justification,
			HasJustification: justificationSet,
		})
	}

	builder := builderFromThread(thread)
	builder.addPendingExampleValidation(rules, matches, notMatches, location)
	for _, rule := range rules {
		builder.addRule(rule)
	}
	return starlark.None, nil
}

// hostExecutableBuiltin implements the `host_executable(name, paths)` builtin,
// mirroring the Rust `host_executable` builtin.
func hostExecutableBuiltin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		name      string
		pathsList *starlark.List
	)
	if err := starlark.UnpackArgs(
		fn.Name(), args, kwargs,
		"name", &name,
		"paths", &pathsList,
	); err != nil {
		return nil, err
	}

	if err := validateHostExecutableName(name); err != nil {
		return nil, err
	}

	parsedPaths, err := parseHostExecutablePaths(name, pathsList)
	if err != nil {
		return nil, err
	}

	builder := builderFromThread(thread)
	builder.addHostExecutable(executableLookupKey(name), parsedPaths)
	return starlark.None, nil
}

// parseHostExecutablePaths validates and de-duplicates the absolute paths in a
// host_executable() call, mirroring the per-path logic in the Rust builtin.
func parseHostExecutablePaths(name string, pathsList *starlark.List) ([]abspath.AbsolutePathBuf, error) {
	values, err := listElements(pathsList)
	if err != nil {
		return nil, err
	}

	expectedKey := executableLookupKey(name)
	var parsed []abspath.AbsolutePathBuf
	for _, value := range values {
		raw, ok := starlark.AsString(value)
		if !ok {
			return nil, &Error{
				Kind:    ErrInvalidRule,
				Message: "host_executable paths must be strings (got " + value.Type() + ")",
			}
		}
		path, err := parseLiteralAbsolutePath(raw)
		if err != nil {
			return nil, err
		}
		pathName, ok := executablePathLookupKey(path.Path())
		if !ok || pathName != expectedKey {
			return nil, &Error{
				Kind:    ErrInvalidRule,
				Message: "host_executable path `" + raw + "` must have basename `" + name + "`",
			}
		}
		if !containsPath(parsed, path) {
			parsed = append(parsed, path)
		}
	}
	return parsed, nil
}

// parseLiteralAbsolutePath converts a policy string into an absolute path,
// rejecting relative inputs. It mirrors Rust's `parse_literal_absolute_path`.
func parseLiteralAbsolutePath(raw string) (abspath.AbsolutePathBuf, error) {
	if !filepath.IsAbs(raw) {
		return abspath.AbsolutePathBuf{}, &Error{
			Kind:    ErrInvalidRule,
			Message: "host_executable paths must be absolute (got " + raw + ")",
		}
	}
	path, err := abspath.FromAbsolutePathChecked(raw)
	if err != nil {
		return abspath.AbsolutePathBuf{}, &Error{
			Kind:    ErrInvalidRule,
			Message: "invalid absolute path `" + raw + "`: " + err.Error(),
		}
	}
	return path, nil
}

// validateHostExecutableName ensures name is a bare executable name (no path
// separators), mirroring Rust's `validate_host_executable_name`.
func validateHostExecutableName(name string) error {
	if name == "" {
		return &Error{Kind: ErrInvalidRule, Message: "host_executable name cannot be empty"}
	}
	// A bare name has exactly one path component equal to itself: it must not
	// contain a separator and must not be a special component like "." or "..".
	base := filepath.Base(name)
	if base != name || containsPathSeparator(name) || name == "." || name == ".." {
		return &Error{
			Kind:    ErrInvalidRule,
			Message: "host_executable name must be a bare executable name (got " + name + ")",
		}
	}
	return nil
}

// containsPathSeparator reports whether name contains any path separator. Both
// '/' and the OS separator are checked so behavior matches Rust's
// `Path::components` across platforms.
func containsPathSeparator(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] == '/' || name[i] == filepath.Separator {
			return true
		}
	}
	return false
}

// callerLocation resolves the source location of the policy statement that
// invoked a builtin, mirroring Rust's `eval.call_stack_top_location`.
func callerLocation(thread *starlark.Thread) *ErrorLocation {
	if thread.CallStackDepth() < 1 {
		return nil
	}
	frame := thread.CallFrame(1)
	pos := frame.Pos
	if !pos.IsValid() {
		return nil
	}
	return &ErrorLocation{
		Path: pos.Filename(),
		Range: TextRange{
			Start: TextPosition{Line: int(pos.Line), Column: int(pos.Col)},
			End:   TextPosition{Line: int(pos.Line), Column: int(pos.Col)},
		},
	}
}

// starlarkErrorLocation extracts a source location from a Starlark evaluation
// error, mirroring Rust's `Error::location` for the Starlark variant.
func starlarkErrorLocation(err error) *ErrorLocation {
	evalErr, ok := err.(*starlark.EvalError)
	if !ok {
		return nil
	}
	frames := evalErr.CallStack
	if len(frames) == 0 {
		return nil
	}
	pos := frames.At(len(frames) - 1).Pos
	if !pos.IsValid() {
		return nil
	}
	return &ErrorLocation{
		Path: pos.Filename(),
		Range: TextRange{
			Start: TextPosition{Line: int(pos.Line), Column: int(pos.Col)},
			End:   TextPosition{Line: int(pos.Line), Column: int(pos.Col)},
		},
	}
}

// hasKeyword reports whether name was supplied as a keyword argument, used to
// distinguish an explicitly-passed value from an omitted optional argument.
func hasKeyword(kwargs []starlark.Tuple, name string) bool {
	for _, kv := range kwargs {
		if len(kv) == 2 {
			if key, ok := starlark.AsString(kv[0]); ok && key == name {
				return true
			}
		}
	}
	return false
}

// isBlank reports whether s is empty or all ASCII/Unicode whitespace, matching
// Rust's `str::trim().is_empty()`.
func isBlank(s string) bool {
	for _, r := range s {
		if !isWhitespace(r) {
			return false
		}
	}
	return true
}

// isWhitespace reports whether r is whitespace per Rust's `char::is_whitespace`
// (which follows the Unicode White_Space property). The common cases are ASCII;
// the broader set is approximated with unicode.IsSpace.
func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return r == 0x85 || r == 0xA0 || isUnicodeSpace(r)
}
