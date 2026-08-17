package core

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the approval *decisioning* primitives from the Rust
// codex-core tools::sandboxing module (the AskForApproval x sandbox logic and
// the "Approved for session" memory), independent of the event/IO machinery in
// approvals.go.

// ----------------------------------------------------------------------------
// ApprovedForSession memory (ApprovalStore)
// ----------------------------------------------------------------------------

// ApprovalStore caches per-key approval decisions for the lifetime of a session,
// implementing the "Allow, don't ask again" memory. Keys are serialized to JSON
// so heterogeneous key types can share one store, mirroring the Rust
// `ApprovalStore`.
//
// Only [protocol.ReviewDecisionApprovedForSession] entries are meaningful for
// skip-prompting; other decisions are not persisted by [WithCachedApproval].
//
// ApprovalStore is safe for concurrent use.
type ApprovalStore struct {
	mu  sync.Mutex
	mem map[string]protocol.ReviewDecision
}

// NewApprovalStore builds an empty store.
func NewApprovalStore() *ApprovalStore {
	return &ApprovalStore{mem: make(map[string]protocol.ReviewDecision)}
}

// Get returns the cached decision for key and whether one was present. The key
// is serialized to JSON to match the Rust serialized-key cache.
func (s *ApprovalStore) Get(key any) (protocol.ReviewDecision, bool) {
	k, err := json.Marshal(key)
	if err != nil {
		return protocol.ReviewDecision{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.mem[string(k)]
	return d, ok
}

// Put records a decision for key. Errors serializing the key are ignored
// (the entry is simply not cached), matching the Rust best-effort behavior.
func (s *ApprovalStore) Put(key any, decision protocol.ReviewDecision) {
	k, err := json.Marshal(key)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mem[string(k)] = decision
}

// isApprovedForSession reports whether the cached decision for key is
// ApprovedForSession.
func (s *ApprovalStore) isApprovedForSession(key any) bool {
	d, ok := s.Get(key)
	return ok && d.Kind == protocol.ReviewDecisionApprovedForSession
}

// WithCachedApproval consults the ApprovedForSession memory before prompting.
//
//   - If every key in keys is already ApprovedForSession, it returns
//     ApprovedForSession without invoking fetch (no prompt).
//   - Otherwise it invokes fetch to obtain a fresh decision and, when that
//     decision is ApprovedForSession, records it for every key so future
//     requests touching any subset can also skip prompting.
//
// An empty key list always invokes fetch (defensive, matching Rust).
func (s *ApprovalStore) WithCachedApproval(
	ctx context.Context,
	keys []any,
	fetch func(ctx context.Context) protocol.ReviewDecision,
) protocol.ReviewDecision {
	if len(keys) == 0 {
		return fetch(ctx)
	}

	allApproved := true
	for _, key := range keys {
		if !s.isApprovedForSession(key) {
			allApproved = false
			break
		}
	}
	if allApproved {
		return protocol.ReviewDecision{Kind: protocol.ReviewDecisionApprovedForSession}
	}

	decision := fetch(ctx)
	if decision.Kind == protocol.ReviewDecisionApprovedForSession {
		for _, key := range keys {
			s.Put(key, decision)
		}
	}
	return decision
}

// ----------------------------------------------------------------------------
// AskForApproval x sandbox decisioning
// ----------------------------------------------------------------------------

// ExecApprovalRequirementKind discriminates an [ExecApprovalRequirement].
type ExecApprovalRequirementKind int

const (
	// ExecApprovalSkip means no approval is required for the tool call.
	ExecApprovalSkip ExecApprovalRequirementKind = iota
	// ExecApprovalNeedsApproval means the user must be consulted.
	ExecApprovalNeedsApproval
	// ExecApprovalForbidden means execution is disallowed by policy.
	ExecApprovalForbidden
)

// ExecApprovalRequirement is the decision of [DefaultExecApprovalRequirement]:
// whether a command may run without prompting, requires approval, or is
// forbidden. It is a reduced port of the Rust `ExecApprovalRequirement` enum.
type ExecApprovalRequirement struct {
	Kind ExecApprovalRequirementKind

	// BypassSandbox is meaningful for the Skip variant: when true the first
	// attempt should run without a sandbox.
	BypassSandbox bool
	// Reason carries the human-readable reason for the Forbidden variant (and the
	// optional reason for NeedsApproval).
	Reason *string
	// ProposedExecpolicyAmendment is an optional execpolicy amendment carried by
	// the Skip/NeedsApproval variants.
	ProposedExecpolicyAmendment *protocol.ExecPolicyAmendment
}

// FilesystemRestricted reports whether the active filesystem sandbox restricts
// access. Core does not own the rich permission/sandbox policy types (they live
// in the permissions/sandbox area), so the turn-running approval path consumes a
// single boolean: true when reads/writes are constrained (Restricted), false
// when filesystem access is unrestricted. The caller derives it from the
// effective [protocol.RequestPermissionProfile] / file-system sandbox policy.
//
// STUB: the full FileSystemSandboxPolicy (writable roots, denied reads, full
// disk access) is owned by the sandbox area; here it is collapsed to this flag
// plus DeniedReadRestrictions below.
type FilesystemSandboxState struct {
	// Restricted reports whether the filesystem sandbox constrains access.
	Restricted bool
	// DeniedReadRestrictions reports whether the policy contains denied-read
	// paths, which only exist inside the sandbox (so unsandboxed execution would
	// silently grant them).
	DeniedReadRestrictions bool
}

// DefaultExecApprovalRequirement decides whether a command needs approval under
// the given approval policy and filesystem sandbox state. It is a faithful port
// of the Rust `default_exec_approval_requirement`:
//
//   - Never, OnFailure: do not ask.
//   - OnRequest: ask only when filesystem access is restricted.
//   - Granular: ask only when filesystem access is restricted, but forbid when
//     granular sandbox approval is disabled.
//   - UnlessTrusted: always ask.
func DefaultExecApprovalRequirement(policy protocol.AskForApproval, fs FilesystemSandboxState) ExecApprovalRequirement {
	var needsApproval bool
	switch policy.Kind {
	case protocol.AskForApprovalNever, protocol.AskForApprovalOnFailure:
		needsApproval = false
	case protocol.AskForApprovalOnRequest, protocol.AskForApprovalGranular:
		needsApproval = fs.Restricted
	case protocol.AskForApprovalUnlessTrusted:
		needsApproval = true
	default:
		needsApproval = false
	}

	if needsApproval && policy.Kind == protocol.AskForApprovalGranular && !GranularAllowsSandboxApproval(policy.Granular) {
		reason := "approval policy disallowed sandbox approval prompt"
		return ExecApprovalRequirement{Kind: ExecApprovalForbidden, Reason: &reason}
	}
	if needsApproval {
		return ExecApprovalRequirement{Kind: ExecApprovalNeedsApproval}
	}
	return ExecApprovalRequirement{Kind: ExecApprovalSkip, BypassSandbox: false}
}

// SandboxOverride mirrors the Rust `SandboxOverride`: whether the first
// execution attempt should bypass the sandbox.
type SandboxOverride int

const (
	// SandboxNoOverride keeps the ambient sandbox for the first attempt.
	SandboxNoOverride SandboxOverride = iota
	// SandboxBypassFirstAttempt runs the first attempt without a sandbox.
	SandboxBypassFirstAttempt
)

// SandboxOverrideForFirstAttempt decides whether to bypass the sandbox on the
// first attempt, faithfully porting the Rust
// `sandbox_override_for_first_attempt`.
//
//   - Denied-read restrictions force NoOverride (they only exist inside the
//     sandbox).
//   - An explicit Skip+bypass requirement (full trust) bypasses the sandbox.
//   - A request that requires escalated permissions bypasses the sandbox.
func SandboxOverrideForFirstAttempt(
	requiresEscalatedPermissions bool,
	requirement ExecApprovalRequirement,
	fs FilesystemSandboxState,
) SandboxOverride {
	if !UnsandboxedExecutionAllowed(fs) {
		return SandboxNoOverride
	}
	if requirement.Kind == ExecApprovalSkip && requirement.BypassSandbox {
		return SandboxBypassFirstAttempt
	}
	if requiresEscalatedPermissions {
		return SandboxBypassFirstAttempt
	}
	return SandboxNoOverride
}

// UnsandboxedExecutionAllowed reports whether the active filesystem policy can be
// represented by running without a sandbox (Rust `unsandboxed_execution_allowed`).
func UnsandboxedExecutionAllowed(fs FilesystemSandboxState) bool {
	return !fs.DeniedReadRestrictions
}

// GranularAllowsSandboxApproval reports whether a granular config permits
// sandbox-approval prompts (Rust `GranularApprovalConfig::allows_sandbox_approval`).
// A nil config is treated as all-disabled.
func GranularAllowsSandboxApproval(cfg *protocol.GranularApprovalConfig) bool {
	return cfg != nil && cfg.SandboxApproval
}

// granularAllowsRequestPermissions reports whether a granular config permits
// request_permissions prompts (Rust `allows_request_permissions`).
func granularAllowsRequestPermissions(cfg *protocol.GranularApprovalConfig) bool {
	return cfg != nil && cfg.RequestPermissions
}

// DefaultAvailableDecisions computes the default set of decisions offered to the
// user for an exec approval, faithfully porting
// `ExecApprovalRequestEvent::default_available_decisions`.
//
//   - A network approval context offers Approved + ApprovedForSession, the
//     "allow" network policy amendment (when present), then Abort.
//   - Additional permissions offer Approved + Abort.
//   - Otherwise: Approved, the proposed execpolicy amendment (when present),
//     then Abort.
func DefaultAvailableDecisions(
	networkApprovalContext *protocol.NetworkApprovalContext,
	proposedExecpolicyAmendment *protocol.ExecPolicyAmendment,
	proposedNetworkPolicyAmendments []protocol.NetworkPolicyAmendment,
	additionalPermissions *protocol.AdditionalPermissionProfile,
) []protocol.ReviewDecision {
	if networkApprovalContext != nil {
		decisions := []protocol.ReviewDecision{
			{Kind: protocol.ReviewDecisionApproved},
			{Kind: protocol.ReviewDecisionApprovedForSession},
		}
		for _, amendment := range proposedNetworkPolicyAmendments {
			if amendment.Action == protocol.NetworkPolicyRuleActionAllow {
				a := amendment
				decisions = append(decisions, protocol.ReviewDecision{
					Kind:                   protocol.ReviewDecisionNetworkPolicyAmendment,
					NetworkPolicyAmendment: &a,
				})
				break
			}
		}
		decisions = append(decisions, protocol.ReviewDecision{Kind: protocol.ReviewDecisionAbort})
		return decisions
	}

	if additionalPermissions != nil {
		return []protocol.ReviewDecision{
			{Kind: protocol.ReviewDecisionApproved},
			{Kind: protocol.ReviewDecisionAbort},
		}
	}

	decisions := []protocol.ReviewDecision{{Kind: protocol.ReviewDecisionApproved}}
	if proposedExecpolicyAmendment != nil {
		amend := *proposedExecpolicyAmendment
		decisions = append(decisions, protocol.ReviewDecision{
			Kind:                        protocol.ReviewDecisionApprovedExecpolicyAmendment,
			ProposedExecpolicyAmendment: &amend,
		})
	}
	decisions = append(decisions, protocol.ReviewDecision{Kind: protocol.ReviewDecisionAbort})
	return decisions
}
