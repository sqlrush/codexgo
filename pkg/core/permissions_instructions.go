package core

import (
	_ "embed"
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports codex's permissions-instructions fragment
// (core/src/context/permissions_instructions.rs): the developer-role
// `<permissions instructions>` message that tells the model the active sandbox
// mode, network access, and approval policy. The prompt texts are embedded
// byte-for-byte from codex's prompts/permissions/*.md.
//
// Scope: the base sandbox-mode × approval-policy matrix used by `codex exec`
// (the request_permissions_tool wrapper, auto_review suffix, writable-roots and
// denied-reads sections are conditional refinements tracked in docs/PARITY.md and
// added with the full permission-profile wiring).

//go:embed prompts/permissions/sandbox_mode/read_only.md
var sandboxReadOnlyTemplate string

//go:embed prompts/permissions/sandbox_mode/workspace_write.md
var sandboxWorkspaceWriteTemplate string

//go:embed prompts/permissions/sandbox_mode/danger_full_access.md
var sandboxDangerFullAccessTemplate string

//go:embed prompts/permissions/approval_policy/never.md
var approvalNeverText string

//go:embed prompts/permissions/approval_policy/unless_trusted.md
var approvalUnlessTrustedText string

//go:embed prompts/permissions/approval_policy/on_failure.md
var approvalOnFailureText string

//go:embed prompts/permissions/approval_policy/on_request.md
var approvalOnRequestText string

const (
	permissionsStartMarker = "<permissions instructions>"
	permissionsEndMarker   = "</permissions instructions>"
	networkAccessKey       = "{{network_access}}"
)

// PermissionsSandboxMode is the effective filesystem sandbox mode for the prompt,
// mirroring the Rust SandboxMode the prompt selects on (read-only /
// workspace-write / danger-full-access).
type PermissionsSandboxMode int

const (
	// SandboxModeReadOnly permits only reads.
	SandboxModeReadOnly PermissionsSandboxMode = iota
	// SandboxModeWorkspaceWrite permits writes to cwd/writable roots.
	SandboxModeWorkspaceWrite
	// SandboxModeDangerFullAccess disables filesystem sandboxing.
	SandboxModeDangerFullAccess
)

// networkAccessWord renders the {{network_access}} value, matching codex's
// kebab-case NetworkAccess Display ("restricted" / "enabled").
func networkAccessWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "restricted"
}

// sandboxPromptText renders the sandbox-mode section, replacing {{network_access}}
// in the embedded template. Mirrors Rust `sandbox_text`.
func sandboxPromptText(mode PermissionsSandboxMode, networkEnabled bool) string {
	var tmpl string
	switch mode {
	case SandboxModeDangerFullAccess:
		tmpl = sandboxDangerFullAccessTemplate
	case SandboxModeWorkspaceWrite:
		tmpl = sandboxWorkspaceWriteTemplate
	default:
		tmpl = sandboxReadOnlyTemplate
	}
	return strings.ReplaceAll(tmpl, networkAccessKey, networkAccessWord(networkEnabled))
}

// approvalPromptText returns the approval-policy section for the given policy
// kind, mirroring the base branches of Rust `approval_text` (without the
// conditional request_permissions_tool wrapper / auto_review suffix).
func approvalPromptText(policy protocol.AskForApprovalKind) string {
	switch policy {
	case protocol.AskForApprovalUnlessTrusted:
		return approvalUnlessTrustedText
	case protocol.AskForApprovalOnFailure:
		return approvalOnFailureText
	case protocol.AskForApprovalOnRequest:
		return approvalOnRequestText
	default: // AskForApprovalNever
		return approvalNeverText
	}
}

// appendPermissionsSection mirrors Rust `append_section` exactly: if the
// accumulator does not end with a newline (an empty string included), push one,
// then append the section. So the first section is prefixed with '\n' and
// subsequent sections are single-newline separated.
func appendPermissionsSection(text, section string) string {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + section
}

// RenderPermissionsInstructions builds the full `<permissions instructions>`
// developer message body for the active sandbox/network/approval settings,
// mirroring codex's PermissionsInstructions::from_permissions + fragment render
// (start_marker + body + end_marker, newlines carried in the body).
func RenderPermissionsInstructions(mode PermissionsSandboxMode, networkEnabled bool, policy protocol.AskForApprovalKind) string {
	text := appendPermissionsSection("", sandboxPromptText(mode, networkEnabled))
	text = appendPermissionsSection(text, approvalPromptText(policy))
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return permissionsStartMarker + text + permissionsEndMarker
}
