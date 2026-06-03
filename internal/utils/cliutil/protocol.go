package cliutil

// AskForApproval mirrors the unit variants of
// codex_protocol::protocol::AskForApproval that the CLI approval flag can
// select.
//
// Only the variants reachable from [ApprovalModeCliArg] are modelled. The
// string values match the serde representation used upstream (kebab-case, with
// the explicit "untrusted" rename for UnlessTrusted) so the values remain
// wire-compatible with any persisted or serialized configuration.
type AskForApproval string

const (
	// AskForApprovalUnlessTrusted auto-approves only "known safe" read-only
	// commands and escalates everything else to the user. Serialized as
	// "untrusted".
	AskForApprovalUnlessTrusted AskForApproval = "untrusted"

	// AskForApprovalOnFailure (DEPRECATED) auto-approves all commands inside a
	// sandbox and escalates failures to the user. Serialized as "on-failure".
	AskForApprovalOnFailure AskForApproval = "on-failure"

	// AskForApprovalOnRequest lets the model decide when to ask for approval.
	// Serialized as "on-request".
	AskForApprovalOnRequest AskForApproval = "on-request"

	// AskForApprovalNever never asks the user to approve commands. Serialized
	// as "never".
	AskForApprovalNever AskForApproval = "never"
)

// SandboxMode mirrors codex_protocol::config_types::SandboxMode.
//
// The string values match the serde kebab-case representation used upstream.
type SandboxMode string

const (
	// SandboxModeReadOnly restricts execution to read-only filesystem access.
	// Serialized as "read-only".
	SandboxModeReadOnly SandboxMode = "read-only"

	// SandboxModeWorkspaceWrite permits writes within the workspace. Serialized
	// as "workspace-write".
	SandboxModeWorkspaceWrite SandboxMode = "workspace-write"

	// SandboxModeDangerFullAccess disables sandboxing entirely. Serialized as
	// "danger-full-access".
	SandboxModeDangerFullAccess SandboxMode = "danger-full-access"
)
