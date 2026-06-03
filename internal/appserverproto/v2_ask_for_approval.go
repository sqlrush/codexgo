package appserverproto

import (
	"encoding/json"
	"fmt"
)

// AskForApprovalV2Kind discriminates the v2 API approval policy. Distinct from
// the internal protocol AskForApproval (which shares the same wire values but is
// owned by codex-protocol); this is the app-server v2::shared mirror.
//
// Rust: v2::shared::AskForApproval, serde rename_all = "kebab-case", with
// UnlessTrusted renamed to "untrusted".
type AskForApprovalV2Kind string

const (
	// AskForApprovalV2UnlessTrusted only auto-approves known-safe commands.
	AskForApprovalV2UnlessTrusted AskForApprovalV2Kind = "untrusted"
	// AskForApprovalV2OnFailure escalates on sandbox failure.
	AskForApprovalV2OnFailure AskForApprovalV2Kind = "on-failure"
	// AskForApprovalV2OnRequest lets the model decide when to ask.
	AskForApprovalV2OnRequest AskForApprovalV2Kind = "on-request"
	// AskForApprovalV2Granular carries fine-grained per-flow controls
	// (experimental: askForApproval.granular).
	AskForApprovalV2Granular AskForApprovalV2Kind = "granular"
	// AskForApprovalV2Never never asks; failures are returned to the model.
	AskForApprovalV2Never AskForApprovalV2Kind = "never"
)

// GranularApprovalConfigV2 holds the v2 granular approval controls. The
// skill_approval and request_permissions fields are `#[serde(default)]` in Rust
// (default false), so missing keys decode to false; all fields are always
// emitted on marshal (no skip_serializing_if).
type GranularApprovalConfigV2 struct {
	SandboxApproval    bool `json:"sandbox_approval"`
	Rules              bool `json:"rules"`
	SkillApproval      bool `json:"skill_approval"`
	RequestPermissions bool `json:"request_permissions"`
	McpElicitations    bool `json:"mcp_elicitations"`
}

// AskForApprovalV2 is the v2 API approval policy. Unit variants serialize as
// bare kebab-case strings; the Granular variant serializes as
// {"granular": {<config fields>}}.
type AskForApprovalV2 struct {
	Kind AskForApprovalV2Kind

	// Granular is populated only when Kind == AskForApprovalV2Granular.
	Granular *GranularApprovalConfigV2
}

// NewAskForApprovalV2Granular builds a granular v2 approval policy.
func NewAskForApprovalV2Granular(config GranularApprovalConfigV2) AskForApprovalV2 {
	return AskForApprovalV2{Kind: AskForApprovalV2Granular, Granular: &config}
}

// MarshalJSON encodes the externally-tagged v2 approval policy.
func (a AskForApprovalV2) MarshalJSON() ([]byte, error) {
	switch a.Kind {
	case AskForApprovalV2UnlessTrusted,
		AskForApprovalV2OnFailure,
		AskForApprovalV2OnRequest,
		AskForApprovalV2Never:
		return json.Marshal(string(a.Kind))
	case AskForApprovalV2Granular:
		cfg := GranularApprovalConfigV2{}
		if a.Granular != nil {
			cfg = *a.Granular
		}
		return json.Marshal(map[string]GranularApprovalConfigV2{"granular": cfg})
	default:
		return nil, fmt.Errorf("appserverproto: unknown AskForApprovalV2 kind: %q", a.Kind)
	}
}

// UnmarshalJSON decodes both the bare-string and tagged-object forms.
func (a *AskForApprovalV2) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch AskForApprovalV2Kind(s) {
		case AskForApprovalV2UnlessTrusted,
			AskForApprovalV2OnFailure,
			AskForApprovalV2OnRequest,
			AskForApprovalV2Never:
			*a = AskForApprovalV2{Kind: AskForApprovalV2Kind(s)}
			return nil
		default:
			return fmt.Errorf("appserverproto: unknown AskForApprovalV2 string variant: %q", s)
		}
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	raw, ok := obj["granular"]
	if !ok {
		return fmt.Errorf("appserverproto: unknown AskForApprovalV2 object variant: %s", string(data))
	}
	var cfg GranularApprovalConfigV2
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	*a = AskForApprovalV2{Kind: AskForApprovalV2Granular, Granular: &cfg}
	return nil
}
