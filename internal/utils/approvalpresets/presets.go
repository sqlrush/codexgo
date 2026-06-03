package approvalpresets

// ApprovalPreset is a simple preset pairing an approval policy with a
// permission profile.
//
// It mirrors codex_utils_approval_presets::ApprovalPreset.
type ApprovalPreset struct {
	// ID is the stable identifier for the preset.
	ID string

	// Label is the display label shown in UIs.
	Label string

	// Description is the short human description shown next to the label.
	Description string

	// Approval is the approval policy to apply.
	Approval AskForApproval

	// ActivePermissionProfile is the built-in permission profile selected by
	// this preset.
	ActivePermissionProfile ActivePermissionProfile

	// PermissionProfile is the concrete permission profile to apply.
	PermissionProfile PermissionProfile
}

// BuiltinApprovalPresets returns the built-in list of approval presets that
// pair approval and permissions.
//
// It mirrors codex_utils_approval_presets::builtin_approval_presets. The list
// is kept UI-agnostic so it can be reused by both the TUI and the MCP server.
//
// A fresh slice (with freshly constructed elements) is returned on every call,
// so callers may mutate the result without affecting subsequent calls.
func BuiltinApprovalPresets() []ApprovalPreset {
	return []ApprovalPreset{
		{
			ID:                      "read-only",
			Label:                   "Read Only",
			Description:             "Codex can read files in the current workspace. Approval is required to edit files or access the internet.",
			Approval:                AskForApprovalOnRequest,
			ActivePermissionProfile: NewActivePermissionProfile(BuiltInPermissionProfileReadOnly),
			PermissionProfile:       PermissionProfileReadOnly(),
		},
		{
			ID:                      "auto",
			Label:                   "Default",
			Description:             "Codex can read and edit files in the current workspace, and run commands. Approval is required to access the internet or edit other files. (Identical to Agent mode)",
			Approval:                AskForApprovalOnRequest,
			ActivePermissionProfile: NewActivePermissionProfile(BuiltInPermissionProfileWorkspace),
			PermissionProfile:       PermissionProfileWorkspaceWrite(),
		},
		{
			ID:                      "full-access",
			Label:                   "Full Access",
			Description:             "Codex can edit files outside this workspace and access the internet without asking for approval. Exercise caution when using.",
			Approval:                AskForApprovalNever,
			ActivePermissionProfile: NewActivePermissionProfile(BuiltInPermissionProfileDangerFullAccess),
			PermissionProfile:       PermissionProfileDisabledProfile(),
		},
	}
}

// BuiltinPermissionProfileForActivePermissionProfile returns the concrete
// permission profile for one of the built-in active profile ids.
//
// It mirrors
// codex_utils_approval_presets::builtin_permission_profile_for_active_permission_profile.
//
// The second return value reports whether a built-in profile was found. It is
// false (and the first return value is the zero PermissionProfile) when the
// active profile extends another profile or when its id is not a recognized
// built-in identifier.
func BuiltinPermissionProfileForActivePermissionProfile(active ActivePermissionProfile) (PermissionProfile, bool) {
	if active.Extends != nil {
		return PermissionProfile{}, false
	}

	switch active.ID {
	case BuiltInPermissionProfileReadOnly:
		return PermissionProfileReadOnly(), true
	case BuiltInPermissionProfileWorkspace:
		return PermissionProfileWorkspaceWrite(), true
	case BuiltInPermissionProfileDangerFullAccess:
		return PermissionProfileDisabledProfile(), true
	default:
		return PermissionProfile{}, false
	}
}
