package protocol

import (
	"encoding/json"
	"fmt"
)

// NetworkAccess represents whether outbound network access is available to the
// agent. Rust: kebab-case, default Restricted.
type NetworkAccess string

const (
	NetworkAccessRestricted NetworkAccess = "restricted"
	NetworkAccessEnabled    NetworkAccess = "enabled"
)

// IsEnabled reports whether outbound network access is enabled.
func (n NetworkAccess) IsEnabled() bool { return n == NetworkAccessEnabled }

// SandboxPolicyType is the discriminator for SandboxPolicy. Rust uses
// `#[serde(tag = "type", rename_all = "kebab-case")]` with explicit renames.
type SandboxPolicyType string

const (
	SandboxPolicyTypeDangerFullAccess SandboxPolicyType = "danger-full-access"
	SandboxPolicyTypeReadOnly         SandboxPolicyType = "read-only"
	SandboxPolicyTypeExternalSandbox  SandboxPolicyType = "external-sandbox"
	SandboxPolicyTypeWorkspaceWrite   SandboxPolicyType = "workspace-write"
)

// SandboxPolicy determines execution restrictions for model shell commands.
//
// This is an internally-tagged enum on the wire (discriminator field "type").
// Because each variant carries different fields, it is modeled as a single
// struct holding all possible fields plus the Type discriminator; custom
// JSON marshaling emits exactly the fields the matching Rust variant would.
//
// Field serialization rules (mirroring serde):
//   - DangerFullAccess: emits only {"type":"danger-full-access"}.
//   - ReadOnly: emits "network_access" only when true (skip_serializing_if Not).
//   - ExternalSandbox: always emits "network_access" (NetworkAccess, default
//     Restricted).
//   - WorkspaceWrite: emits "writable_roots" only when non-empty; always emits
//     "network_access", "exclude_tmpdir_env_var", "exclude_slash_tmp".
type SandboxPolicy struct {
	Type SandboxPolicyType

	// ReadOnly / WorkspaceWrite: boolean network access (defaults false).
	NetworkAccessBool bool
	// ExternalSandbox: structured network access (defaults Restricted).
	NetworkAccess NetworkAccess

	// WorkspaceWrite fields.
	WritableRoots       []AbsolutePath
	ExcludeTmpdirEnvVar bool
	ExcludeSlashTmp     bool
}

// NewReadOnlySandboxPolicy constructs a read-only policy.
func NewReadOnlySandboxPolicy(networkAccess bool) SandboxPolicy {
	return SandboxPolicy{Type: SandboxPolicyTypeReadOnly, NetworkAccessBool: networkAccess}
}

// NewDangerFullAccessSandboxPolicy constructs a danger-full-access policy.
func NewDangerFullAccessSandboxPolicy() SandboxPolicy {
	return SandboxPolicy{Type: SandboxPolicyTypeDangerFullAccess}
}

// MarshalJSON encodes the SandboxPolicy as an internally-tagged JSON object.
func (s SandboxPolicy) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(s.Type)}
	switch s.Type {
	case SandboxPolicyTypeDangerFullAccess:
		// no additional fields
	case SandboxPolicyTypeReadOnly:
		if s.NetworkAccessBool {
			m["network_access"] = true
		}
	case SandboxPolicyTypeExternalSandbox:
		na := s.NetworkAccess
		if na == "" {
			na = NetworkAccessRestricted
		}
		m["network_access"] = string(na)
	case SandboxPolicyTypeWorkspaceWrite:
		if len(s.WritableRoots) > 0 {
			m["writable_roots"] = s.WritableRoots
		}
		m["network_access"] = s.NetworkAccessBool
		m["exclude_tmpdir_env_var"] = s.ExcludeTmpdirEnvVar
		m["exclude_slash_tmp"] = s.ExcludeSlashTmp
	default:
		return nil, fmt.Errorf("unknown SandboxPolicy type: %q", s.Type)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes an internally-tagged SandboxPolicy.
func (s *SandboxPolicy) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type                SandboxPolicyType `json:"type"`
		NetworkAccess       json.RawMessage   `json:"network_access"`
		WritableRoots       []AbsolutePath    `json:"writable_roots"`
		ExcludeTmpdirEnvVar bool              `json:"exclude_tmpdir_env_var"`
		ExcludeSlashTmp     bool              `json:"exclude_slash_tmp"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	out := SandboxPolicy{Type: raw.Type}
	switch raw.Type {
	case SandboxPolicyTypeDangerFullAccess:
		// no fields
	case SandboxPolicyTypeReadOnly:
		if len(raw.NetworkAccess) > 0 {
			if err := json.Unmarshal(raw.NetworkAccess, &out.NetworkAccessBool); err != nil {
				return err
			}
		}
	case SandboxPolicyTypeExternalSandbox:
		out.NetworkAccess = NetworkAccessRestricted
		if len(raw.NetworkAccess) > 0 {
			if err := json.Unmarshal(raw.NetworkAccess, &out.NetworkAccess); err != nil {
				return err
			}
		}
	case SandboxPolicyTypeWorkspaceWrite:
		out.WritableRoots = raw.WritableRoots
		out.ExcludeTmpdirEnvVar = raw.ExcludeTmpdirEnvVar
		out.ExcludeSlashTmp = raw.ExcludeSlashTmp
		if len(raw.NetworkAccess) > 0 {
			if err := json.Unmarshal(raw.NetworkAccess, &out.NetworkAccessBool); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown SandboxPolicy type: %q", raw.Type)
	}
	*s = out
	return nil
}

// WritableRoot is a writable root path accompanied by subpaths that should
// remain read-only and protected metadata names. This type is not Serialize in
// Rust (only JsonSchema), so the JSON tags follow the field names directly.
type WritableRoot struct {
	Root                   AbsolutePath   `json:"root"`
	ReadOnlySubpaths       []AbsolutePath `json:"read_only_subpaths"`
	ProtectedMetadataNames []string       `json:"protected_metadata_names"`
}
