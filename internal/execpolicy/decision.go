// Package execpolicy ports the OpenAI Codex Rust crate
// `codex-rs/execpolicy` to Go as part of a faithful, drop-in-compatible
// reimplementation of Codex 0.136.0.
//
// It is the Starlark-based command-approval engine. Policy files are written in
// Starlark and evaluated with go.starlark.net. The engine exposes three builtins
// used by Codex policies:
//
//   - prefix_rule(pattern, decision, justification, match, not_match)
//   - network_rule(host, protocol, decision, justification)
//   - host_executable(name, paths)
//
// network_rule entries register per-host network-policy decisions; they compile
// into ordered allow/deny domain lists via [Policy.CompiledNetworkDomains],
// which the network sandbox policy consumes.
//
// A command is checked against the loaded rules with ordered token matching: a
// list token denotes OR alternatives, an exact first-token (absolute path) match
// wins, and otherwise a basename fallback resolves the program (optionally gated
// by host_executable allowlists). The strictest decision across all matched
// rules wins (forbidden > prompt > allow).
//
// The result is serialized to the same JSON shape Codex emits via serde:
//
//	{
//	  "matchedRules": [
//	    {
//	      "prefixRuleMatch": {
//	        "matchedPrefix": ["..."],
//	        "decision": "allow|prompt|forbidden",
//	        "resolvedProgram": "/abs/path",
//	        "justification": "..."
//	      }
//	    }
//	  ],
//	  "decision": "allow|prompt|forbidden"
//	}
package execpolicy

import "fmt"

// Decision is the outcome a rule assigns to a command. Ordering matters: the
// engine selects the strictest (numerically largest) decision across all
// matches, so the constants are declared in increasing order of severity to
// mirror the Rust `#[derive(Ord)]` on the enum.
type Decision int

const (
	// DecisionAllow lets the command run without further approval.
	DecisionAllow Decision = iota
	// DecisionPrompt requests explicit user approval and is rejected outright
	// under approval_policy="never".
	DecisionPrompt
	// DecisionForbidden blocks the command without further consideration.
	DecisionForbidden
)

// ParseDecision parses a policy `decision` string. It mirrors
// Rust's `Decision::parse`, accepting only "allow", "prompt", or "forbidden".
func ParseDecision(raw string) (Decision, error) {
	switch raw {
	case "allow":
		return DecisionAllow, nil
	case "prompt":
		return DecisionPrompt, nil
	case "forbidden":
		return DecisionForbidden, nil
	default:
		return 0, &Error{Kind: ErrInvalidDecision, Message: raw}
	}
}

// String returns the camelCase serde representation of the decision, matching
// the Rust `#[serde(rename_all = "camelCase")]` derive. All three values are
// single lowercase words, so the representation equals the policy string.
func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return "allow"
	case DecisionPrompt:
		return "prompt"
	case DecisionForbidden:
		return "forbidden"
	default:
		return fmt.Sprintf("Decision(%d)", int(d))
	}
}

// MarshalJSON renders the decision as its camelCase string, matching serde.
func (d Decision) MarshalJSON() ([]byte, error) {
	switch d {
	case DecisionAllow, DecisionPrompt, DecisionForbidden:
		return []byte(`"` + d.String() + `"`), nil
	default:
		return nil, fmt.Errorf("execpolicy: cannot marshal invalid decision %d", int(d))
	}
}

// UnmarshalJSON parses a camelCase decision string, matching serde.
func (d *Decision) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("execpolicy: decision must be a JSON string, got %s", data)
	}
	parsed, err := ParseDecision(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// max returns the stricter of two decisions (the larger value).
func (d Decision) max(other Decision) Decision {
	if other > d {
		return other
	}
	return d
}
