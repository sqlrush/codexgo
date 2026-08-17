package protocol

import (
	"encoding/json"
	"fmt"
)

// NetworkSandboxPolicy describes whether outbound network access is permitted
// for a filesystem-split permission profile.
//
// Rust: `#[serde(rename_all = "kebab-case")]`, default Restricted. The two
// variants are unit variants, so they serialize as bare kebab-case strings.
type NetworkSandboxPolicy string

const (
	// NetworkSandboxPolicyRestricted denies outbound network access (default).
	NetworkSandboxPolicyRestricted NetworkSandboxPolicy = "restricted"
	// NetworkSandboxPolicyEnabled allows outbound network access.
	NetworkSandboxPolicyEnabled NetworkSandboxPolicy = "enabled"
)

// IsEnabled reports whether network access is enabled.
func (n NetworkSandboxPolicy) IsEnabled() bool { return n == NetworkSandboxPolicyEnabled }

// FileSystemAccessMode is the access level for a filesystem entry.
//
// Rust: `#[serde(rename_all = "lowercase")]`. The Deny variant carries
// `#[serde(alias = "none")]`, so the legacy input value "none" decodes to Deny
// while Deny always serializes as "deny".
type FileSystemAccessMode string

const (
	// FileSystemAccessModeRead grants read access.
	FileSystemAccessModeRead FileSystemAccessMode = "read"
	// FileSystemAccessModeWrite grants read and write access.
	FileSystemAccessModeWrite FileSystemAccessMode = "write"
	// FileSystemAccessModeDeny denies all access. Accepts the legacy alias
	// "none" on input.
	FileSystemAccessModeDeny FileSystemAccessMode = "deny"
)

// CanRead reports whether the mode permits reads.
func (m FileSystemAccessMode) CanRead() bool { return m != FileSystemAccessModeDeny }

// CanWrite reports whether the mode permits writes.
func (m FileSystemAccessMode) CanWrite() bool { return m == FileSystemAccessModeWrite }

// MarshalJSON encodes the access mode as its canonical lowercase string.
func (m FileSystemAccessMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(m))
}

// UnmarshalJSON decodes the access mode, normalizing the legacy "none" alias to
// "deny" to mirror the Rust serde alias.
func (m *FileSystemAccessMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "none" {
		s = string(FileSystemAccessModeDeny)
	}
	*m = FileSystemAccessMode(s)
	return nil
}

// FileSystemSpecialPathKind is the discriminator for FileSystemSpecialPath.
type FileSystemSpecialPathKind string

const (
	// FileSystemSpecialPathKindRoot names the filesystem root.
	FileSystemSpecialPathKindRoot FileSystemSpecialPathKind = "root"
	// FileSystemSpecialPathKindMinimal names the minimal platform default set.
	FileSystemSpecialPathKindMinimal FileSystemSpecialPathKind = "minimal"
	// FileSystemSpecialPathKindProjectRoots names the workspace project roots.
	// Accepts the legacy deserialize alias "current_working_directory".
	FileSystemSpecialPathKindProjectRoots FileSystemSpecialPathKind = "project_roots"
	// FileSystemSpecialPathKindTmpdir names the $TMPDIR directory.
	FileSystemSpecialPathKindTmpdir FileSystemSpecialPathKind = "tmpdir"
	// FileSystemSpecialPathKindSlashTmp names the /tmp directory.
	FileSystemSpecialPathKindSlashTmp FileSystemSpecialPathKind = "slash_tmp"
	// FileSystemSpecialPathKindUnknown preserves forward-compatible special
	// path tokens introduced by newer Codex releases.
	FileSystemSpecialPathKindUnknown FileSystemSpecialPathKind = "unknown"
)

// FileSystemSpecialPath is a symbolic path token resolved at runtime.
//
// Rust models this as an internally-tagged enum (`#[serde(tag = "kind",
// rename_all = "snake_case")]`). Most variants carry no fields; ProjectRoots
// and Unknown carry an optional `subpath` (skipped when None), and Unknown also
// carries a required `path` string.
//
// The ProjectRoots variant accepts the legacy alias "current_working_directory"
// on input but always serializes as "project_roots".
type FileSystemSpecialPath struct {
	Kind FileSystemSpecialPathKind

	// Subpath applies to ProjectRoots and Unknown; nil/empty is omitted.
	Subpath *string

	// Path is required for the Unknown variant only.
	Path string
}

// NewProjectRootsSpecialPath builds a ProjectRoots special path with the given
// optional subpath.
func NewProjectRootsSpecialPath(subpath *string) FileSystemSpecialPath {
	return FileSystemSpecialPath{Kind: FileSystemSpecialPathKindProjectRoots, Subpath: subpath}
}

// NewUnknownSpecialPath builds an Unknown special path.
func NewUnknownSpecialPath(path string, subpath *string) FileSystemSpecialPath {
	return FileSystemSpecialPath{Kind: FileSystemSpecialPathKindUnknown, Path: path, Subpath: subpath}
}

// MarshalJSON encodes the internally-tagged special path object.
func (p FileSystemSpecialPath) MarshalJSON() ([]byte, error) {
	m := map[string]any{"kind": string(p.Kind)}
	switch p.Kind {
	case FileSystemSpecialPathKindRoot,
		FileSystemSpecialPathKindMinimal,
		FileSystemSpecialPathKindTmpdir,
		FileSystemSpecialPathKindSlashTmp:
		// no additional fields
	case FileSystemSpecialPathKindProjectRoots:
		if p.Subpath != nil {
			m["subpath"] = *p.Subpath
		}
	case FileSystemSpecialPathKindUnknown:
		m["path"] = p.Path
		if p.Subpath != nil {
			m["subpath"] = *p.Subpath
		}
	default:
		return nil, fmt.Errorf("unknown FileSystemSpecialPath kind: %q", p.Kind)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the internally-tagged special path object, normalizing
// the legacy "current_working_directory" alias to "project_roots".
func (p *FileSystemSpecialPath) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind    string  `json:"kind"`
		Subpath *string `json:"subpath"`
		Path    string  `json:"path"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	kind := raw.Kind
	if kind == "current_working_directory" {
		kind = string(FileSystemSpecialPathKindProjectRoots)
	}

	out := FileSystemSpecialPath{Kind: FileSystemSpecialPathKind(kind)}
	switch FileSystemSpecialPathKind(kind) {
	case FileSystemSpecialPathKindRoot,
		FileSystemSpecialPathKindMinimal,
		FileSystemSpecialPathKindTmpdir,
		FileSystemSpecialPathKindSlashTmp:
		// no additional fields
	case FileSystemSpecialPathKindProjectRoots:
		out.Subpath = raw.Subpath
	case FileSystemSpecialPathKindUnknown:
		out.Path = raw.Path
		out.Subpath = raw.Subpath
	default:
		return fmt.Errorf("unknown FileSystemSpecialPath kind: %q", raw.Kind)
	}
	*p = out
	return nil
}

// FileSystemPathType is the discriminator for FileSystemPath.
type FileSystemPathType string

const (
	// FileSystemPathTypePath is an absolute path entry.
	FileSystemPathTypePath FileSystemPathType = "path"
	// FileSystemPathTypeGlobPattern is a git-style glob pattern entry. Pattern
	// entries currently support FileSystemAccessModeDeny only.
	FileSystemPathTypeGlobPattern FileSystemPathType = "glob_pattern"
	// FileSystemPathTypeSpecial wraps a symbolic FileSystemSpecialPath.
	FileSystemPathTypeSpecial FileSystemPathType = "special"
)

// FileSystemPath identifies a filesystem location targeted by a sandbox entry.
//
// Rust models this as an internally-tagged enum (`#[serde(tag = "type",
// rename_all = "snake_case")]`) with three variants: Path { path }, GlobPattern
// { pattern }, and Special { value }.
type FileSystemPath struct {
	Type FileSystemPathType

	// Path is set for the Path variant.
	Path AbsolutePath
	// Pattern is set for the GlobPattern variant.
	Pattern string
	// Special is set for the Special variant.
	Special *FileSystemSpecialPath
}

// NewFileSystemPath builds an absolute-path FileSystemPath.
func NewFileSystemPath(path AbsolutePath) FileSystemPath {
	return FileSystemPath{Type: FileSystemPathTypePath, Path: path}
}

// NewFileSystemGlobPattern builds a glob-pattern FileSystemPath.
func NewFileSystemGlobPattern(pattern string) FileSystemPath {
	return FileSystemPath{Type: FileSystemPathTypeGlobPattern, Pattern: pattern}
}

// NewFileSystemSpecialPath builds a special-token FileSystemPath.
func NewFileSystemSpecialPath(value FileSystemSpecialPath) FileSystemPath {
	return FileSystemPath{Type: FileSystemPathTypeSpecial, Special: &value}
}

// MarshalJSON encodes the internally-tagged filesystem path object.
func (p FileSystemPath) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(p.Type)}
	switch p.Type {
	case FileSystemPathTypePath:
		m["path"] = p.Path
	case FileSystemPathTypeGlobPattern:
		m["pattern"] = p.Pattern
	case FileSystemPathTypeSpecial:
		value := FileSystemSpecialPath{}
		if p.Special != nil {
			value = *p.Special
		}
		m["value"] = value
	default:
		return nil, fmt.Errorf("unknown FileSystemPath type: %q", p.Type)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the internally-tagged filesystem path object.
func (p *FileSystemPath) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type    FileSystemPathType     `json:"type"`
		Path    AbsolutePath           `json:"path"`
		Pattern string                 `json:"pattern"`
		Value   *FileSystemSpecialPath `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	out := FileSystemPath{Type: raw.Type}
	switch raw.Type {
	case FileSystemPathTypePath:
		out.Path = raw.Path
	case FileSystemPathTypeGlobPattern:
		out.Pattern = raw.Pattern
	case FileSystemPathTypeSpecial:
		out.Special = raw.Value
	default:
		return fmt.Errorf("unknown FileSystemPath type: %q", raw.Type)
	}
	*p = out
	return nil
}

// FileSystemSandboxEntry pairs a path with its access mode.
//
// Rust: a plain struct, fields serialized by name.
type FileSystemSandboxEntry struct {
	Path   FileSystemPath       `json:"path"`
	Access FileSystemAccessMode `json:"access"`
}

// FileSystemSandboxKind selects the overall enforcement model of a filesystem
// sandbox policy.
//
// Rust: `#[serde(rename_all = "kebab-case")]`, default Restricted.
type FileSystemSandboxKind string

const (
	// FileSystemSandboxKindRestricted enforces the entry list (default).
	FileSystemSandboxKindRestricted FileSystemSandboxKind = "restricted"
	// FileSystemSandboxKindUnrestricted allows all filesystem access.
	FileSystemSandboxKindUnrestricted FileSystemSandboxKind = "unrestricted"
	// FileSystemSandboxKindExternalSandbox defers enforcement to an external
	// sandbox.
	FileSystemSandboxKindExternalSandbox FileSystemSandboxKind = "external-sandbox"
)

// FileSystemSandboxPolicy is the canonical filesystem sandbox configuration.
//
// Rust serde rules:
//   - `kind` is always serialized.
//   - `glob_scan_max_depth` is `Option<usize>` with
//     `skip_serializing_if = "Option::is_none"`.
//   - `entries` is `Vec<...>` with `skip_serializing_if = "Vec::is_empty"`.
//
// glob_scan_max_depth is modeled as *uint so a nil pointer is omitted and a
// present value (including zero) is emitted.
type FileSystemSandboxPolicy struct {
	Kind             FileSystemSandboxKind
	GlobScanMaxDepth *uint
	Entries          []FileSystemSandboxEntry
}

// MarshalJSON encodes the policy, skipping empty optional fields to match serde.
func (p FileSystemSandboxPolicy) MarshalJSON() ([]byte, error) {
	m := map[string]any{"kind": string(p.Kind)}
	if p.GlobScanMaxDepth != nil {
		m["glob_scan_max_depth"] = *p.GlobScanMaxDepth
	}
	if len(p.Entries) > 0 {
		m["entries"] = p.Entries
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the policy. Missing optional fields decode to their zero
// values (nil pointer / nil slice), mirroring serde `#[serde(default)]`.
func (p *FileSystemSandboxPolicy) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind             FileSystemSandboxKind    `json:"kind"`
		GlobScanMaxDepth *uint                    `json:"glob_scan_max_depth"`
		Entries          []FileSystemSandboxEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = FileSystemSandboxPolicy{
		Kind:             raw.Kind,
		GlobScanMaxDepth: raw.GlobScanMaxDepth,
		Entries:          raw.Entries,
	}
	return nil
}

// DefaultFileSystemSandboxPolicy returns the Rust `Default` value: a restricted
// policy that grants read access to the filesystem root.
func DefaultFileSystemSandboxPolicy() FileSystemSandboxPolicy {
	return FileSystemSandboxPolicy{
		Kind: FileSystemSandboxKindRestricted,
		Entries: []FileSystemSandboxEntry{{
			Path:   NewFileSystemSpecialPath(FileSystemSpecialPath{Kind: FileSystemSpecialPathKindRoot}),
			Access: FileSystemAccessModeRead,
		}},
	}
}

// FileSystemPermissions is a partial filesystem permission overlay used by
// per-command permission requests and approved grants.
//
// Rust uses a hand-written Serialize/Deserialize pair with an untagged input
// shape:
//   - Canonical form: `{ "entries": [...], "glob_scan_max_depth": N }` (both
//     fields skipped when empty/None).
//   - Legacy form: `{ "read": [...], "write": [...] }` (both fields skipped when
//     None).
//
// On serialize, the value uses the legacy form only when it is expressible as
// read/write roots (no glob_scan_max_depth, every entry an absolute Path entry
// with Read or Write access); otherwise it uses the canonical form. We replicate
// that exact decision so output bytes match Rust.
type FileSystemPermissions struct {
	Entries          []FileSystemSandboxEntry
	GlobScanMaxDepth *uint
}

// IsEmpty reports whether the overlay carries no entries.
func (f FileSystemPermissions) IsEmpty() bool { return len(f.Entries) == 0 }

// asLegacyRoots returns the read and write root lists when the permissions are
// expressible in the legacy read/write shape, or (nil, nil, false) otherwise.
// nil slices distinguish "absent" (skipped) from "present but empty"; per Rust,
// an empty list is reported as absent.
func (f FileSystemPermissions) asLegacyRoots() (read, write []AbsolutePath, ok bool) {
	if f.GlobScanMaxDepth != nil {
		return nil, nil, false
	}
	for _, entry := range f.Entries {
		if entry.Path.Type != FileSystemPathTypePath {
			return nil, nil, false
		}
		switch entry.Access {
		case FileSystemAccessModeRead:
			read = append(read, entry.Path.Path)
		case FileSystemAccessModeWrite:
			write = append(write, entry.Path.Path)
		default: // Deny
			return nil, nil, false
		}
	}
	if len(read) == 0 {
		read = nil
	}
	if len(write) == 0 {
		write = nil
	}
	return read, write, true
}

// MarshalJSON serializes using the legacy read/write shape when expressible, or
// the canonical entries shape otherwise, matching the Rust Serialize impl.
func (f FileSystemPermissions) MarshalJSON() ([]byte, error) {
	if read, write, ok := f.asLegacyRoots(); ok {
		m := map[string]any{}
		if read != nil {
			m["read"] = read
		}
		if write != nil {
			m["write"] = write
		}
		return json.Marshal(m)
	}

	m := map[string]any{}
	if len(f.Entries) > 0 {
		m["entries"] = f.Entries
	}
	if f.GlobScanMaxDepth != nil {
		m["glob_scan_max_depth"] = *f.GlobScanMaxDepth
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes either the canonical or legacy shape (untagged in Rust).
// The canonical form is attempted first; a legacy {read,write} object decodes
// into equivalent Path entries.
func (f *FileSystemPermissions) UnmarshalJSON(data []byte) error {
	// Canonical form: entries / glob_scan_max_depth. deny_unknown_fields in Rust
	// means a {read,write} object is NOT a valid canonical value, so we detect
	// the legacy shape by the presence of those keys.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	_, hasRead := probe["read"]
	_, hasWrite := probe["write"]
	_, hasEntries := probe["entries"]
	_, hasDepth := probe["glob_scan_max_depth"]

	if (hasRead || hasWrite) && !hasEntries && !hasDepth {
		var legacy struct {
			Read  []AbsolutePath `json:"read"`
			Write []AbsolutePath `json:"write"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		*f = fileSystemPermissionsFromReadWrite(legacy.Read, legacy.Write)
		return nil
	}

	var canonical struct {
		Entries          []FileSystemSandboxEntry `json:"entries"`
		GlobScanMaxDepth *uint                    `json:"glob_scan_max_depth"`
	}
	if err := json.Unmarshal(data, &canonical); err != nil {
		return err
	}
	*f = FileSystemPermissions{Entries: canonical.Entries, GlobScanMaxDepth: canonical.GlobScanMaxDepth}
	return nil
}

// fileSystemPermissionsFromReadWrite builds permissions from legacy read/write
// root lists, mirroring `FileSystemPermissions::from_read_write_roots`.
func fileSystemPermissionsFromReadWrite(read, write []AbsolutePath) FileSystemPermissions {
	var entries []FileSystemSandboxEntry
	for _, path := range read {
		entries = append(entries, FileSystemSandboxEntry{
			Path:   NewFileSystemPath(path),
			Access: FileSystemAccessModeRead,
		})
	}
	for _, path := range write {
		entries = append(entries, FileSystemSandboxEntry{
			Path:   NewFileSystemPath(path),
			Access: FileSystemAccessModeWrite,
		})
	}
	return FileSystemPermissions{Entries: entries}
}

// NetworkPermissions is the network half of a partial permission overlay.
//
// Rust: a plain struct with a single `Option<bool>` field `enabled`. The field
// has no skip attribute, so `null` is emitted when None.
type NetworkPermissions struct {
	Enabled *bool `json:"enabled"`
}

// IsEmpty reports whether the network overlay carries no decision.
func (n NetworkPermissions) IsEmpty() bool { return n.Enabled == nil }

// AdditionalPermissionProfile is a partial permission overlay used for
// per-command requests and approved session/turn grants.
//
// Rust: a plain struct; both fields are `Option<...>` with no skip attribute,
// so `null` is emitted when None.
type AdditionalPermissionProfile struct {
	Network    *NetworkPermissions    `json:"network"`
	FileSystem *FileSystemPermissions `json:"file_system"`
}

// IsEmpty reports whether neither half of the overlay is populated.
func (p AdditionalPermissionProfile) IsEmpty() bool {
	return p.Network == nil && p.FileSystem == nil
}

// PermissionGrantScope is the lifetime of a granted permission overlay.
//
// Rust: `#[serde(rename_all = "snake_case")]`, default Turn.
type PermissionGrantScope string

const (
	// PermissionGrantScopeTurn grants the overlay for the current turn only
	// (default).
	PermissionGrantScopeTurn PermissionGrantScope = "turn"
	// PermissionGrantScopeSession grants the overlay for the whole session.
	PermissionGrantScopeSession PermissionGrantScope = "session"
)

// RequestPermissionProfile is the permission overlay carried by permission
// request/response payloads.
//
// Rust: `#[serde(deny_unknown_fields)]`; both fields are `Option<...>` with no
// skip attribute, so `null` is emitted when None.
type RequestPermissionProfile struct {
	Network    *NetworkPermissions    `json:"network"`
	FileSystem *FileSystemPermissions `json:"file_system"`
}

// IsEmpty reports whether neither half of the profile is populated.
func (p RequestPermissionProfile) IsEmpty() bool {
	return p.Network == nil && p.FileSystem == nil
}

// ToAdditionalPermissionProfile converts the request profile into the broader
// AdditionalPermissionProfile overlay (Rust `From` impl).
func (p RequestPermissionProfile) ToAdditionalPermissionProfile() AdditionalPermissionProfile {
	return AdditionalPermissionProfile{Network: p.Network, FileSystem: p.FileSystem}
}

// RequestPermissionProfileFromAdditional converts an AdditionalPermissionProfile
// into a RequestPermissionProfile (Rust `From` impl).
func RequestPermissionProfileFromAdditional(p AdditionalPermissionProfile) RequestPermissionProfile {
	return RequestPermissionProfile{Network: p.Network, FileSystem: p.FileSystem}
}

// RequestPermissionsArgs are the arguments for a permission request tool call.
//
// Rust: `reason` is `Option<String>` with `skip_serializing_if = Option::is_none`;
// `permissions` is always serialized.
type RequestPermissionsArgs struct {
	Reason      *string                  `json:"reason,omitempty"`
	Permissions RequestPermissionProfile `json:"permissions"`
}

// RequestPermissionsResponse is the response to a permission request.
//
// Rust: `scope` is `#[serde(default)]` (default Turn); `strict_auto_review` is
// `#[serde(default, skip_serializing_if = "Not::not")]`, so it is omitted when
// false.
type RequestPermissionsResponse struct {
	Permissions      RequestPermissionProfile `json:"permissions"`
	Scope            PermissionGrantScope     `json:"scope"`
	StrictAutoReview bool                     `json:"strict_auto_review,omitempty"`
}

// RequestPermissionsEvent notifies the UI that the agent requested permissions.
//
// Rust serde rules:
//   - `call_id` always serialized.
//   - `turn_id` is `#[serde(default)]` (omitted-on-input tolerated; always
//     emitted, even when empty, as a plain String field).
//   - `started_at_ms` always serialized (i64).
//   - `reason` is `Option<String>` with `skip_serializing_if = Option::is_none`.
//   - `permissions` always serialized.
//   - `cwd` is `Option<AbsolutePathBuf>` with `skip_serializing_if =
//     Option::is_none`.
type RequestPermissionsEvent struct {
	CallID      string                   `json:"call_id"`
	TurnID      string                   `json:"turn_id"`
	StartedAtMs int64                    `json:"started_at_ms"`
	Reason      *string                  `json:"reason,omitempty"`
	Permissions RequestPermissionProfile `json:"permissions"`
	Cwd         *AbsolutePath            `json:"cwd,omitempty"`
}
