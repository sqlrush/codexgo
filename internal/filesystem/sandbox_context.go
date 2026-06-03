package filesystem

import (
	"encoding/json"
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// FileSystemSandboxContext carries the runtime permission profile and ambient
// context needed to decide whether and how an operation must be sandboxed.
//
// Rust: `FileSystemSandboxContext` with `#[serde(rename_all = "camelCase")]`:
//   - permissions: always serialized.
//   - cwd: Option<AbsolutePathBuf>, skipped when None.
//   - windowsSandboxLevel: always serialized.
//   - windowsSandboxPrivateDesktop: bool, #[serde(default)].
//   - useLegacyLandlock: bool, #[serde(default)].
//
// Cwd is modeled as a pointer so a nil value is omitted on the wire, matching
// `skip_serializing_if = "Option::is_none"`.
type FileSystemSandboxContext struct {
	// Permissions is the active runtime permission profile.
	Permissions protocol.PermissionProfile
	// Cwd is the optional ambient working directory.
	Cwd *protocol.AbsolutePath
	// WindowsSandboxLevel selects Windows sandbox enforcement.
	WindowsSandboxLevel protocol.WindowsSandboxLevel
	// WindowsSandboxPrivateDesktop requests a private desktop on Windows.
	WindowsSandboxPrivateDesktop bool
	// UseLegacyLandlock selects the legacy landlock backend on Linux.
	UseLegacyLandlock bool
}

// fileSystemSandboxContextJSON is the camelCase wire shape.
type fileSystemSandboxContextJSON struct {
	Permissions                  protocol.PermissionProfile   `json:"permissions"`
	Cwd                          *protocol.AbsolutePath       `json:"cwd,omitempty"`
	WindowsSandboxLevel          protocol.WindowsSandboxLevel `json:"windowsSandboxLevel"`
	WindowsSandboxPrivateDesktop bool                         `json:"windowsSandboxPrivateDesktop"`
	UseLegacyLandlock            bool                         `json:"useLegacyLandlock"`
}

// MarshalJSON encodes the camelCase wire shape, omitting cwd when nil.
func (c FileSystemSandboxContext) MarshalJSON() ([]byte, error) {
	return json.Marshal(fileSystemSandboxContextJSON{
		Permissions:                  c.Permissions,
		Cwd:                          c.Cwd,
		WindowsSandboxLevel:          c.WindowsSandboxLevel,
		WindowsSandboxPrivateDesktop: c.WindowsSandboxPrivateDesktop,
		UseLegacyLandlock:            c.UseLegacyLandlock,
	})
}

// UnmarshalJSON decodes the camelCase wire shape. Missing optional booleans
// default to false (serde `#[serde(default)]`).
func (c *FileSystemSandboxContext) UnmarshalJSON(data []byte) error {
	var raw fileSystemSandboxContextJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = FileSystemSandboxContext{
		Permissions:                  raw.Permissions,
		Cwd:                          raw.Cwd,
		WindowsSandboxLevel:          raw.WindowsSandboxLevel,
		WindowsSandboxPrivateDesktop: raw.WindowsSandboxPrivateDesktop,
		UseLegacyLandlock:            raw.UseLegacyLandlock,
	}
	return nil
}

// FromPermissionProfile builds a context with no cwd.
//
// Rust: `FileSystemSandboxContext::from_permission_profile`.
func FromPermissionProfile(permissions protocol.PermissionProfile) FileSystemSandboxContext {
	return fromPermissionsAndCwd(permissions, nil)
}

// FromPermissionProfileWithCwd builds a context carrying the given cwd.
//
// Rust: `FileSystemSandboxContext::from_permission_profile_with_cwd`.
func FromPermissionProfileWithCwd(permissions protocol.PermissionProfile, cwd protocol.AbsolutePath) FileSystemSandboxContext {
	return fromPermissionsAndCwd(permissions, &cwd)
}

// fromPermissionsAndCwd mirrors the private Rust `from_permissions_and_cwd`
// constructor, defaulting the Windows fields to their disabled/false values.
func fromPermissionsAndCwd(permissions protocol.PermissionProfile, cwd *protocol.AbsolutePath) FileSystemSandboxContext {
	return FileSystemSandboxContext{
		Permissions:                  permissions,
		Cwd:                          cwd,
		WindowsSandboxLevel:          protocol.WindowsSandboxLevelDisabled,
		WindowsSandboxPrivateDesktop: false,
		UseLegacyLandlock:            false,
	}
}

// fileSystemSandboxPolicy returns the effective filesystem sandbox policy for
// the context's permission profile.
//
// Rust: `PermissionProfile::file_system_sandbox_policy`. This is a thin dispatch
// over the protocol permission profile; it is reproduced here because the Go
// protocol package does not yet expose the method.
func (c FileSystemSandboxContext) fileSystemSandboxPolicy() protocol.FileSystemSandboxPolicy {
	switch c.Permissions.Kind {
	case protocol.PermissionProfileManaged:
		return c.Permissions.FileSystem.ToSandboxPolicy()
	case protocol.PermissionProfileExternal:
		return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindExternalSandbox}
	default: // Disabled
		return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindUnrestricted}
	}
}

// ShouldRunInSandbox reports whether an operation must be executed inside a
// platform sandbox: the policy is restricted and does not already grant
// full-disk write access.
//
// Rust: `FileSystemSandboxContext::should_run_in_sandbox`.
func (c FileSystemSandboxContext) ShouldRunInSandbox() bool {
	policy := c.fileSystemSandboxPolicy()
	return policy.Kind == protocol.FileSystemSandboxKindRestricted &&
		!hasFullDiskWriteAccess(policy)
}

// HasCwdDependentPermissions reports whether the policy contains any entry whose
// meaning depends on the ambient cwd (a relative glob pattern or a ProjectRoots
// special path).
//
// Rust: `FileSystemSandboxContext::has_cwd_dependent_permissions`.
func (c FileSystemSandboxContext) HasCwdDependentPermissions() bool {
	return fileSystemPolicyHasCwdDependentEntries(c.fileSystemSandboxPolicy())
}

// DropCwdIfUnused returns a copy of the context with cwd cleared when the policy
// has no cwd-dependent entries. Inputs are not mutated.
//
// Rust: `FileSystemSandboxContext::drop_cwd_if_unused` (which takes self by
// value); the Go port returns a new value to preserve immutability.
func (c FileSystemSandboxContext) DropCwdIfUnused() FileSystemSandboxContext {
	if !c.HasCwdDependentPermissions() {
		c.Cwd = nil
	}
	return c
}

// fileSystemPolicyHasCwdDependentEntries mirrors the Rust free function of the
// same name.
func fileSystemPolicyHasCwdDependentEntries(policy protocol.FileSystemSandboxPolicy) bool {
	for _, entry := range policy.Entries {
		switch entry.Path.Type {
		case protocol.FileSystemPathTypeGlobPattern:
			if !filepath.IsAbs(entry.Path.Pattern) {
				return true
			}
		case protocol.FileSystemPathTypeSpecial:
			if entry.Path.Special != nil &&
				entry.Path.Special.Kind == protocol.FileSystemSpecialPathKindProjectRoots {
				return true
			}
		}
	}
	return false
}
