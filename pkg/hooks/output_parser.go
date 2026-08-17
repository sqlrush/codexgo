package hooks

import (
	"encoding/json"
	"strings"
)

// universalOutput is the parsed, semantic form of the universal envelope.
// Mirrors UniversalOutput.
//
// (universalOutput is declared in output_wire.go.)

// sessionStartOutput mirrors SessionStartOutput.
type sessionStartOutput struct {
	universal         universalOutput
	additionalContext *string
}

// preToolUseOutput mirrors PreToolUseOutput.
type preToolUseOutput struct {
	universal         universalOutput
	blockReason       *string
	additionalContext *string
	updatedInput      json.RawMessage
	invalidReason     *string
}

// permissionRequestDecisionKind discriminates the parsed permission decision.
type permissionRequestDecisionKind int

const (
	permissionRequestDecisionNone permissionRequestDecisionKind = iota
	permissionRequestDecisionAllow
	permissionRequestDecisionDeny
)

// parsedPermissionRequestDecision is the parsed allow/deny decision. Mirrors the
// output_parser PermissionRequestDecision enum (distinct from the public
// PermissionRequestDecision exposed by the permission_request event).
type parsedPermissionRequestDecision struct {
	kind    permissionRequestDecisionKind
	message string
}

// permissionRequestOutput mirrors PermissionRequestOutput.
type permissionRequestOutput struct {
	universal     universalOutput
	decision      *parsedPermissionRequestDecision
	invalidReason *string
}

// postToolUseOutput mirrors PostToolUseOutput.
type postToolUseOutput struct {
	universal          universalOutput
	shouldBlock        bool
	reason             *string
	invalidBlockReason *string
	additionalContext  *string
	invalidReason      *string
}

// userPromptSubmitOutput mirrors UserPromptSubmitOutput.
type userPromptSubmitOutput struct {
	universal          universalOutput
	shouldBlock        bool
	reason             *string
	invalidBlockReason *string
	additionalContext  *string
}

// stopOutput mirrors StopOutput.
type stopOutput struct {
	universal          universalOutput
	shouldBlock        bool
	reason             *string
	invalidBlockReason *string
}

// preCompactOutput mirrors PreCompactOutput.
type preCompactOutput struct {
	universal     universalOutput
	invalidReason *string
}

// statelessHookOutput mirrors StatelessHookOutput.
type statelessHookOutput struct {
	universal     universalOutput
	invalidReason *string
}

// --- command output wire wrappers ---
//
// Each wrapper flattens the universal envelope (continue/stopReason/
// suppressOutput/systemMessage) and adds event-specific fields. Rust marks all
// of these `deny_unknown_fields`; parseJSON enforces that by requiring an object
// and rejecting on any decode failure (which would surface as a None just like
// serde_json::from_value returning Err).

// parseUniversalWire decodes the shared camelCase envelope with continue
// defaulting to true. Mirrors HookUniversalOutputWire.
func parseUniversalWire(m map[string]json.RawMessage) (universalOutput, error) {
	out := universalOutput{ContinueProcessing: true}
	if raw, ok := m["continue"]; ok {
		if err := json.Unmarshal(raw, &out.ContinueProcessing); err != nil {
			return out, err
		}
	}
	if raw, ok := m["stopReason"]; ok {
		if err := json.Unmarshal(raw, &out.StopReason); err != nil {
			return out, err
		}
	}
	if raw, ok := m["suppressOutput"]; ok {
		if err := json.Unmarshal(raw, &out.SuppressOutput); err != nil {
			return out, err
		}
	}
	if raw, ok := m["systemMessage"]; ok {
		if err := json.Unmarshal(raw, &out.SystemMessage); err != nil {
			return out, err
		}
	}
	return out, nil
}

// objectFields parses a non-empty, trimmed object into a field map. Returns
// (nil, false) when stdout is empty or not a JSON object, mirroring parse_json's
// early return of None.
func objectFields(stdout string) (map[string]json.RawMessage, bool) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return nil, false
	}
	var probe any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil, false
	}
	if _, ok := probe.(map[string]any); !ok {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return nil, false
	}
	return m, true
}

// rejectUnknown reports whether m has keys outside allowed (deny_unknown_fields).
func rejectUnknown(m map[string]json.RawMessage, allowed ...string) bool {
	set := make(map[string]struct{}, len(allowed))
	for _, k := range allowed {
		set[k] = struct{}{}
	}
	for k := range m {
		if _, ok := set[k]; !ok {
			return true
		}
	}
	return false
}

// looksLikeJSON reports whether stdout begins (after leading whitespace) with a
// JSON object/array opener. Mirrors looks_like_json.
func looksLikeJSON(stdout string) bool {
	trimmed := strings.TrimLeft(stdout, " \t\r\n")
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

func optStringField(m map[string]json.RawMessage, key string) (*string, bool) {
	raw, ok := m[key]
	if !ok {
		return nil, true
	}
	var s *string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func optRawField(m map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	raw, ok := m[key]
	if !ok {
		return nil, false
	}
	if string(raw) == "null" {
		return nil, false
	}
	return raw, true
}
