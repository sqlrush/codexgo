package localexec

import (
	"context"
	"strings"

	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the sandbox-denial approval escalation from codex-core's
// tools::orchestrator (ToolOrchestrator::run) + tools::sandboxing. When a
// sandboxed command fails with a SandboxErr::Denied and the approval policy
// permits, codex emits a request_command_approval (core.ExecApprovalRequest) and, on
// approval, retries the command WITHOUT the sandbox (escalated). The shell and
// unified-exec exec paths drive this loop after their first (sandboxed)
// attempt.
//
// The reduced port keeps the load-bearing escalation block of
// ToolOrchestrator::run: on the first attempt's SandboxErr::Denied, consult
// wants_no_sandbox_approval(policy) (gate the prompt) and
// unsandboxed_execution_allowed(fs) (denied-read restrictions only exist inside
// the sandbox), then prompt and retry unsandboxed on approval. Both shell and
// unified_exec runtimes return escalate_on_failure()==true, so that branch is
// not threaded here.
//
// STUB: guardian review routing, network-policy denial escalation, the
// ApprovedForSession cache (with_cached_approval), permission-request hooks, and
// per-call sandbox_permissions / additional_permissions are owned by the
// guardian / permissions / network areas and are not threaded here. The
// non-network FS denial path the shell + unified-exec exec calls hit is fully
// ported.

// sandboxDenialReason is the terse, stable approval reason the orchestrator
// builds from a sandbox denial. Mirrors build_denial_reason_from_output, which
// accepts the output but returns this fixed string for UX/test stability.
const sandboxDenialReason = "command failed; retry without sandbox?"

// sandboxEscalationDecision is the orchestrator's choice after a first-attempt
// sandbox denial: whether to retry the command unsandboxed (escalated) and, when
// not, why the denial is surfaced to the model unchanged.
type sandboxEscalationDecision int

const (
	// sandboxEscalationSurfaceDenial reports the denial to the model unchanged
	// (the policy never prompts, denied reads forbid unsandboxed retry, or the
	// user denied/aborted the escalation prompt).
	sandboxEscalationSurfaceDenial sandboxEscalationDecision = iota
	// sandboxEscalationRetryUnsandboxed retries the command with SandboxType none.
	sandboxEscalationRetryUnsandboxed
)

// sandboxEscalationRequest bundles the inputs the escalation orchestrator needs
// after a first-attempt sandbox denial. It is the reduced Go analogue of the
// state ToolOrchestrator::run carries into its retry branch.
type sandboxEscalationRequest struct {
	// Turn is the read-only turn snapshot whose approval policy + sandbox mode
	// drive the decision.
	Turn *core.TurnContext
	// CallID correlates the (retry) approval request with its response.
	CallID string
	// Command is the argv proposed for execution (echoed in the approval event).
	Command []string
	// Cwd is the working directory for the command.
	Cwd string
	// TTY marks a unified-exec (PTY) command; it selects the ExecCommand
	// approval action kind (separate cache key from shell_command).
	TTY bool
	// ToolName is the invoking tool, recorded with the approval decision.
	ToolName protocol.ToolName
}

// resolveSandboxEscalation decides whether a first-attempt sandbox denial should
// escalate to an unsandboxed retry, prompting for approval when the policy wants
// it. It is the Go analogue of the SandboxErr::Denied arm of
// ToolOrchestrator::run for the non-network FS denial path.
//
// The flow mirrors the Rust block:
//  1. unsandboxed_allowed = unsandboxed_execution_allowed(fs).
//  2. If !wants_no_sandbox_approval(policy) -> surface the denial (Never /
//     OnRequest do not retry without sandbox for an FS denial).
//  3. If !unsandboxed_allowed -> surface the denial (denied reads only exist
//     inside the sandbox, so bypassing it would silently grant them).
//  4. Otherwise prompt for approval (unless should_bypass_approval) and, on
//     approval, retry unsandboxed; on deny/abort/timeout, surface the denial.
//
// A nil session (no client to prompt) resolves the prompt as a deny via the
// pre-closed-waiter fallback in core.Session.RequestCommandApproval, mirroring the
// headless request_user_input resolution.
func resolveSandboxEscalation(ctx context.Context, sess *core.Session, req sandboxEscalationRequest) sandboxEscalationDecision {
	policy := turnApprovalPolicy(req.Turn)
	fsState := turnFilesystemSandboxState(req.Turn)

	unsandboxedAllowed := core.UnsandboxedExecutionAllowed(fsState)

	// Under Never / OnRequest, do not retry without the sandbox; surface the
	// denial preserving the original output (wants_no_sandbox_approval gate). The
	// network-prompt special case (OnRequest + managed network) does not apply to
	// the FS denial path threaded here.
	if !wantsNoSandboxApproval(policy) {
		return sandboxEscalationSurfaceDenial
	}
	if !unsandboxedAllowed {
		return sandboxEscalationSurfaceDenial
	}

	// should_bypass_approval(policy, already_approved=false): Never bypasses, but
	// Never is already filtered out above, so a fresh approval prompt is required.
	if shouldBypassNoSandboxApproval(policy) {
		return sandboxEscalationRetryUnsandboxed
	}

	if sess == nil {
		return sandboxEscalationSurfaceDenial
	}

	// The retry approval goes through the centralized approval stage (0.147
	// tools/approvals.rs; spec 50 D0.4): hooks → automated reviewer → user, with
	// the approved-for-session cache and decision recording applied uniformly.
	retryReason := sandboxDenialReason
	kind := core.ApprovalActionShell
	if req.TTY {
		kind = core.ApprovalActionExecCommand
	}
	decision, err := sess.RequestApproval(ctx, core.ApprovalAction{
		Kind:        kind,
		ID:          req.CallID,
		Command:     req.Command,
		HookCommand: strings.Join(req.Command, " "),
		Cwd:         req.Cwd,
		TTY:         req.TTY,
	}, core.ApprovalContext{
		Turn:        req.Turn,
		CallID:      req.CallID,
		ToolName:    req.ToolName,
		RetryReason: &retryReason,
	})
	if err != nil {
		return sandboxEscalationSurfaceDenial
	}
	if reviewDecisionApproves(decision) {
		return sandboxEscalationRetryUnsandboxed
	}
	return sandboxEscalationSurfaceDenial
}

// wantsNoSandboxApproval reports whether the policy permits requesting approval
// for no-sandbox execution. Mirrors the default Approvable::wants_no_sandbox_approval:
// OnFailure / UnlessTrusted yes; Never / OnRequest no; Granular honors its
// sandbox_approval flag.
func wantsNoSandboxApproval(policy protocol.AskForApproval) bool {
	switch policy.Kind {
	case protocol.AskForApprovalOnFailure, protocol.AskForApprovalUnlessTrusted:
		return true
	case protocol.AskForApprovalNever, protocol.AskForApprovalOnRequest:
		return false
	case protocol.AskForApprovalGranular:
		return core.GranularAllowsSandboxApproval(policy.Granular)
	default:
		return false
	}
}

// shouldBypassNoSandboxApproval reports whether the retry can skip the approval
// prompt. Mirrors the default Approvable::should_bypass_approval with
// already_approved=false: only Never bypasses (and Never never reaches here).
func shouldBypassNoSandboxApproval(policy protocol.AskForApproval) bool {
	return policy.Kind == protocol.AskForApprovalNever
}

// reviewDecisionApproves reports whether a review decision authorizes the
// escalated (unsandboxed) retry. Mirrors reject_if_not_approved: Approved /
// ApprovedForSession / ApprovedExecpolicyAmendment proceed; Denied / Abort /
// TimedOut reject. The network-policy-amendment arm is not reachable on the FS
// denial path.
func reviewDecisionApproves(decision protocol.ReviewDecision) bool {
	switch decision.Kind {
	case protocol.ReviewDecisionApproved,
		protocol.ReviewDecisionApprovedForSession,
		protocol.ReviewDecisionApprovedExecpolicyAmendment:
		return true
	default:
		return false
	}
}

// turnApprovalPolicy resolves the effective approval policy for a turn,
// defaulting to OnRequest (codex's config default) for the zero value.
func turnApprovalPolicy(tc *core.TurnContext) protocol.AskForApproval {
	if tc != nil && tc.ApprovalPolicy.Kind != "" {
		return tc.ApprovalPolicy
	}
	return protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest}
}

// turnFilesystemSandboxState projects the turn's resolved filesystem policy onto
// the reduced core.FilesystemSandboxState the escalation decision consumes (Restricted
// + DeniedReadRestrictions). Mirrors turn_context.file_system_sandbox_policy()
// reduced to the two booleans unsandboxed_execution_allowed needs.
func turnFilesystemSandboxState(tc *core.TurnContext) core.FilesystemSandboxState {
	pol := resolveTurnSandboxPolicy(tc)
	fs := pol.FileSystemSandboxPolicy
	return core.FilesystemSandboxState{
		Restricted:             fs.Kind == protocol.FileSystemSandboxKindRestricted,
		DeniedReadRestrictions: fileSystemPolicyHasDeniedReads(fs),
	}
}

// fileSystemPolicyHasDeniedReads reports whether a Restricted policy carries any
// Deny entry. Mirrors has_denied_read_restrictions (the sandbox package's
// fsHasDeniedReadRestrictions is unexported, so the load-bearing rule is applied
// here over the protocol policy core already resolves).
func fileSystemPolicyHasDeniedReads(p protocol.FileSystemSandboxPolicy) bool {
	if p.Kind != protocol.FileSystemSandboxKindRestricted {
		return false
	}
	for _, entry := range p.Entries {
		if entry.Access == protocol.FileSystemAccessModeDeny {
			return true
		}
	}
	return false
}

// isLikelySandboxDenied reports whether a sandboxed command's failure is likely
// a sandbox denial. It is a faithful port of is_likely_sandbox_denied (exec.rs):
// a none backend or a zero exit never qualifies; otherwise a well-known denial
// keyword in any output stream qualifies, the quick-reject shell exit codes
// (2/126/127) disqualify, and a Linux seccomp SIGSYS exit qualifies.
func isLikelySandboxDenied(sandboxType sandbox.SandboxType, exitCode int, stdout, stderr, aggregated string) bool {
	if sandboxType == sandbox.SandboxTypeNone || exitCode == 0 {
		return false
	}

	for _, section := range []string{stderr, stdout, aggregated} {
		lower := strings.ToLower(section)
		for _, needle := range sandboxDeniedKeywords {
			if strings.Contains(lower, needle) {
				return true
			}
		}
	}

	switch exitCode {
	case 2, 126, 127:
		// Well-known non-sandbox shell exit codes (misuse / permission / not found).
		return false
	}

	// Linux seccomp SIGSYS: exit code EXIT_CODE_SIGNAL_BASE (128) + SIGSYS (31).
	if sandboxType == sandbox.SandboxTypeLinuxSeccomp && exitCode == execSignalBase+sigSysCode {
		return true
	}
	return false
}

// sandboxDeniedKeywords mirrors SANDBOX_DENIED_KEYWORDS (exec.rs).
var sandboxDeniedKeywords = []string{
	"operation not permitted",
	"permission denied",
	"read-only file system",
	"seccomp",
	"sandbox",
	"landlock",
	"failed to write file",
}

// execSignalBase mirrors EXIT_CODE_SIGNAL_BASE (128): the base a process exit
// code uses to encode a terminating signal.
const execSignalBase = 128

// sigSysCode mirrors libc::SIGSYS (31 on Linux): the bad-system-call signal the
// seccomp sandbox raises on a denied syscall.
const sigSysCode = 31
