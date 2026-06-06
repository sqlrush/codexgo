package execpolicy

import (
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// HeuristicsFallback computes a decision for a command that matched no rule. It
// mirrors the Rust `Fn(&[String]) -> Decision` heuristics callback. A nil
// fallback means "do not synthesize a heuristics match".
type HeuristicsFallback func(cmd []string) Decision

// MatchOptions controls optional matching behavior. It mirrors the Rust
// `MatchOptions` struct.
type MatchOptions struct {
	// ResolveHostExecutables enables basename fallback for absolute program
	// paths, gated by any host_executable() definitions.
	ResolveHostExecutables bool
}

// Policy is an immutable set of compiled rules keyed by their first token,
// together with network rules and host-executable allowlists. It mirrors the
// Rust `Policy` struct.
//
// A Policy is constructed by a [PolicyParser] and is safe to share once built;
// its methods never mutate it.
type Policy struct {
	// rulesByProgram preserves insertion order per program key so that matched
	// rules appear in the same order Rust's multimap yields them.
	rulesByProgram orderedRuleMap
	// networkRules holds the network-policy rules in declaration order. They
	// compile into allow/deny domain lists via CompiledNetworkDomains.
	networkRules []NetworkRule
	// hostExecutablesByName maps a normalized basename to its allowlist of
	// absolute paths. Presence with an empty slice means "no path may resolve
	// via basename fallback"; absence means "unrestricted".
	hostExecutablesByName map[string][]abspath.AbsolutePathBuf
}

// orderedRuleMap is a multimap from program key to rules that preserves both
// insertion order of keys (irrelevant to matching but kept for determinism) and
// insertion order of rules within a key (relevant: matched rules are reported in
// rule-declaration order).
type orderedRuleMap struct {
	rules map[string][]Rule
}

func newOrderedRuleMap() orderedRuleMap {
	return orderedRuleMap{rules: make(map[string][]Rule)}
}

func (m *orderedRuleMap) insert(program string, rule Rule) {
	m.rules[program] = append(m.rules[program], rule)
}

func (m orderedRuleMap) get(program string) ([]Rule, bool) {
	rules, ok := m.rules[program]
	return rules, ok
}

func (m orderedRuleMap) clone() orderedRuleMap {
	cloned := newOrderedRuleMap()
	for program, rules := range m.rules {
		cloned.rules[program] = append([]Rule(nil), rules...)
	}
	return cloned
}

// EmptyPolicy returns a policy with no rules, mirroring Rust's `Policy::empty`.
func EmptyPolicy() *Policy {
	return &Policy{
		rulesByProgram:        newOrderedRuleMap(),
		hostExecutablesByName: make(map[string][]abspath.AbsolutePathBuf),
	}
}

// Rules returns the rules indexed by the given program token and whether any
// exist. The returned slice must not be mutated.
func (p *Policy) Rules(program string) ([]Rule, bool) {
	return p.rulesByProgram.get(program)
}

// NetworkRules returns the policy's network rules in declaration order,
// mirroring Rust's `Policy::network_rules`. The returned slice must not be
// mutated.
func (p *Policy) NetworkRules() []NetworkRule {
	return p.networkRules
}

// HostExecutables returns the allowlist registered for the given basename and
// whether one exists. The returned slice must not be mutated.
func (p *Policy) HostExecutables(name string) ([]abspath.AbsolutePathBuf, bool) {
	paths, ok := p.hostExecutablesByName[name]
	return paths, ok
}

// AddPrefixRule returns a new policy that additionally allows or restricts the
// given prefix, mirroring Rust's `Policy::add_prefix_rule`. Following the
// project's immutability rules, the receiver is not mutated; the result is a
// copy with the rule appended. The prefix must be non-empty.
func (p *Policy) AddPrefixRule(prefix []string, decision Decision) (*Policy, error) {
	if len(prefix) == 0 {
		return nil, &Error{Kind: ErrInvalidPattern, Message: "prefix cannot be empty"}
	}

	rest := make([]PatternToken, len(prefix)-1)
	for i, token := range prefix[1:] {
		rest[i] = NewSinglePatternToken(token)
	}
	rule := &PrefixRule{
		Pattern:  PrefixPattern{first: prefix[0], rest: rest},
		Decision: decision,
	}

	clone := &Policy{
		rulesByProgram:        p.rulesByProgram.clone(),
		networkRules:          append([]NetworkRule(nil), p.networkRules...),
		hostExecutablesByName: cloneHostExecutables(p.hostExecutablesByName),
	}
	clone.rulesByProgram.insert(prefix[0], rule)
	return clone, nil
}

// AddNetworkRule returns a new policy that additionally registers the given
// network rule, mirroring Rust's `Policy::add_network_rule`. The host is
// normalized and validated; a present-but-blank justification is rejected.
// Following the project's immutability rules, the receiver is not mutated.
func (p *Policy) AddNetworkRule(host string, protocol NetworkRuleProtocol, decision Decision, justification *string) (*Policy, error) {
	normalizedHost, err := normalizeNetworkRuleHost(host)
	if err != nil {
		return nil, err
	}
	if justification != nil && isBlank(*justification) {
		return nil, &Error{Kind: ErrInvalidRule, Message: "justification cannot be empty"}
	}

	rule := NetworkRule{
		Host:     normalizedHost,
		Protocol: protocol,
		Decision: decision,
	}
	if justification != nil {
		rule.Justification = *justification
		rule.HasJustification = true
	}

	clone := &Policy{
		rulesByProgram:        p.rulesByProgram.clone(),
		networkRules:          append(append([]NetworkRule(nil), p.networkRules...), rule),
		hostExecutablesByName: cloneHostExecutables(p.hostExecutablesByName),
	}
	return clone, nil
}

// MergeOverlay returns a new policy combining the receiver's rules with the
// overlay's, mirroring Rust's `Policy::merge_overlay`. Prefix rules and network
// rules from the overlay are appended after the base's; host-executable
// allowlists follow last-definition-wins (overlay entries override base ones).
func (p *Policy) MergeOverlay(overlay *Policy) *Policy {
	combinedRules := p.rulesByProgram.clone()
	for program, rules := range overlay.rulesByProgram.rules {
		for _, rule := range rules {
			combinedRules.insert(program, rule)
		}
	}

	combinedNetworkRules := append([]NetworkRule(nil), p.networkRules...)
	combinedNetworkRules = append(combinedNetworkRules, overlay.networkRules...)

	hostExecutables := cloneHostExecutables(p.hostExecutablesByName)
	for name, paths := range overlay.hostExecutablesByName {
		hostExecutables[name] = append([]abspath.AbsolutePathBuf(nil), paths...)
	}

	return &Policy{
		rulesByProgram:        combinedRules,
		networkRules:          combinedNetworkRules,
		hostExecutablesByName: hostExecutables,
	}
}

// CompiledNetworkDomains compiles the network rules into ordered allow/deny
// domain lists, mirroring Rust's `Policy::compiled_network_domains`. Rules are
// processed in declaration order; an Allow moves the host into the allowed list
// (removing any prior deny), a Forbidden moves it into the denied list
// (removing any prior allow), and a Prompt is ignored for domain compilation.
// Within each list a host appears once, at its most recent position.
func (p *Policy) CompiledNetworkDomains() (allowed []string, denied []string) {
	allowed = []string{}
	denied = []string{}
	for _, rule := range p.networkRules {
		switch rule.Decision {
		case DecisionAllow:
			denied = removeDomain(denied, rule.Host)
			allowed = upsertDomain(allowed, rule.Host)
		case DecisionForbidden:
			allowed = removeDomain(allowed, rule.Host)
			denied = upsertDomain(denied, rule.Host)
		case DecisionPrompt:
			// Prompt rules do not contribute to the compiled domain lists.
		}
	}
	return allowed, denied
}

// upsertDomain appends host to entries, moving it to the end if already present,
// mirroring Rust's `upsert_domain`.
func upsertDomain(entries []string, host string) []string {
	entries = removeDomain(entries, host)
	return append(entries, host)
}

// removeDomain drops every occurrence of host from entries, mirroring the
// `retain` calls in Rust's `compiled_network_domains`.
func removeDomain(entries []string, host string) []string {
	out := entries[:0:0]
	for _, entry := range entries {
		if entry != host {
			out = append(out, entry)
		}
	}
	return out
}

func cloneHostExecutables(src map[string][]abspath.AbsolutePathBuf) map[string][]abspath.AbsolutePathBuf {
	cloned := make(map[string][]abspath.AbsolutePathBuf, len(src))
	for name, paths := range src {
		cloned[name] = append([]abspath.AbsolutePathBuf(nil), paths...)
	}
	return cloned
}

// Check evaluates cmd against the policy with default options, mirroring Rust's
// `Policy::check`. When no rule matches and heuristicsFallback is non-nil, a
// single heuristics match is synthesized so the result is always non-empty.
func (p *Policy) Check(cmd []string, heuristicsFallback HeuristicsFallback) Evaluation {
	matched := p.MatchesForCommand(cmd, heuristicsFallback, MatchOptions{})
	return evaluationFromMatches(matched)
}

// CheckWithOptions evaluates cmd against the policy with the given options,
// mirroring Rust's `Policy::check_with_options`.
func (p *Policy) CheckWithOptions(cmd []string, heuristicsFallback HeuristicsFallback, options MatchOptions) Evaluation {
	matched := p.MatchesForCommand(cmd, heuristicsFallback, options)
	return evaluationFromMatches(matched)
}

// CheckMultiple evaluates several commands and aggregates the matches,
// mirroring Rust's `Policy::check_multiple`.
func (p *Policy) CheckMultiple(commands [][]string, heuristicsFallback HeuristicsFallback) Evaluation {
	return p.CheckMultipleWithOptions(commands, heuristicsFallback, MatchOptions{})
}

// CheckMultipleWithOptions evaluates several commands with the given options and
// aggregates the matches, mirroring Rust's
// `Policy::check_multiple_with_options`.
func (p *Policy) CheckMultipleWithOptions(commands [][]string, heuristicsFallback HeuristicsFallback, options MatchOptions) Evaluation {
	var matched []RuleMatch
	for _, command := range commands {
		matched = append(matched, p.MatchesForCommand(command, heuristicsFallback, options)...)
	}
	return evaluationFromMatches(matched)
}

// MatchesForCommand returns the rules matching cmd under the given options,
// mirroring Rust's `Policy::matches_for_command_with_options`.
//
// It first tries exact first-token matches; if none match and
// ResolveHostExecutables is set, it falls back to basename resolution. If still
// nothing matched and heuristicsFallback is non-nil, it returns a single
// synthesized heuristics match (so the result is guaranteed non-empty in that
// case). A nil heuristicsFallback (used during example validation) yields an
// empty slice when nothing matched.
func (p *Policy) MatchesForCommand(cmd []string, heuristicsFallback HeuristicsFallback, options MatchOptions) []RuleMatch {
	matched := p.matchExactRules(cmd)
	if len(matched) == 0 && options.ResolveHostExecutables {
		matched = p.matchHostExecutableRules(cmd)
	}

	if len(matched) == 0 && heuristicsFallback != nil {
		return []RuleMatch{{
			kind:     ruleMatchHeuristics,
			command:  append([]string(nil), cmd...),
			decision: heuristicsFallback(cmd),
		}}
	}
	return matched
}

// matchExactRules returns matches for rules keyed by the command's first token.
// It mirrors Rust's `Policy::match_exact_rules`.
func (p *Policy) matchExactRules(cmd []string) []RuleMatch {
	if len(cmd) == 0 {
		return nil
	}
	rules, ok := p.rulesByProgram.get(cmd[0])
	if !ok {
		return nil
	}
	var matched []RuleMatch
	for _, rule := range rules {
		if m, ok := rule.matches(cmd); ok {
			matched = append(matched, m)
		}
	}
	return matched
}

// matchHostExecutableRules attempts basename fallback for an absolute program
// path, mirroring Rust's `Policy::match_host_executable_rules`.
//
// The first token must be an absolute path; its basename is looked up among the
// rules. If a host_executable() allowlist exists for that basename, the absolute
// path must appear in it. On success the matched rules carry the resolved
// absolute program path.
func (p *Policy) matchHostExecutableRules(cmd []string) []RuleMatch {
	if len(cmd) == 0 {
		return nil
	}
	program, err := abspath.FromAbsolutePathChecked(cmd[0])
	if err != nil {
		return nil
	}
	basename, ok := executablePathLookupKey(program.Path())
	if !ok {
		return nil
	}
	rules, ok := p.rulesByProgram.get(basename)
	if !ok {
		return nil
	}
	if paths, exists := p.hostExecutablesByName[basename]; exists {
		if !containsPath(paths, program) {
			return nil
		}
	}

	basenameCommand := make([]string, 0, len(cmd))
	basenameCommand = append(basenameCommand, basename)
	basenameCommand = append(basenameCommand, cmd[1:]...)

	var matched []RuleMatch
	for _, rule := range rules {
		if m, ok := rule.matches(basenameCommand); ok {
			matched = append(matched, m.withResolvedProgram(program))
		}
	}
	return matched
}

// containsPath reports whether target appears in paths, comparing the
// normalized path strings (mirroring Rust's `AbsolutePathBuf` equality).
func containsPath(paths []abspath.AbsolutePathBuf, target abspath.AbsolutePathBuf) bool {
	for _, path := range paths {
		if path.Path() == target.Path() {
			return true
		}
	}
	return false
}

// Evaluation is the result of checking one or more commands. It mirrors the Rust
// `Evaluation` struct and serializes to the same camelCase JSON shape.
type Evaluation struct {
	Decision     Decision    `json:"decision"`
	MatchedRules []RuleMatch `json:"matchedRules"`
}

// IsMatch reports whether any non-heuristics rule matched, mirroring Rust's
// `Evaluation::is_match`.
func (e Evaluation) IsMatch() bool {
	for _, m := range e.MatchedRules {
		if !m.IsHeuristics() {
			return true
		}
	}
	return false
}

// evaluationFromMatches computes the strictest decision across matches and
// builds the evaluation. The caller must ensure matched is non-empty, matching
// the Rust invariant; an empty slice yields the zero (allow) decision.
func evaluationFromMatches(matched []RuleMatch) Evaluation {
	decision := DecisionAllow
	for i, m := range matched {
		if i == 0 {
			decision = m.Decision()
		} else {
			decision = decision.max(m.Decision())
		}
	}
	return Evaluation{Decision: decision, MatchedRules: matched}
}
