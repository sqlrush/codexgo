package protocol

import (
	"encoding/json"
	"fmt"
)

// Reserved identifiers for the built-in permission profiles.
//
// Rust: associated `pub const` string constants on the protocol module.
const (
	// BuiltInPermissionProfileReadOnly is the built-in read-only profile id.
	BuiltInPermissionProfileReadOnly = ":read-only"
	// BuiltInPermissionProfileWorkspace is the built-in workspace-write profile id.
	BuiltInPermissionProfileWorkspace = ":workspace"
	// BuiltInPermissionProfileDangerFullAccess is the built-in full-access profile id.
	BuiltInPermissionProfileDangerFullAccess = ":danger-full-access"
)

// SandboxEnforcement selects who owns sandbox construction for a profile.
//
// Rust: `#[serde(rename_all = "snake_case")]`, default Managed. The variants are
// unit variants, so they serialize as bare snake_case strings.
type SandboxEnforcement string

const (
	// SandboxEnforcementManaged means Codex owns sandbox construction (default).
	SandboxEnforcementManaged SandboxEnforcement = "managed"
	// SandboxEnforcementDisabled means no outer filesystem sandbox is applied.
	SandboxEnforcementDisabled SandboxEnforcement = "disabled"
	// SandboxEnforcementExternal means filesystem isolation is enforced by an
	// external caller.
	SandboxEnforcementExternal SandboxEnforcement = "external"
)

// ManagedFileSystemPermissionsKind is the discriminator for
// ManagedFileSystemPermissions.
type ManagedFileSystemPermissionsKind string

const (
	// ManagedFileSystemPermissionsRestricted applies a managed filesystem
	// sandbox from the listed entries.
	ManagedFileSystemPermissionsRestricted ManagedFileSystemPermissionsKind = "restricted"
	// ManagedFileSystemPermissionsUnrestricted applies a managed sandbox that
	// allows all filesystem access.
	ManagedFileSystemPermissionsUnrestricted ManagedFileSystemPermissionsKind = "unrestricted"
)

// ManagedFileSystemPermissions describes filesystem permissions for profiles
// where Codex owns sandbox construction.
//
// Rust models this as an internally-tagged enum (`#[serde(tag = "type",
// rename_all = "snake_case")]`):
//   - Restricted { entries, glob_scan_max_depth? }: `entries` is always
//     serialized (no skip attribute, so an empty list emits `[]`);
//     `glob_scan_max_depth` is `Option<NonZeroUsize>` with
//     `skip_serializing_if = "Option::is_none"`.
//   - Unrestricted: carries no additional fields.
//
// glob_scan_max_depth is modeled as *uint so a nil pointer is omitted; a present
// value serializes as a plain integer. (Rust uses NonZeroUsize, which differs
// only in that zero is unrepresentable; the wire form is identical.)
type ManagedFileSystemPermissions struct {
	Kind ManagedFileSystemPermissionsKind

	// Entries applies to Restricted. A nil slice still serializes as `[]`.
	Entries []FileSystemSandboxEntry
	// GlobScanMaxDepth applies to Restricted; nil is omitted.
	GlobScanMaxDepth *uint
}

// NewRestrictedManagedFileSystem builds a Restricted managed filesystem value.
func NewRestrictedManagedFileSystem(entries []FileSystemSandboxEntry, globScanMaxDepth *uint) ManagedFileSystemPermissions {
	return ManagedFileSystemPermissions{
		Kind:             ManagedFileSystemPermissionsRestricted,
		Entries:          entries,
		GlobScanMaxDepth: globScanMaxDepth,
	}
}

// NewUnrestrictedManagedFileSystem builds an Unrestricted managed filesystem
// value.
func NewUnrestrictedManagedFileSystem() ManagedFileSystemPermissions {
	return ManagedFileSystemPermissions{Kind: ManagedFileSystemPermissionsUnrestricted}
}

// ToSandboxPolicy converts the managed permissions into a FileSystemSandboxPolicy
// (Rust `to_sandbox_policy`).
func (m ManagedFileSystemPermissions) ToSandboxPolicy() FileSystemSandboxPolicy {
	switch m.Kind {
	case ManagedFileSystemPermissionsRestricted:
		return FileSystemSandboxPolicy{
			Kind:             FileSystemSandboxKindRestricted,
			GlobScanMaxDepth: m.GlobScanMaxDepth,
			Entries:          m.Entries,
		}
	default: // Unrestricted
		return FileSystemSandboxPolicy{Kind: FileSystemSandboxKindUnrestricted}
	}
}

// MarshalJSON encodes the internally-tagged managed filesystem permissions.
func (m ManagedFileSystemPermissions) MarshalJSON() ([]byte, error) {
	obj := map[string]any{"type": string(m.Kind)}
	switch m.Kind {
	case ManagedFileSystemPermissionsRestricted:
		// `entries` has no skip attribute in Rust, so an empty list emits `[]`.
		entries := m.Entries
		if entries == nil {
			entries = []FileSystemSandboxEntry{}
		}
		obj["entries"] = entries
		if m.GlobScanMaxDepth != nil {
			obj["glob_scan_max_depth"] = *m.GlobScanMaxDepth
		}
	case ManagedFileSystemPermissionsUnrestricted:
		// no additional fields
	default:
		return nil, fmt.Errorf("unknown ManagedFileSystemPermissions kind: %q", m.Kind)
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes the internally-tagged managed filesystem permissions.
func (m *ManagedFileSystemPermissions) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type             ManagedFileSystemPermissionsKind `json:"type"`
		Entries          []FileSystemSandboxEntry         `json:"entries"`
		GlobScanMaxDepth *uint                            `json:"glob_scan_max_depth"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := ManagedFileSystemPermissions{Kind: raw.Type}
	switch raw.Type {
	case ManagedFileSystemPermissionsRestricted:
		out.Entries = raw.Entries
		out.GlobScanMaxDepth = raw.GlobScanMaxDepth
	case ManagedFileSystemPermissionsUnrestricted:
		// no additional fields
	default:
		return fmt.Errorf("unknown ManagedFileSystemPermissions kind: %q", raw.Type)
	}
	*m = out
	return nil
}

// PermissionProfileKind is the discriminator for PermissionProfile.
type PermissionProfileKind string

const (
	// PermissionProfileManaged means Codex owns sandbox construction.
	PermissionProfileManaged PermissionProfileKind = "managed"
	// PermissionProfileDisabled means no outer sandbox is applied.
	PermissionProfileDisabled PermissionProfileKind = "disabled"
	// PermissionProfileExternal means filesystem isolation is enforced by an
	// external caller.
	PermissionProfileExternal PermissionProfileKind = "external"
)

// PermissionProfile is the canonical active runtime permissions for a
// conversation, turn, or command.
//
// Rust serializes this as an internally-tagged enum (`#[serde(tag = "type",
// rename_all = "snake_case")]`):
//   - Managed { file_system, network }
//   - Disabled
//   - External { network }
//
// Deserialization is untagged with two alternatives tried in order:
//  1. The tagged shape above (TaggedPermissionProfile).
//  2. A legacy shape `{ network?, file_system? }` (LegacyPermissionProfile,
//     `deny_unknown_fields`) written to older rollout files before enforcement
//     was explicit. The legacy shape is normalized into the tagged
//     representation via from_runtime_permissions semantics.
type PermissionProfile struct {
	Kind PermissionProfileKind

	// FileSystem applies to Managed.
	FileSystem ManagedFileSystemPermissions
	// Network applies to Managed and External.
	Network NetworkSandboxPolicy
}

// NewManagedPermissionProfile builds a Managed profile.
func NewManagedPermissionProfile(fileSystem ManagedFileSystemPermissions, network NetworkSandboxPolicy) PermissionProfile {
	return PermissionProfile{Kind: PermissionProfileManaged, FileSystem: fileSystem, Network: network}
}

// NewDisabledPermissionProfile builds a Disabled profile.
func NewDisabledPermissionProfile() PermissionProfile {
	return PermissionProfile{Kind: PermissionProfileDisabled}
}

// NewExternalPermissionProfile builds an External profile.
func NewExternalPermissionProfile(network NetworkSandboxPolicy) PermissionProfile {
	return PermissionProfile{Kind: PermissionProfileExternal, Network: network}
}

// DefaultPermissionProfile returns the Rust `Default`: a Managed profile with an
// empty Restricted filesystem and restricted network access.
func DefaultPermissionProfile() PermissionProfile {
	return NewManagedPermissionProfile(
		NewRestrictedManagedFileSystem(nil, nil),
		NetworkSandboxPolicyRestricted,
	)
}

// Enforcement reports the SandboxEnforcement implied by the profile variant.
func (p PermissionProfile) Enforcement() SandboxEnforcement {
	switch p.Kind {
	case PermissionProfileManaged:
		return SandboxEnforcementManaged
	case PermissionProfileDisabled:
		return SandboxEnforcementDisabled
	default: // External
		return SandboxEnforcementExternal
	}
}

// MarshalJSON encodes the internally-tagged permission profile object.
func (p PermissionProfile) MarshalJSON() ([]byte, error) {
	obj := map[string]any{"type": string(p.Kind)}
	switch p.Kind {
	case PermissionProfileManaged:
		obj["file_system"] = p.FileSystem
		obj["network"] = string(p.Network)
	case PermissionProfileDisabled:
		// no additional fields
	case PermissionProfileExternal:
		obj["network"] = string(p.Network)
	default:
		return nil, fmt.Errorf("unknown PermissionProfile kind: %q", p.Kind)
	}
	return json.Marshal(obj)
}

// UnmarshalJSON decodes a permission profile, preferring the tagged shape and
// falling back to the legacy `{ network?, file_system? }` shape (Rust untagged
// PermissionProfileDe).
func (p *PermissionProfile) UnmarshalJSON(data []byte) error {
	if profile, ok := decodeTaggedPermissionProfile(data); ok {
		*p = profile
		return nil
	}
	profile, err := decodeLegacyPermissionProfile(data)
	if err != nil {
		return err
	}
	*p = profile
	return nil
}

// decodeTaggedPermissionProfile attempts to decode the internally-tagged shape.
// It returns ok=false when the `type` discriminator is absent or unrecognized so
// the caller can fall back to the legacy shape.
func decodeTaggedPermissionProfile(data []byte) (PermissionProfile, bool) {
	var raw struct {
		Type       *PermissionProfileKind        `json:"type"`
		FileSystem *ManagedFileSystemPermissions `json:"file_system"`
		Network    *NetworkSandboxPolicy         `json:"network"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return PermissionProfile{}, false
	}
	if raw.Type == nil {
		return PermissionProfile{}, false
	}
	switch *raw.Type {
	case PermissionProfileManaged:
		fs := ManagedFileSystemPermissions{}
		if raw.FileSystem != nil {
			fs = *raw.FileSystem
		}
		network := NetworkSandboxPolicyRestricted
		if raw.Network != nil {
			network = *raw.Network
		}
		return NewManagedPermissionProfile(fs, network), true
	case PermissionProfileDisabled:
		return NewDisabledPermissionProfile(), true
	case PermissionProfileExternal:
		network := NetworkSandboxPolicyRestricted
		if raw.Network != nil {
			network = *raw.Network
		}
		return NewExternalPermissionProfile(network), true
	default:
		return PermissionProfile{}, false
	}
}

// decodeLegacyPermissionProfile decodes the legacy `{ network?, file_system? }`
// rollout shape and normalizes it into a Managed/External tagged profile,
// mirroring the Rust `From<LegacyPermissionProfile>` impl.
func decodeLegacyPermissionProfile(data []byte) (PermissionProfile, error) {
	var legacy struct {
		Network    *NetworkPermissions    `json:"network"`
		FileSystem *FileSystemPermissions `json:"file_system"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return PermissionProfile{}, err
	}

	// Build the filesystem sandbox policy from the legacy overlay. An absent
	// file_system maps to an empty restricted policy.
	var fsPolicy FileSystemSandboxPolicy
	if legacy.FileSystem != nil {
		fsPolicy = fileSystemSandboxPolicyFromPermissions(*legacy.FileSystem)
	} else {
		fsPolicy = FileSystemSandboxPolicy{Kind: FileSystemSandboxKindRestricted}
	}

	networkEnabled := legacy.Network != nil &&
		legacy.Network.Enabled != nil && *legacy.Network.Enabled
	networkPolicy := NetworkSandboxPolicyRestricted
	if networkEnabled {
		networkPolicy = NetworkSandboxPolicyEnabled
	}

	return permissionProfileFromRuntime(fsPolicy, networkPolicy), nil
}

// fileSystemSandboxPolicyFromPermissions mirrors Rust
// `From<&FileSystemPermissions> for FileSystemSandboxPolicy`: a restricted policy
// carrying the overlay entries and glob scan depth.
func fileSystemSandboxPolicyFromPermissions(perms FileSystemPermissions) FileSystemSandboxPolicy {
	return FileSystemSandboxPolicy{
		Kind:             FileSystemSandboxKindRestricted,
		GlobScanMaxDepth: perms.GlobScanMaxDepth,
		Entries:          perms.Entries,
	}
}

// permissionProfileFromRuntime normalizes a filesystem sandbox policy and a
// network policy into a tagged PermissionProfile, mirroring Rust
// `from_runtime_permissions` (which uses Managed enforcement for Restricted and
// Unrestricted kinds and External for ExternalSandbox).
func permissionProfileFromRuntime(fsPolicy FileSystemSandboxPolicy, network NetworkSandboxPolicy) PermissionProfile {
	switch fsPolicy.Kind {
	case FileSystemSandboxKindExternalSandbox:
		return NewExternalPermissionProfile(network)
	case FileSystemSandboxKindUnrestricted:
		return NewManagedPermissionProfile(NewUnrestrictedManagedFileSystem(), network)
	default: // Restricted
		return NewManagedPermissionProfile(
			NewRestrictedManagedFileSystem(fsPolicy.Entries, fsPolicy.GlobScanMaxDepth),
			network,
		)
	}
}

// ActivePermissionProfile is metadata for the named or implicit built-in profile
// that produced the active PermissionProfile.
//
// Rust: `id` always serialized; `extends` is `Option<String>` with
// `skip_serializing_if = "Option::is_none"`.
type ActivePermissionProfile struct {
	ID      string  `json:"id"`
	Extends *string `json:"extends,omitempty"`
}

// NewActivePermissionProfile builds an ActivePermissionProfile with no parent.
func NewActivePermissionProfile(id string) ActivePermissionProfile {
	return ActivePermissionProfile{ID: id}
}

// ReadOnlyActivePermissionProfile returns the built-in read-only profile
// metadata (Rust `ActivePermissionProfile::read_only`).
func ReadOnlyActivePermissionProfile() ActivePermissionProfile {
	return NewActivePermissionProfile(BuiltInPermissionProfileReadOnly)
}
