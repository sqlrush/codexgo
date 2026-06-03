package execpolicy

import (
	"unicode"

	"go.starlark.net/starlark"
)

// listElements returns the elements of a Starlark list, or nil for a nil list
// (an omitted optional argument). It centralizes iteration so callers do not
// depend on the concrete iterator API.
func listElements(list *starlark.List) ([]starlark.Value, error) {
	if list == nil {
		return nil, nil
	}
	elems := make([]starlark.Value, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		elems = append(elems, list.Index(i))
	}
	return elems, nil
}

// parsePattern converts the `pattern` argument into pattern tokens, mirroring
// Rust's `parse_pattern`. An empty pattern is rejected.
func parsePattern(pattern *starlark.List) ([]PatternToken, error) {
	values, err := listElements(pattern)
	if err != nil {
		return nil, err
	}
	tokens := make([]PatternToken, 0, len(values))
	for _, value := range values {
		token, err := parsePatternToken(value)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 {
		return nil, &Error{Kind: ErrInvalidPattern, Message: "pattern cannot be empty"}
	}
	return tokens, nil
}

// parsePatternToken converts a single pattern element (a string or a list of
// strings) into a [PatternToken], mirroring Rust's `parse_pattern_token`. A
// single-element alternatives list collapses to a Single token; a multi-element
// list becomes an Alts token; an empty list is rejected.
func parsePatternToken(value starlark.Value) (PatternToken, error) {
	if s, ok := starlark.AsString(value); ok {
		return NewSinglePatternToken(s), nil
	}

	list, ok := value.(*starlark.List)
	if !ok {
		return PatternToken{}, &Error{
			Kind:    ErrInvalidPattern,
			Message: "pattern element must be a string or list of strings (got " + value.Type() + ")",
		}
	}

	elems, err := listElements(list)
	if err != nil {
		return PatternToken{}, err
	}
	tokens := make([]string, 0, len(elems))
	for _, elem := range elems {
		s, ok := starlark.AsString(elem)
		if !ok {
			return PatternToken{}, &Error{
				Kind:    ErrInvalidPattern,
				Message: "pattern alternative must be a string (got " + elem.Type() + ")",
			}
		}
		tokens = append(tokens, s)
	}

	switch len(tokens) {
	case 0:
		return PatternToken{}, &Error{Kind: ErrInvalidPattern, Message: "pattern alternatives cannot be empty"}
	case 1:
		return NewSinglePatternToken(tokens[0]), nil
	default:
		return NewAltsPatternToken(tokens), nil
	}
}

// parseExamples converts a `match`/`not_match` argument into a list of token
// commands, mirroring Rust's `parse_examples`. A nil list yields no examples.
func parseExamples(examples *starlark.List) ([][]string, error) {
	values, err := listElements(examples)
	if err != nil {
		return nil, err
	}
	parsed := make([][]string, 0, len(values))
	for _, value := range values {
		example, err := parseExample(value)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, example)
	}
	return parsed, nil
}

// parseExample converts one example (a shell string or a list of token strings)
// into a token command, mirroring Rust's `parse_example`.
func parseExample(value starlark.Value) ([]string, error) {
	if s, ok := starlark.AsString(value); ok {
		return parseStringExample(s)
	}
	list, ok := value.(*starlark.List)
	if !ok {
		return nil, &Error{
			Kind:    ErrInvalidExample,
			Message: "example must be a string or list of strings (got " + value.Type() + ")",
		}
	}
	return parseListExample(list)
}

// parseStringExample tokenizes a shell-string example, mirroring Rust's
// `parse_string_example`. Invalid shell syntax or an empty result is rejected.
func parseStringExample(raw string) ([]string, error) {
	tokens, ok := shlexSplit(raw)
	if !ok {
		return nil, &Error{Kind: ErrInvalidExample, Message: "example string has invalid shell syntax"}
	}
	if len(tokens) == 0 {
		return nil, &Error{Kind: ErrInvalidExample, Message: "example cannot be an empty string"}
	}
	return tokens, nil
}

// parseListExample converts a list of token strings into a command, mirroring
// Rust's `parse_list_example`. Non-string tokens or an empty list are rejected.
func parseListExample(list *starlark.List) ([]string, error) {
	elems, err := listElements(list)
	if err != nil {
		return nil, err
	}
	tokens := make([]string, 0, len(elems))
	for _, elem := range elems {
		s, ok := starlark.AsString(elem)
		if !ok {
			return nil, &Error{
				Kind:    ErrInvalidExample,
				Message: "example tokens must be strings (got " + elem.Type() + ")",
			}
		}
		tokens = append(tokens, s)
	}
	if len(tokens) == 0 {
		return nil, &Error{Kind: ErrInvalidExample, Message: "example cannot be an empty list"}
	}
	return tokens, nil
}

// isUnicodeSpace reports whether r is Unicode whitespace, used by [isWhitespace]
// for the rare non-ASCII cases.
func isUnicodeSpace(r rune) bool {
	return unicode.IsSpace(r)
}
