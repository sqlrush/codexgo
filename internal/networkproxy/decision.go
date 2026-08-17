package networkproxy

import (
	"context"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// NetworkPolicyRequest describes a network request being evaluated. command and
// execPolicyHint let an embedding app map exec approvals to network access.
type NetworkPolicyRequest struct {
	Protocol       NetworkProtocol
	Host           string
	Port           uint16
	ClientAddr     string
	Method         string
	Command        string
	ExecPolicyHint string
}

// NetworkDecisionKind is the high-level decision outcome.
type NetworkDecisionKind int

const (
	// DecisionAllow permits the request.
	DecisionAllow NetworkDecisionKind = iota
	// DecisionDeny rejects the request.
	DecisionDeny
)

// NetworkDecision is the result of policy evaluation. For a deny, Reason,
// Source, and Decision carry the originating context. Reason/Source/Decision are
// the protocol-package wire types reused for interop.
type NetworkDecision struct {
	Kind     NetworkDecisionKind
	Reason   string
	Source   protocol.NetworkDecisionSource
	Decision protocol.NetworkPolicyDecision
}

// Allow returns an allow decision.
func Allow() NetworkDecision {
	return NetworkDecision{Kind: DecisionAllow}
}

// Deny returns a deny decision from the Decider source.
func Deny(reason string) NetworkDecision {
	return DenyWithSource(reason, protocol.NetworkDecisionSourceDecider)
}

// Ask returns an "ask" deny decision from the Decider source.
func Ask(reason string) NetworkDecision {
	return AskWithSource(reason, protocol.NetworkDecisionSourceDecider)
}

// DenyWithSource returns a deny decision attributed to a source.
func DenyWithSource(reason string, source protocol.NetworkDecisionSource) NetworkDecision {
	if reason == "" {
		reason = reasonPolicyDenied
	}
	return NetworkDecision{Kind: DecisionDeny, Reason: reason, Source: source, Decision: protocol.NetworkPolicyDecisionDeny}
}

// AskWithSource returns an "ask" deny decision attributed to a source.
func AskWithSource(reason string, source protocol.NetworkDecisionSource) NetworkDecision {
	if reason == "" {
		reason = reasonPolicyDenied
	}
	return NetworkDecision{Kind: DecisionDeny, Reason: reason, Source: source, Decision: protocol.NetworkPolicyDecisionAsk}
}

// NetworkPolicyDecider can override allowlist-only blocks. It is consulted only
// for not_allowed (allowlist-miss) baseline blocks; explicit deny and
// not_allowed_local always win. Implementations must be safe for concurrent use.
type NetworkPolicyDecider interface {
	Decide(ctx context.Context, req NetworkPolicyRequest) NetworkDecision
}

// DeciderFunc adapts a function to the NetworkPolicyDecider interface.
type DeciderFunc func(ctx context.Context, req NetworkPolicyRequest) NetworkDecision

// Decide implements NetworkPolicyDecider.
func (f DeciderFunc) Decide(ctx context.Context, req NetworkPolicyRequest) NetworkDecision {
	return f(ctx, req)
}

// mapDeciderDecision re-attributes a decider decision to the Decider source,
// mirroring Rust's `map_decider_decision`.
func mapDeciderDecision(decision NetworkDecision) NetworkDecision {
	if decision.Kind == DecisionAllow {
		return decision
	}
	return NetworkDecision{
		Kind:     DecisionDeny,
		Reason:   decision.Reason,
		Source:   protocol.NetworkDecisionSourceDecider,
		Decision: decision.Decision,
	}
}
