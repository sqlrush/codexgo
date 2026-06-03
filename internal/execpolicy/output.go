package execpolicy

import (
	"encoding/json"
	"fmt"
	"os"
)

// execPolicyCheckOutput is the JSON shape printed by the `check` command,
// mirroring the Rust `ExecPolicyCheckOutput`. Unlike [Evaluation], the
// `decision` field is omitted entirely when no rule matched.
type execPolicyCheckOutput struct {
	MatchedRules []RuleMatch `json:"matchedRules"`
	Decision     *Decision   `json:"decision,omitempty"`
}

// FormatMatchesJSON serializes a slice of matches to the same JSON shape Codex's
// `execpolicy check` command emits, mirroring Rust's `format_matches_json`.
//
// The effective `decision` is the strictest severity across all matches and is
// omitted when there are no matches. When pretty is true the output is indented
// with two spaces (matching serde_json::to_string_pretty).
func FormatMatchesJSON(matchedRules []RuleMatch, pretty bool) (string, error) {
	// A nil slice must serialize as `[]` (not `null`) to match serde's
	// representation of an empty `&[RuleMatch]`.
	rules := matchedRules
	if rules == nil {
		rules = []RuleMatch{}
	}
	output := execPolicyCheckOutput{MatchedRules: rules}
	if len(matchedRules) > 0 {
		decision := matchedRules[0].Decision()
		for _, m := range matchedRules[1:] {
			decision = decision.max(m.Decision())
		}
		output.Decision = &decision
	}

	var (
		data []byte
		err  error
	)
	if pretty {
		data, err = json.MarshalIndent(output, "", "  ")
	} else {
		data, err = json.Marshal(output)
	}
	if err != nil {
		return "", fmt.Errorf("execpolicy: serialize matches: %w", err)
	}
	return string(data), nil
}

// LoadPolicies parses every policy file in policyPaths in order and returns the
// merged [Policy], mirroring Rust's `load_policies`. Each file is read from
// disk; a read or parse failure is wrapped with the offending path.
func LoadPolicies(policyPaths []string) (*Policy, error) {
	parser := NewPolicyParser()
	for _, policyPath := range policyPaths {
		contents, err := os.ReadFile(policyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read policy at %s: %w", policyPath, err)
		}
		if err := parser.Parse(policyPath, string(contents)); err != nil {
			return nil, fmt.Errorf("failed to parse policy at %s: %w", policyPath, err)
		}
	}
	return parser.Build(), nil
}
