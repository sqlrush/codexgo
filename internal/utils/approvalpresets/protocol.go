// Package approvalpresets provides built-in approval presets that pair an
// approval policy with a permission profile.
//
// It is a faithful Go port of the codex-utils-approval-presets Rust crate.
// The presets are intentionally UI-agnostic so they can be reused by both the
// TUI and the MCP server. The externally observable identifiers, labels,
// descriptions, and JSON serialization of the contained protocol types are
// preserved exactly so that callers (and any persisted/serialized data) behave
// identically across the two implementations.
//
// The upstream crate depends on several types from the codex_protocol crate.
// Until that crate is ported to Go, the minimal subset of those types required
// by the presets is defined locally in this package. The JSON encodings mirror
// the serde representations used upstream so the values remain wire-compatible.
package approvalpresets

import (
	"encoding/json"
	"fmt"
)

// Reserved identifiers for the built-in permission profiles.
//
// These mirror the codex_protocol constants of the same name.
const (
	// BuiltInPermissionProfileReadOnly is the identifier for the built-in
	// read-only permission profile.
	BuiltInPermissionProfileReadOnly = ":read-only"

	// BuiltInPermissionProfileWorkspace is the identifier for the built-in
	// workspace-write permission profile.
	BuiltInPermissionProfileWorkspace = ":workspace"

	// BuiltInPermissionProfileDangerFullAccess is the identifier for the
	// built-in full-access permission profile.
	BuiltInPermissionProfileDangerFullAccess = ":danger-full-access"
)

// AskForApproval mirrors codex_protocol::protocol::AskForApproval.
//
// Only the unit variants exercised by the approval presets are modelled here.
// The string values match the serde kebab-case representation (with the
// explicit "untrusted" rename for UnlessTrusted) so JSON encoding is
// wire-compatible.
type AskForApproval string

const (
	// AskForApprovalUnlessTrusted auto-approves only "known safe" read-only
	// commands; everything else asks the user. Serialized as "untrusted".
	AskForApprovalUnlessTrusted AskForApproval = "untrusted"

	// AskForApprovalOnFailure auto-approves all commands inside a sandbox and
	// escalates failures to the user. Serialized as "on-failure".
	AskForApprovalOnFailure AskForApproval = "on-failure"

	// AskForApprovalOnRequest lets the model decide when to ask for approval.
	// Serialized as "on-request".
	AskForApprovalOnRequest AskForApproval = "on-request"

	// AskForApprovalNever never asks the user to approve commands. Serialized
	// as "never".
	AskForApprovalNever AskForApproval = "never"
)

// ActivePermissionProfile mirrors codex_protocol::models::ActivePermissionProfile.
//
// It records the named or implicit built-in permissions profile that produced
// the active PermissionProfile so clients can display a stable profile identity
// without reverse-engineering it from the compiled permissions.
type ActivePermissionProfile struct {
	// ID is the profile identifier, such as ":workspace" or a user-defined id.
	ID string `json:"id"`

	// Extends is the optional parent profile identifier from the selected
	// profile's "extends" setting. It is omitted from JSON when nil.
	Extends *string `json:"extends,omitempty"`
}

// NewActivePermissionProfile constructs an ActivePermissionProfile with the
// given id and no parent profile, mirroring ActivePermissionProfile::new.
func NewActivePermissionProfile(id string) ActivePermissionProfile {
	return ActivePermissionProfile{ID: id}
}

// ReadOnlyActivePermissionProfile returns the built-in read-only active
// profile, mirroring ActivePermissionProfile::read_only.
func ReadOnlyActivePermissionProfile() ActivePermissionProfile {
	return NewActivePermissionProfile(BuiltInPermissionProfileReadOnly)
}

// NetworkSandboxPolicy mirrors codex_protocol::permissions::NetworkSandboxPolicy.
//
// The string values match the serde kebab-case representation.
type NetworkSandboxPolicy string

const (
	// NetworkSandboxRestricted denies network access. Serialized as "restricted".
	NetworkSandboxRestricted NetworkSandboxPolicy = "restricted"

	// NetworkSandboxEnabled allows network access. Serialized as "enabled".
	NetworkSandboxEnabled NetworkSandboxPolicy = "enabled"
)

// IsEnabled reports whether the policy allows network access.
func (p NetworkSandboxPolicy) IsEnabled() bool {
	return p == NetworkSandboxEnabled
}

// FileSystemAccessMode mirrors codex_protocol::permissions::FileSystemAccessMode.
//
// The string values match the serde lowercase representation.
type FileSystemAccessMode string

const (
	// FileSystemAccessRead grants read access. Serialized as "read".
	FileSystemAccessRead FileSystemAccessMode = "read"

	// FileSystemAccessWrite grants write access. Serialized as "write".
	FileSystemAccessWrite FileSystemAccessMode = "write"

	// FileSystemAccessDeny denies access. Serialized as "deny".
	FileSystemAccessDeny FileSystemAccessMode = "deny"
)

// CanRead reports whether the mode permits reads.
func (m FileSystemAccessMode) CanRead() bool {
	return m != FileSystemAccessDeny
}

// CanWrite reports whether the mode permits writes.
func (m FileSystemAccessMode) CanWrite() bool {
	return m == FileSystemAccessWrite
}

// FileSystemSpecialPathKind mirrors the serde "kind" tag of
// codex_protocol::permissions::FileSystemSpecialPath.
type FileSystemSpecialPathKind string

const (
	// SpecialPathRoot targets the filesystem root.
	SpecialPathRoot FileSystemSpecialPathKind = "root"

	// SpecialPathMinimal targets the minimal default set of paths.
	SpecialPathMinimal FileSystemSpecialPathKind = "minimal"

	// SpecialPathProjectRoots targets the active project roots.
	SpecialPathProjectRoots FileSystemSpecialPathKind = "project_roots"

	// SpecialPathTmpdir targets the platform temporary directory.
	SpecialPathTmpdir FileSystemSpecialPathKind = "tmpdir"

	// SpecialPathSlashTmp targets "/tmp".
	SpecialPathSlashTmp FileSystemSpecialPathKind = "slash_tmp"

	// SpecialPathUnknown preserves a forward-compatible special-path token.
	SpecialPathUnknown FileSystemSpecialPathKind = "unknown"
)

// FileSystemSpecialPath mirrors codex_protocol::permissions::FileSystemSpecialPath.
//
// It is an internally tagged union keyed on Kind. Subpath applies to the
// ProjectRoots and Unknown variants and is omitted from JSON when nil. Path
// applies to the Unknown variant only.
type FileSystemSpecialPath struct {
	// Kind selects the special-path variant.
	Kind FileSystemSpecialPathKind `json:"kind"`

	// Subpath is an optional subpath, used by ProjectRoots and Unknown.
	Subpath *string `json:"subpath,omitempty"`

	// Path carries the raw token for the Unknown variant.
	Path *string `json:"path,omitempty"`
}

// NewSpecialPathRoot returns the Root special path.
func NewSpecialPathRoot() FileSystemSpecialPath {
	return FileSystemSpecialPath{Kind: SpecialPathRoot}
}

// NewSpecialPathProjectRoots returns a ProjectRoots special path with an
// optional subpath. Pass nil for no subpath.
func NewSpecialPathProjectRoots(subpath *string) FileSystemSpecialPath {
	return FileSystemSpecialPath{Kind: SpecialPathProjectRoots, Subpath: cloneStringPtr(subpath)}
}

// NewSpecialPathTmpdir returns the Tmpdir special path.
func NewSpecialPathTmpdir() FileSystemSpecialPath {
	return FileSystemSpecialPath{Kind: SpecialPathTmpdir}
}

// NewSpecialPathSlashTmp returns the SlashTmp special path.
func NewSpecialPathSlashTmp() FileSystemSpecialPath {
	return FileSystemSpecialPath{Kind: SpecialPathSlashTmp}
}

// FileSystemPathKind mirrors the serde "type" tag of
// codex_protocol::permissions::FileSystemPath.
type FileSystemPathKind string

const (
	// FileSystemPathTypePath targets a concrete absolute path.
	FileSystemPathTypePath FileSystemPathKind = "path"

	// FileSystemPathTypeGlobPattern targets a git-style glob pattern.
	FileSystemPathTypeGlobPattern FileSystemPathKind = "glob_pattern"

	// FileSystemPathTypeSpecial targets a special path.
	FileSystemPathTypeSpecial FileSystemPathKind = "special"
)

// FileSystemPath mirrors codex_protocol::permissions::FileSystemPath.
//
// It is an internally tagged union keyed on Type. Path applies to the Path
// variant, Pattern to the GlobPattern variant, and Value to the Special
// variant. Unused fields are omitted from JSON.
type FileSystemPath struct {
	// Type selects the path variant.
	Type FileSystemPathKind `json:"type"`

	// Path is the concrete absolute path for the Path variant.
	Path *string `json:"path,omitempty"`

	// Pattern is the glob pattern for the GlobPattern variant.
	Pattern *string `json:"pattern,omitempty"`

	// Value is the special path for the Special variant.
	Value *FileSystemSpecialPath `json:"value,omitempty"`
}

// NewSpecialFileSystemPath wraps a special path as a FileSystemPath.
func NewSpecialFileSystemPath(value FileSystemSpecialPath) FileSystemPath {
	v := value
	return FileSystemPath{Type: FileSystemPathTypeSpecial, Value: &v}
}

// NewConcreteFileSystemPath wraps a concrete absolute path as a FileSystemPath.
func NewConcreteFileSystemPath(path string) FileSystemPath {
	p := path
	return FileSystemPath{Type: FileSystemPathTypePath, Path: &p}
}

// FileSystemSandboxEntry mirrors codex_protocol::permissions::FileSystemSandboxEntry.
type FileSystemSandboxEntry struct {
	// Path is the targeted path.
	Path FileSystemPath `json:"path"`

	// Access is the access mode granted to the path.
	Access FileSystemAccessMode `json:"access"`
}

// ManagedFileSystemPermissionsKind mirrors the serde "type" tag of
// codex_protocol::models::ManagedFileSystemPermissions.
type ManagedFileSystemPermissionsKind string

const (
	// ManagedFileSystemRestricted applies a managed sandbox from the entries.
	ManagedFileSystemRestricted ManagedFileSystemPermissionsKind = "restricted"

	// ManagedFileSystemUnrestricted applies a managed sandbox allowing all
	// filesystem access.
	ManagedFileSystemUnrestricted ManagedFileSystemPermissionsKind = "unrestricted"
)

// ManagedFileSystemPermissions mirrors
// codex_protocol::models::ManagedFileSystemPermissions.
//
// It is an internally tagged union keyed on Type. Entries and
// GlobScanMaxDepth apply to the Restricted variant only. GlobScanMaxDepth is
// omitted from JSON when nil and must be non-zero when present (mirroring the
// upstream NonZeroUsize).
type ManagedFileSystemPermissions struct {
	// Type selects the managed-permissions variant.
	Type ManagedFileSystemPermissionsKind `json:"type"`

	// Entries are the filesystem sandbox entries for the Restricted variant.
	// It is omitted from JSON when empty.
	Entries []FileSystemSandboxEntry `json:"entries,omitempty"`

	// GlobScanMaxDepth optionally bounds glob scan depth for the Restricted
	// variant. When set it must be greater than zero.
	GlobScanMaxDepth *uint `json:"glob_scan_max_depth,omitempty"`
}

// PermissionProfileKind mirrors the serde "type" tag of
// codex_protocol::models::PermissionProfile.
type PermissionProfileKind string

const (
	// PermissionProfileManaged is the variant where Codex owns sandbox
	// construction.
	PermissionProfileManaged PermissionProfileKind = "managed"

	// PermissionProfileDisabled is the variant that applies no outer sandbox.
	PermissionProfileDisabled PermissionProfileKind = "disabled"

	// PermissionProfileExternal is the variant where filesystem isolation is
	// enforced by an external caller.
	PermissionProfileExternal PermissionProfileKind = "external"
)

// PermissionProfile mirrors codex_protocol::models::PermissionProfile.
//
// It is an internally tagged union keyed on Type:
//
//   - Managed: carries FileSystem and Network.
//   - Disabled: carries nothing.
//   - External: carries Network only.
//
// The JSON shape matches serde's adjacently flattened representation:
// {"type":"managed","file_system":{...},"network":"..."},
// {"type":"disabled"}, and {"type":"external","network":"..."}.
type PermissionProfile struct {
	// Type selects the permission-profile variant.
	Type PermissionProfileKind `json:"type"`

	// FileSystem applies to the Managed variant.
	FileSystem *ManagedFileSystemPermissions `json:"file_system,omitempty"`

	// Network applies to the Managed and External variants.
	Network *NetworkSandboxPolicy `json:"network,omitempty"`
}

// PermissionProfileReadOnly returns the managed read-only filesystem profile
// with restricted network access, mirroring PermissionProfile::read_only.
func PermissionProfileReadOnly() PermissionProfile {
	network := NetworkSandboxRestricted
	return PermissionProfile{
		Type: PermissionProfileManaged,
		FileSystem: &ManagedFileSystemPermissions{
			Type: ManagedFileSystemRestricted,
			Entries: []FileSystemSandboxEntry{
				{
					Path:   NewSpecialFileSystemPath(NewSpecialPathRoot()),
					Access: FileSystemAccessRead,
				},
			},
		},
		Network: &network,
	}
}

// PermissionProfileWorkspaceWrite returns the managed workspace-write
// filesystem profile with restricted network access, mirroring
// PermissionProfile::workspace_write (empty writable roots, no exclusions).
//
// The entries use symbolic project-root references that must be resolved
// against the active permission root before enforcement.
func PermissionProfileWorkspaceWrite() PermissionProfile {
	gitSub := ".git"
	agentsSub := ".agents"
	codexSub := ".codex"

	network := NetworkSandboxRestricted
	return PermissionProfile{
		Type: PermissionProfileManaged,
		FileSystem: &ManagedFileSystemPermissions{
			Type: ManagedFileSystemRestricted,
			Entries: []FileSystemSandboxEntry{
				{
					Path:   NewSpecialFileSystemPath(NewSpecialPathRoot()),
					Access: FileSystemAccessRead,
				},
				{
					Path:   NewSpecialFileSystemPath(NewSpecialPathProjectRoots(nil)),
					Access: FileSystemAccessWrite,
				},
				{
					Path:   NewSpecialFileSystemPath(NewSpecialPathSlashTmp()),
					Access: FileSystemAccessWrite,
				},
				{
					Path:   NewSpecialFileSystemPath(NewSpecialPathTmpdir()),
					Access: FileSystemAccessWrite,
				},
				{
					Path:   NewSpecialFileSystemPath(NewSpecialPathProjectRoots(&gitSub)),
					Access: FileSystemAccessRead,
				},
				{
					Path:   NewSpecialFileSystemPath(NewSpecialPathProjectRoots(&agentsSub)),
					Access: FileSystemAccessRead,
				},
				{
					Path:   NewSpecialFileSystemPath(NewSpecialPathProjectRoots(&codexSub)),
					Access: FileSystemAccessRead,
				},
			},
		},
		Network: &network,
	}
}

// PermissionProfileDisabledProfile returns the disabled permission profile,
// mirroring PermissionProfile::Disabled.
func PermissionProfileDisabledProfile() PermissionProfile {
	return PermissionProfile{Type: PermissionProfileDisabled}
}

// cloneStringPtr returns a deep copy of a *string so callers cannot mutate the
// pointee held by returned values.
func cloneStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// MarshalJSON guards against serializing an invalid (zero) glob scan depth.
//
// Upstream models this field as NonZeroUsize, so a present-but-zero value is
// not representable. We fail fast rather than emit an out-of-contract value.
func (m ManagedFileSystemPermissions) MarshalJSON() ([]byte, error) {
	if m.GlobScanMaxDepth != nil && *m.GlobScanMaxDepth == 0 {
		return nil, fmt.Errorf("approvalpresets: glob_scan_max_depth must be non-zero when set")
	}
	type alias ManagedFileSystemPermissions
	return json.Marshal(alias(m))
}
