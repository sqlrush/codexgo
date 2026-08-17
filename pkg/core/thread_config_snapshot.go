package core

import (
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ThreadConfigSnapshot is the immutable view of a thread's effective
// configuration at a point in time. It is a reduced, faithful port of the Rust
// `codex_thread::ThreadConfigSnapshot`: the model/provider identity, approval
// policy, permission profile, cwd/workspace roots, and the reasoning/personality
// settings that callers (app-server, fork/resume) read before starting a turn.
//
// Treat all fields as read-only after construction; the snapshot is taken under
// the session state lock and never mutated afterward (immutability contract).
//
// STUB: the Rust snapshot also exposes the derived sandbox_policy() projection,
// active_permission_profile, profile_workspace_roots, and the rich permission
// profile object. Those derivations are owned by the permissions/sandbox area
// agents; PermissionProfile is carried here as an opaque value.
type ThreadConfigSnapshot struct {
	// Model is the effective model slug.
	Model string
	// ModelProviderID identifies the model provider.
	ModelProviderID string
	// ServiceTier is the optional service-tier request value.
	ServiceTier *string
	// ApprovalPolicy controls when execution escalates for approval.
	ApprovalPolicy protocol.AskForApproval
	// ApprovalsReviewer selects the reviewer for approvals.
	ApprovalsReviewer protocol.ApprovalsReviewer
	// PermissionProfile is the effective permission profile (opaque to core).
	PermissionProfile any
	// Cwd is the absolute working-directory root for the thread.
	Cwd string
	// WorkspaceRoots are the thread-scoped workspace roots.
	WorkspaceRoots []string
	// Ephemeral reports whether the thread is ephemeral (no metadata updates).
	Ephemeral bool
	// ReasoningEffort is the effective reasoning effort, if set.
	ReasoningEffort *protocol.ReasoningEffort
	// ReasoningSummary configures reasoning-summary verbosity, if set.
	ReasoningSummary *protocol.ReasoningSummary
	// Personality is the model personality preference, if set.
	Personality *protocol.Personality
	// CollaborationMode is the effective collaboration mode.
	CollaborationMode protocol.CollaborationMode
	// SessionSource classifies the origin of the thread.
	SessionSource any
	// ThreadSource is the optional analytics source classification.
	ThreadSource any
}

// threadConfigSnapshot builds a [ThreadConfigSnapshot] from the current session
// configuration, taken under the session state lock. It mirrors the Rust
// `Codex::thread_config_snapshot`.
func (c *Codex) threadConfigSnapshot() ThreadConfigSnapshot {
	cfg := c.session.CloneConfiguration()
	return ThreadConfigSnapshot{
		Model:             cfg.Model(),
		ModelProviderID:   cfg.ProviderID,
		ServiceTier:       clonePtr(cfg.ServiceTier),
		ApprovalPolicy:    cfg.ApprovalPolicy,
		ApprovalsReviewer: cfg.ApprovalsReviewer,
		Cwd:               cfg.Cwd,
		WorkspaceRoots:    append([]string(nil), cfg.WorkspaceRoots...),
		ReasoningEffort:   clonePtr(cfg.CollaborationMode.Settings.ReasoningEffort),
		ReasoningSummary:  clonePtr(cfg.ModelReasoningSummary),
		Personality:       clonePtr(cfg.Personality),
		CollaborationMode: cfg.CollaborationMode,
		SessionSource:     cfg.SessionSource,
		ThreadSource:      cfg.ThreadSource,
	}
}
