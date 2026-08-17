package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func uintPtr(v uint) *uint { return &v }

func TestSandboxEnforcementVariants(t *testing.T) {
	cases := []struct {
		value SandboxEnforcement
		want  string
	}{
		{SandboxEnforcementManaged, `"managed"`},
		{SandboxEnforcementDisabled, `"disabled"`},
		{SandboxEnforcementExternal, `"external"`},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.value)
		if err != nil {
			t.Fatalf("marshal %s: %v", tc.value, err)
		}
		if string(b) != tc.want {
			t.Fatalf("JSON mismatch for %s:\n got: %s\nwant: %s", tc.value, b, tc.want)
		}
		var got SandboxEnforcement
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.value, err)
		}
		if got != tc.value {
			t.Fatalf("round-trip mismatch for %s: %q", tc.value, got)
		}
	}
}

func TestManagedFileSystemPermissionsRestricted(t *testing.T) {
	// entries has no skip attribute, so an empty list emits `[]`.
	m := NewRestrictedManagedFileSystem(nil, nil)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"entries":[],"type":"restricted"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got ManagedFileSystemPermissions
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != ManagedFileSystemPermissionsRestricted || len(got.Entries) != 0 || got.GlobScanMaxDepth != nil {
		t.Fatalf("decoded mismatch: %+v", got)
	}
}

func TestManagedFileSystemPermissionsRestrictedWithDepthAndEntries(t *testing.T) {
	entries := []FileSystemSandboxEntry{{
		Path:   NewFileSystemSpecialPath(FileSystemSpecialPath{Kind: FileSystemSpecialPathKindRoot}),
		Access: FileSystemAccessModeRead,
	}}
	m := NewRestrictedManagedFileSystem(entries, uintPtr(2))
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"entries":[{"path":{"type":"special","value":{"kind":"root"}},"access":"read"}],"glob_scan_max_depth":2,"type":"restricted"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got ManagedFileSystemPermissions
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, m)
	}
}

func TestManagedFileSystemPermissionsUnrestricted(t *testing.T) {
	m := NewUnrestrictedManagedFileSystem()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"unrestricted"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got ManagedFileSystemPermissions
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != ManagedFileSystemPermissionsUnrestricted {
		t.Fatalf("decoded mismatch: %+v", got)
	}
}

func TestPermissionProfileManaged(t *testing.T) {
	p := NewManagedPermissionProfile(
		NewUnrestrictedManagedFileSystem(),
		NetworkSandboxPolicyEnabled,
	)
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"file_system":{"type":"unrestricted"},"network":"enabled","type":"managed"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got PermissionProfile
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, p)
	}
	if got.Enforcement() != SandboxEnforcementManaged {
		t.Fatalf("enforcement mismatch: %q", got.Enforcement())
	}
}

func TestPermissionProfileDisabled(t *testing.T) {
	p := NewDisabledPermissionProfile()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"disabled"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got PermissionProfile
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, p)
	}
	if got.Enforcement() != SandboxEnforcementDisabled {
		t.Fatalf("enforcement mismatch: %q", got.Enforcement())
	}
}

func TestPermissionProfileExternal(t *testing.T) {
	p := NewExternalPermissionProfile(NetworkSandboxPolicyRestricted)
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"network":"restricted","type":"external"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
	var got PermissionProfile
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, p)
	}
	if got.Enforcement() != SandboxEnforcementExternal {
		t.Fatalf("enforcement mismatch: %q", got.Enforcement())
	}
}

func TestPermissionProfileDefault(t *testing.T) {
	p := DefaultPermissionProfile()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"file_system":{"entries":[],"type":"restricted"},"network":"restricted","type":"managed"}`
	if string(b) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b, want)
	}
}

// TestPermissionProfileLegacyDecode verifies the untagged fallback to the legacy
// { network?, file_system? } rollout shape used by older Codex releases.
func TestPermissionProfileLegacyDecodeNetworkEnabled(t *testing.T) {
	in := []byte(`{"network":{"enabled":true},"file_system":{"read":["/a"]}}`)
	var got PermissionProfile
	if err := json.Unmarshal(in, &got); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}
	if got.Kind != PermissionProfileManaged {
		t.Fatalf("expected managed, got %q", got.Kind)
	}
	if got.Network != NetworkSandboxPolicyEnabled {
		t.Fatalf("expected enabled network, got %q", got.Network)
	}
	if got.FileSystem.Kind != ManagedFileSystemPermissionsRestricted {
		t.Fatalf("expected restricted fs, got %q", got.FileSystem.Kind)
	}
	if len(got.FileSystem.Entries) != 1 ||
		got.FileSystem.Entries[0].Access != FileSystemAccessModeRead ||
		got.FileSystem.Entries[0].Path.Type != FileSystemPathTypePath ||
		got.FileSystem.Entries[0].Path.Path != AbsolutePath("/a") {
		t.Fatalf("legacy file_system entries mismatch: %+v", got.FileSystem.Entries)
	}
}

func TestPermissionProfileLegacyDecodeEmpty(t *testing.T) {
	in := []byte(`{}`)
	var got PermissionProfile
	if err := json.Unmarshal(in, &got); err != nil {
		t.Fatalf("unmarshal legacy empty: %v", err)
	}
	// Absent file_system maps to an empty restricted policy; absent network maps
	// to restricted.
	if got.Kind != PermissionProfileManaged ||
		got.Network != NetworkSandboxPolicyRestricted ||
		got.FileSystem.Kind != ManagedFileSystemPermissionsRestricted ||
		len(got.FileSystem.Entries) != 0 {
		t.Fatalf("legacy empty decode mismatch: %+v", got)
	}
}

func TestActivePermissionProfile(t *testing.T) {
	p := NewActivePermissionProfile(":workspace")
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"id":":workspace"}` {
		t.Fatalf("JSON mismatch: %s", b)
	}

	parent := ":read-only"
	withExtends := ActivePermissionProfile{ID: ":workspace", Extends: &parent}
	b2, err := json.Marshal(withExtends)
	if err != nil {
		t.Fatalf("marshal extends: %v", err)
	}
	want := `{"id":":workspace","extends":":read-only"}`
	if string(b2) != want {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", b2, want)
	}
	var got ActivePermissionProfile
	if err := json.Unmarshal(b2, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, withExtends) {
		t.Fatalf("round-trip mismatch: %+v != %+v", got, withExtends)
	}
}

func TestReadOnlyActivePermissionProfile(t *testing.T) {
	p := ReadOnlyActivePermissionProfile()
	if p.ID != BuiltInPermissionProfileReadOnly {
		t.Fatalf("expected read-only id, got %q", p.ID)
	}
}
