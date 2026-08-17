package protocol

import (
	"encoding/json"
	"fmt"
)

// AskForApprovalKind is the discriminator for AskForApproval.
type AskForApprovalKind string

const (
	// AskForApprovalUnlessTrusted: only known-safe read-only commands are
	// auto-approved (Rust variant UnlessTrusted, serde rename "untrusted").
	AskForApprovalUnlessTrusted AskForApprovalKind = "untrusted"
	// AskForApprovalOnFailure: deprecated; all commands auto-approved in a
	// sandbox, escalate on failure (kebab-case "on-failure").
	AskForApprovalOnFailure AskForApprovalKind = "on-failure"
	// AskForApprovalOnRequest: the model decides when to ask (default).
	AskForApprovalOnRequest AskForApprovalKind = "on-request"
	// AskForApprovalGranular: fine-grained per-flow controls. Carries a
	// GranularApprovalConfig payload.
	AskForApprovalGranular AskForApprovalKind = "granular"
	// AskForApprovalNever: never ask; failures returned to the model.
	AskForApprovalNever AskForApprovalKind = "never"
)

// AskForApproval determines the conditions under which the user is consulted to
// approve a proposed command.
//
// Rust models this as an externally-tagged enum with kebab-case variant names:
//   - unit variants serialize as bare strings ("untrusted", "on-failure",
//     "on-request", "never");
//   - the Granular(config) tuple variant serializes as
//     {"granular": {<config fields>}}.
//
// We model it as a struct with a Kind discriminator plus an optional Granular
// payload, with custom JSON marshaling to reproduce the exact wire shapes.
type AskForApproval struct {
	Kind AskForApprovalKind

	// Granular is populated only when Kind == AskForApprovalGranular.
	Granular *GranularApprovalConfig
}

// NewAskForApprovalGranular builds a granular approval policy.
func NewAskForApprovalGranular(config GranularApprovalConfig) AskForApproval {
	return AskForApproval{Kind: AskForApprovalGranular, Granular: &config}
}

// MarshalJSON encodes the AskForApproval to its externally-tagged form.
func (a AskForApproval) MarshalJSON() ([]byte, error) {
	switch a.Kind {
	case AskForApprovalUnlessTrusted,
		AskForApprovalOnFailure,
		AskForApprovalOnRequest,
		AskForApprovalNever:
		return json.Marshal(string(a.Kind))
	case AskForApprovalGranular:
		cfg := GranularApprovalConfig{}
		if a.Granular != nil {
			cfg = *a.Granular
		}
		return json.Marshal(map[string]GranularApprovalConfig{"granular": cfg})
	default:
		return nil, fmt.Errorf("unknown AskForApproval kind: %q", a.Kind)
	}
}

// UnmarshalJSON decodes both the bare-string and tagged-object forms.
func (a *AskForApproval) UnmarshalJSON(data []byte) error {
	// Unit variants arrive as bare JSON strings.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch AskForApprovalKind(s) {
		case AskForApprovalUnlessTrusted,
			AskForApprovalOnFailure,
			AskForApprovalOnRequest,
			AskForApprovalNever:
			*a = AskForApproval{Kind: AskForApprovalKind(s)}
			return nil
		default:
			return fmt.Errorf("unknown AskForApproval string variant: %q", s)
		}
	}

	// The Granular variant arrives as {"granular": {...}}.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	raw, ok := obj["granular"]
	if !ok {
		return fmt.Errorf("unknown AskForApproval object variant: %s", string(data))
	}
	var cfg GranularApprovalConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	*a = AskForApproval{Kind: AskForApprovalGranular, Granular: &cfg}
	return nil
}

// GranularApprovalConfig carries fine-grained controls for individual approval
// flows. A true field allows that category; a false field auto-rejects it.
//
// `skill_approval` and `request_permissions` are `#[serde(default)]` in Rust
// (default false), so missing keys decode to false. They are still always
// emitted on marshal (the Rust struct has no skip_serializing_if), matching
// serde's default of always serializing bool fields.
type GranularApprovalConfig struct {
	SandboxApproval    bool `json:"sandbox_approval"`
	Rules              bool `json:"rules"`
	SkillApproval      bool `json:"skill_approval"`
	RequestPermissions bool `json:"request_permissions"`
	McpElicitations    bool `json:"mcp_elicitations"`
}

// NetworkApprovalProtocol identifies the protocol for a network approval
// context. Rust: snake_case. Https accepts the deserialize aliases
// "https_connect" and "http-connect".
type NetworkApprovalProtocol string

const (
	NetworkApprovalProtocolHTTP      NetworkApprovalProtocol = "http"
	NetworkApprovalProtocolHTTPS     NetworkApprovalProtocol = "https"
	NetworkApprovalProtocolSocks5TCP NetworkApprovalProtocol = "socks5_tcp"
	NetworkApprovalProtocolSocks5UDP NetworkApprovalProtocol = "socks5_udp"
)

// NormalizeNetworkApprovalProtocol maps accepted aliases to the canonical
// value, mirroring the Rust serde aliases on the Https variant.
func NormalizeNetworkApprovalProtocol(v string) NetworkApprovalProtocol {
	switch v {
	case "https", "https_connect", "http-connect":
		return NetworkApprovalProtocolHTTPS
	default:
		return NetworkApprovalProtocol(v)
	}
}

// NetworkApprovalContext describes a host/protocol pair awaiting approval.
type NetworkApprovalContext struct {
	Host     string                  `json:"host"`
	Protocol NetworkApprovalProtocol `json:"protocol"`
}

// NetworkPolicyDecision is a proxy network policy decision. Rust (in
// codex_network_proxy): lowercase.
type NetworkPolicyDecision string

const (
	NetworkPolicyDecisionDeny NetworkPolicyDecision = "deny"
	NetworkPolicyDecisionAsk  NetworkPolicyDecision = "ask"
)

// NetworkDecisionSource identifies which layer produced a network decision.
// Rust (in codex_network_proxy): snake_case.
type NetworkDecisionSource string

const (
	NetworkDecisionSourceBaselinePolicy NetworkDecisionSource = "baseline_policy"
	NetworkDecisionSourceModeGuard      NetworkDecisionSource = "mode_guard"
	NetworkDecisionSourceProxyState     NetworkDecisionSource = "proxy_state"
	NetworkDecisionSourceDecider        NetworkDecisionSource = "decider"
)

// NetworkPolicyDecisionPayload describes a network policy decision and its
// originating context. Rust: camelCase. `protocol` uses `#[serde(default)]`.
//
// Note: in Rust this type is Deserialize-only; we add Serialize-friendly tags
// so round-tripping is possible in Go.
type NetworkPolicyDecisionPayload struct {
	Decision NetworkPolicyDecision    `json:"decision"`
	Source   NetworkDecisionSource    `json:"source"`
	Protocol *NetworkApprovalProtocol `json:"protocol,omitempty"`
	Host     *string                  `json:"host"`
	Reason   *string                  `json:"reason"`
	Port     *uint16                  `json:"port"`
}

// IsAskFromDecider reports whether the decision is Ask originating from the
// Decider source.
func (p NetworkPolicyDecisionPayload) IsAskFromDecider() bool {
	return p.Decision == NetworkPolicyDecisionAsk && p.Source == NetworkDecisionSourceDecider
}
