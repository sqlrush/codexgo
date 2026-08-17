package hooks

import (
	"regexp"
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// MatcherPatternForEvent returns the matcher pattern that is meaningful for the
// given event, or nil for events that ignore matchers (UserPromptSubmit, Stop).
// Mirrors matcher_pattern_for_event.
func MatcherPatternForEvent(name protocol.HookEventName, matcher *string) *string {
	switch name {
	case protocol.HookEventNamePreToolUse,
		protocol.HookEventNamePermissionRequest,
		protocol.HookEventNamePostToolUse,
		protocol.HookEventNameSessionStart,
		protocol.HookEventNameSubagentStart,
		protocol.HookEventNameSubagentStop,
		protocol.HookEventNamePreCompact,
		protocol.HookEventNamePostCompact:
		return matcher
	case protocol.HookEventNameUserPromptSubmit, protocol.HookEventNameStop:
		return nil
	default:
		return nil
	}
}

// ValidateMatcherPattern reports whether matcher is a valid matcher: a
// match-all matcher, an exact (pipe-separated) matcher, or a compilable regex.
// Mirrors validate_matcher_pattern.
func ValidateMatcherPattern(matcher string) error {
	if isMatchAllMatcher(matcher) || isExactMatcher(matcher) {
		return nil
	}
	_, err := regexp.Compile(matcher)
	return err
}

// MatchesMatcher reports whether input matches matcher. A nil matcher matches
// everything; an empty or "*" matcher matches everything; an exact matcher uses
// pipe-separated equality; otherwise the matcher is treated as a regex.
//
// Mirrors matches_matcher.
func MatchesMatcher(matcher, input *string) bool {
	if matcher == nil {
		return true
	}
	m := *matcher
	switch {
	case isMatchAllMatcher(m):
		return true
	case isExactMatcher(m):
		if input == nil {
			return false
		}
		for _, candidate := range strings.Split(m, "|") {
			if candidate == *input {
				return true
			}
		}
		return false
	default:
		if input == nil {
			return false
		}
		re, err := regexp.Compile(m)
		if err != nil {
			return false
		}
		return re.MatchString(*input)
	}
}

// MatcherInputs returns the canonical tool name followed by any matcher
// aliases, keeping the canonical name first. Mirrors matcher_inputs.
func MatcherInputs(toolName string, matcherAliases []string) []string {
	out := make([]string, 0, 1+len(matcherAliases))
	out = append(out, toolName)
	out = append(out, matcherAliases...)
	return out
}

func isMatchAllMatcher(matcher string) bool {
	return matcher == "" || matcher == "*"
}

func isExactMatcher(matcher string) bool {
	for _, ch := range matcher {
		isAlnum := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		if !isAlnum && ch != '_' && ch != '|' {
			return false
		}
	}
	return true
}
