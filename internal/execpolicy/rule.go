package execpolicy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// PatternTokenKind distinguishes the two shapes a [PatternToken] can take.
type PatternTokenKind int

const (
	// PatternTokenSingle matches one fixed string.
	PatternTokenSingle PatternTokenKind = iota
	// PatternTokenAlts matches any one of several allowed alternatives.
	PatternTokenAlts
)

// PatternToken matches a single command token, either a fixed string or one of
// several allowed alternatives. It mirrors the Rust `PatternToken` enum. Values
// are immutable: the slice of alternatives is never mutated after construction.
type PatternToken struct {
	kind PatternTokenKind
	// value holds the fixed string for a Single token.
	value string
	// alts holds the alternatives for an Alts token.
	alts []string
}

// NewSinglePatternToken returns a [PatternToken] that matches exactly value.
func NewSinglePatternToken(value string) PatternToken {
	return PatternToken{kind: PatternTokenSingle, value: value}
}

// NewAltsPatternToken returns a [PatternToken] that matches any of alts. The
// slice is copied so the result is independent of the caller's slice.
func NewAltsPatternToken(alts []string) PatternToken {
	cloned := append([]string(nil), alts...)
	return PatternToken{kind: PatternTokenAlts, alts: cloned}
}

// Kind reports whether the token is a single value or a set of alternatives.
func (t PatternToken) Kind() PatternTokenKind { return t.kind }

// matches reports whether token satisfies this pattern token.
func (t PatternToken) matches(token string) bool {
	switch t.kind {
	case PatternTokenSingle:
		return t.value == token
	case PatternTokenAlts:
		for _, alt := range t.alts {
			if alt == token {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// Alternatives returns the slice of acceptable values for this token. For a
// Single token it returns a one-element slice, mirroring Rust's
// `PatternToken::alternatives`. The returned slice must not be mutated.
func (t PatternToken) Alternatives() []string {
	if t.kind == PatternTokenSingle {
		return []string{t.value}
	}
	return t.alts
}

// equal reports structural equality, used by tests.
func (t PatternToken) equal(other PatternToken) bool {
	if t.kind != other.kind {
		return false
	}
	if t.kind == PatternTokenSingle {
		return t.value == other.value
	}
	if len(t.alts) != len(other.alts) {
		return false
	}
	for i := range t.alts {
		if t.alts[i] != other.alts[i] {
			return false
		}
	}
	return true
}

// debug formats the token like Rust's `{:?}` derive for `PatternToken`, used to
// render rules inside example-validation error messages.
func (t PatternToken) debug() string {
	if t.kind == PatternTokenSingle {
		return fmt.Sprintf("Single(%q)", t.value)
	}
	parts := make([]string, len(t.alts))
	for i, alt := range t.alts {
		parts[i] = fmt.Sprintf("%q", alt)
	}
	return "Alts([" + strings.Join(parts, ", ") + "])"
}

// PrefixPattern matches the leading tokens of a command. The first token is
// fixed because policies are keyed by their first token. It mirrors the Rust
// `PrefixPattern` struct. Values are immutable.
type PrefixPattern struct {
	first string
	rest  []PatternToken
}

// First returns the fixed leading token of the pattern.
func (p PrefixPattern) First() string { return p.first }

// Rest returns the remaining pattern tokens. The returned slice must not be
// mutated.
func (p PrefixPattern) Rest() []PatternToken { return p.rest }

// matchesPrefix reports whether cmd begins with this pattern. On success it
// returns a fresh copy of the matched prefix tokens; on failure ok=false. It
// mirrors Rust's `PrefixPattern::matches_prefix`.
func (p PrefixPattern) matchesPrefix(cmd []string) ([]string, bool) {
	patternLength := len(p.rest) + 1
	if len(cmd) < patternLength || cmd[0] != p.first {
		return nil, false
	}
	for i, patternToken := range p.rest {
		if !patternToken.matches(cmd[i+1]) {
			return nil, false
		}
	}
	matched := make([]string, patternLength)
	copy(matched, cmd[:patternLength])
	return matched, true
}

// PrefixRule assigns a [Decision] (and optional justification) to commands that
// begin with its [PrefixPattern]. It mirrors the Rust `PrefixRule` struct and
// implements [Rule].
type PrefixRule struct {
	Pattern       PrefixPattern
	Decision      Decision
	Justification string
	// HasJustification distinguishes an absent justification (Rust `None`)
	// from an empty one. Empty justifications are rejected at parse time, so
	// in practice this is true exactly when a non-empty justification was set.
	HasJustification bool
}

// Program returns the rule's keying token, mirroring Rust's
// `Rule::program` for `PrefixRule`.
func (r *PrefixRule) Program() string { return r.Pattern.first }

// matches returns a [RuleMatch] when cmd satisfies the rule's prefix pattern.
// It mirrors Rust's `Rule::matches` for `PrefixRule`.
func (r *PrefixRule) matches(cmd []string) (RuleMatch, bool) {
	matched, ok := r.Pattern.matchesPrefix(cmd)
	if !ok {
		return RuleMatch{}, false
	}
	return RuleMatch{
		kind:             ruleMatchPrefix,
		matchedPrefix:    matched,
		decision:         r.Decision,
		justification:    r.Justification,
		hasJustification: r.HasJustification,
	}, true
}

// debug formats the rule like Rust's `{:?}` derive for `PrefixRule`, used to
// render rules inside example-validation error messages.
func (r *PrefixRule) debug() string {
	restParts := make([]string, len(r.Pattern.rest))
	for i, token := range r.Pattern.rest {
		restParts[i] = token.debug()
	}
	justification := "None"
	if r.HasJustification {
		justification = fmt.Sprintf("Some(%q)", r.Justification)
	}
	return fmt.Sprintf(
		"PrefixRule { pattern: PrefixPattern { first: %q, rest: [%s] }, decision: %s, justification: %s }",
		r.Pattern.first,
		strings.Join(restParts, ", "),
		decisionDebug(r.Decision),
		justification,
	)
}

// decisionDebug formats a decision like Rust's `{:?}` derive for `Decision`.
func decisionDebug(d Decision) string {
	switch d {
	case DecisionAllow:
		return "Allow"
	case DecisionPrompt:
		return "Prompt"
	case DecisionForbidden:
		return "Forbidden"
	default:
		return fmt.Sprintf("Decision(%d)", int(d))
	}
}

// Rule is the behavior shared by all policy rules. It mirrors the Rust `Rule`
// trait. Only [PrefixRule] implements it in this port; the interface is kept so
// the matching engine can treat rules uniformly.
type Rule interface {
	// Program returns the token under which the rule is indexed.
	Program() string
	// matches returns a [RuleMatch] when cmd satisfies the rule.
	matches(cmd []string) (RuleMatch, bool)
	// debug formats the rule for error messages.
	debug() string
}

// ruleMatchKind distinguishes the two shapes a [RuleMatch] can take.
type ruleMatchKind int

const (
	ruleMatchPrefix ruleMatchKind = iota
	ruleMatchHeuristics
)

// RuleMatch records a single rule that matched a command (or the heuristics
// fallback used when no rule matched). It mirrors the Rust `RuleMatch` enum and
// serializes to the same externally-tagged JSON shape.
type RuleMatch struct {
	kind ruleMatchKind

	// Fields for ruleMatchPrefix.
	matchedPrefix      []string
	decision           Decision
	resolvedProgram    abspath.AbsolutePathBuf
	hasResolvedProgram bool
	justification      string
	hasJustification   bool

	// Fields for ruleMatchHeuristics.
	command []string
}

// Decision returns the decision carried by the match, mirroring Rust's
// `RuleMatch::decision`.
func (m RuleMatch) Decision() Decision { return m.decision }

// IsHeuristics reports whether the match is the heuristics fallback rather than
// a real rule match.
func (m RuleMatch) IsHeuristics() bool { return m.kind == ruleMatchHeuristics }

// withResolvedProgram returns a copy of the match with its resolved program set,
// mirroring Rust's `RuleMatch::with_resolved_program`. Heuristics matches are
// returned unchanged.
func (m RuleMatch) withResolvedProgram(resolved abspath.AbsolutePathBuf) RuleMatch {
	if m.kind != ruleMatchPrefix {
		return m
	}
	clone := m
	clone.resolvedProgram = resolved
	clone.hasResolvedProgram = true
	return clone
}

// debug formats the match like Rust's `{:?}` derive for `RuleMatch`, used in
// the ExampleDidMatch error message.
func (m RuleMatch) debug() string {
	if m.kind == ruleMatchHeuristics {
		return fmt.Sprintf(
			"HeuristicsRuleMatch { command: %s, decision: %s }",
			debugStringSlice(m.command),
			decisionDebug(m.decision),
		)
	}
	resolved := "None"
	if m.hasResolvedProgram {
		resolved = fmt.Sprintf("Some(%q)", m.resolvedProgram.String())
	}
	justification := "None"
	if m.hasJustification {
		justification = fmt.Sprintf("Some(%q)", m.justification)
	}
	return fmt.Sprintf(
		"PrefixRuleMatch { matched_prefix: %s, decision: %s, resolved_program: %s, justification: %s }",
		debugStringSlice(m.matchedPrefix),
		decisionDebug(m.decision),
		resolved,
		justification,
	)
}

// prefixRuleMatchJSON is the serde shape of the PrefixRuleMatch variant body.
type prefixRuleMatchJSON struct {
	MatchedPrefix   []string `json:"matchedPrefix"`
	Decision        Decision `json:"decision"`
	ResolvedProgram *string  `json:"resolvedProgram,omitempty"`
	Justification   *string  `json:"justification,omitempty"`
}

// heuristicsRuleMatchJSON is the serde shape of the HeuristicsRuleMatch variant
// body.
type heuristicsRuleMatchJSON struct {
	Command  []string `json:"command"`
	Decision Decision `json:"decision"`
}

// MarshalJSON renders the match using Rust's externally-tagged serde
// representation: `{"prefixRuleMatch": {...}}` or `{"heuristicsRuleMatch":
// {...}}`. The `resolvedProgram` and `justification` fields are omitted when
// absent, matching `skip_serializing_if = "Option::is_none"`.
func (m RuleMatch) MarshalJSON() ([]byte, error) {
	switch m.kind {
	case ruleMatchPrefix:
		body := prefixRuleMatchJSON{
			MatchedPrefix: emptyToSlice(m.matchedPrefix),
			Decision:      m.decision,
		}
		if m.hasResolvedProgram {
			resolved := m.resolvedProgram.String()
			body.ResolvedProgram = &resolved
		}
		if m.hasJustification {
			justification := m.justification
			body.Justification = &justification
		}
		return json.Marshal(map[string]prefixRuleMatchJSON{"prefixRuleMatch": body})
	case ruleMatchHeuristics:
		body := heuristicsRuleMatchJSON{
			Command:  emptyToSlice(m.command),
			Decision: m.decision,
		}
		return json.Marshal(map[string]heuristicsRuleMatchJSON{"heuristicsRuleMatch": body})
	default:
		return nil, fmt.Errorf("execpolicy: cannot marshal invalid rule match kind %d", int(m.kind))
	}
}

// emptyToSlice ensures a nil slice marshals as `[]` rather than `null`, matching
// serde's representation of an empty `Vec`.
func emptyToSlice(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// debugStringSlice formats a slice of strings like Rust's `{:?}` for a
// `Vec<String>`, e.g. `["a", "b"]`.
func debugStringSlice(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprintf("%q", value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
