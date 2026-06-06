package approvalpresets

import (
	"encoding/json"
	"reflect"
	"testing"
)

func strptr(s string) *string { return &s }

// TestBuiltinApprovalPresets verifies the stable identity (id, label,
// description, approval policy, and selected active profile) of each built-in
// preset, mirroring the upstream definitions exactly.
func TestBuiltinApprovalPresets(t *testing.T) {
	presets := BuiltinApprovalPresets()

	tests := []struct {
		name        string
		id          string
		label       string
		description string
		approval    AskForApproval
		activeID    string
		profileKind PermissionProfileKind
	}{
		{
			name:        "read-only",
			id:          "read-only",
			label:       "Read Only",
			description: "Codex can read files in the current workspace. Approval is required to edit files or access the internet.",
			approval:    AskForApprovalOnRequest,
			activeID:    ":read-only",
			profileKind: PermissionProfileManaged,
		},
		{
			name:        "auto",
			id:          "auto",
			label:       "Default",
			description: "Codex can read and edit files in the current workspace, and run commands. Approval is required to access the internet or edit other files. (Identical to Agent mode)",
			approval:    AskForApprovalOnRequest,
			activeID:    ":workspace",
			profileKind: PermissionProfileManaged,
		},
		{
			name:        "full-access",
			id:          "full-access",
			label:       "Full Access",
			description: "Codex can edit files outside this workspace and access the internet without asking for approval. Exercise caution when using.",
			approval:    AskForApprovalNever,
			activeID:    ":danger-full-access",
			profileKind: PermissionProfileDisabled,
		},
	}

	if len(presets) != len(tests) {
		t.Fatalf("BuiltinApprovalPresets() returned %d presets, want %d", len(presets), len(tests))
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := presets[i]
			if p.ID != tt.id {
				t.Errorf("ID = %q, want %q", p.ID, tt.id)
			}
			if p.Label != tt.label {
				t.Errorf("Label = %q, want %q", p.Label, tt.label)
			}
			if p.Description != tt.description {
				t.Errorf("Description = %q, want %q", p.Description, tt.description)
			}
			if p.Approval != tt.approval {
				t.Errorf("Approval = %q, want %q", p.Approval, tt.approval)
			}
			if p.ActivePermissionProfile.ID != tt.activeID {
				t.Errorf("ActivePermissionProfile.ID = %q, want %q", p.ActivePermissionProfile.ID, tt.activeID)
			}
			if p.ActivePermissionProfile.Extends != nil {
				t.Errorf("ActivePermissionProfile.Extends = %v, want nil", *p.ActivePermissionProfile.Extends)
			}
			if p.PermissionProfile.Type != tt.profileKind {
				t.Errorf("PermissionProfile.Type = %q, want %q", p.PermissionProfile.Type, tt.profileKind)
			}
		})
	}
}

// TestBuiltinApprovalPresetsImmutable verifies that mutating a returned slice
// does not affect the result of a subsequent call.
func TestBuiltinApprovalPresetsImmutable(t *testing.T) {
	first := BuiltinApprovalPresets()
	first[0].ID = "tampered"
	first[0].PermissionProfile.FileSystem.Entries[0].Access = FileSystemAccessWrite

	second := BuiltinApprovalPresets()
	if second[0].ID != "read-only" {
		t.Errorf("second call ID = %q, want %q (mutation leaked)", second[0].ID, "read-only")
	}
	if got := second[0].PermissionProfile.FileSystem.Entries[0].Access; got != FileSystemAccessRead {
		t.Errorf("second call entry access = %q, want %q (mutation leaked)", got, FileSystemAccessRead)
	}
}

// TestBuiltinPermissionProfileForActivePermissionProfile covers each built-in
// id, the extends short-circuit, and unknown ids.
func TestBuiltinPermissionProfileForActivePermissionProfile(t *testing.T) {
	tests := []struct {
		name     string
		active   ActivePermissionProfile
		wantOK   bool
		wantKind PermissionProfileKind
	}{
		{
			name:     "read-only",
			active:   NewActivePermissionProfile(BuiltInPermissionProfileReadOnly),
			wantOK:   true,
			wantKind: PermissionProfileManaged,
		},
		{
			name:     "workspace",
			active:   NewActivePermissionProfile(BuiltInPermissionProfileWorkspace),
			wantOK:   true,
			wantKind: PermissionProfileManaged,
		},
		{
			name:     "danger-full-access",
			active:   NewActivePermissionProfile(BuiltInPermissionProfileDangerFullAccess),
			wantOK:   true,
			wantKind: PermissionProfileDisabled,
		},
		{
			name:   "extends short-circuits to none",
			active: ActivePermissionProfile{ID: BuiltInPermissionProfileReadOnly, Extends: strptr(":workspace")},
			wantOK: false,
		},
		{
			name:   "unknown id",
			active: NewActivePermissionProfile("custom-profile"),
			wantOK: false,
		},
		{
			name:   "empty id",
			active: NewActivePermissionProfile(""),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := BuiltinPermissionProfileForActivePermissionProfile(tt.active)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				if !reflect.DeepEqual(got, PermissionProfile{}) {
					t.Errorf("got = %+v, want zero PermissionProfile", got)
				}
				return
			}
			if got.Type != tt.wantKind {
				t.Errorf("got.Type = %q, want %q", got.Type, tt.wantKind)
			}
		})
	}
}

// TestBuiltinProfileMatchesPresets ensures the lookup function returns the same
// profiles embedded in the presets list for each built-in id.
func TestBuiltinProfileMatchesPresets(t *testing.T) {
	for _, p := range BuiltinApprovalPresets() {
		got, ok := BuiltinPermissionProfileForActivePermissionProfile(p.ActivePermissionProfile)
		if !ok {
			t.Errorf("%s: lookup returned not-found for active id %q", p.ID, p.ActivePermissionProfile.ID)
			continue
		}
		if !reflect.DeepEqual(got, p.PermissionProfile) {
			t.Errorf("%s: lookup profile mismatch\n got: %+v\nwant: %+v", p.ID, got, p.PermissionProfile)
		}
	}
}

// TestPermissionProfileJSON verifies the JSON encoding matches the serde wire
// format used upstream so the values remain drop-in compatible.
func TestPermissionProfileJSON(t *testing.T) {
	tests := []struct {
		name    string
		profile PermissionProfile
		want    string
	}{
		{
			name:    "disabled",
			profile: PermissionProfileDisabledProfile(),
			want:    `{"type":"disabled"}`,
		},
		{
			name:    "read-only",
			profile: PermissionProfileReadOnly(),
			want:    `{"type":"managed","file_system":{"type":"restricted","entries":[{"path":{"type":"special","value":{"kind":"root"}},"access":"read"}]},"network":"restricted"}`,
		},
		{
			name:    "workspace-write",
			profile: PermissionProfileWorkspaceWrite(),
			want: `{"type":"managed","file_system":{"type":"restricted","entries":[` +
				`{"path":{"type":"special","value":{"kind":"root"}},"access":"read"},` +
				`{"path":{"type":"special","value":{"kind":"project_roots"}},"access":"write"},` +
				`{"path":{"type":"special","value":{"kind":"slash_tmp"}},"access":"write"},` +
				`{"path":{"type":"special","value":{"kind":"tmpdir"}},"access":"write"},` +
				`{"path":{"type":"special","value":{"kind":"project_roots","subpath":".git"}},"access":"read"},` +
				`{"path":{"type":"special","value":{"kind":"project_roots","subpath":".agents"}},"access":"read"},` +
				`{"path":{"type":"special","value":{"kind":"project_roots","subpath":".codexgo"}},"access":"read"}` +
				`]},"network":"restricted"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.profile)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(b) != tt.want {
				t.Errorf("json mismatch\n got: %s\nwant: %s", b, tt.want)
			}
		})
	}
}

// TestActivePermissionProfileJSON verifies the active-profile JSON shape,
// including omission of the optional extends field.
func TestActivePermissionProfileJSON(t *testing.T) {
	tests := []struct {
		name    string
		profile ActivePermissionProfile
		want    string
	}{
		{
			name:    "no extends omits field",
			profile: NewActivePermissionProfile(":workspace"),
			want:    `{"id":":workspace"}`,
		},
		{
			name:    "with extends",
			profile: ActivePermissionProfile{ID: "child", Extends: strptr(":workspace")},
			want:    `{"id":"child","extends":":workspace"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.profile)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(b) != tt.want {
				t.Errorf("json mismatch\n got: %s\nwant: %s", b, tt.want)
			}
		})
	}
}

// TestAskForApprovalJSON verifies the kebab-case (and renamed) approval policy
// wire values.
func TestAskForApprovalJSON(t *testing.T) {
	tests := []struct {
		value AskForApproval
		want  string
	}{
		{AskForApprovalUnlessTrusted, `"untrusted"`},
		{AskForApprovalOnFailure, `"on-failure"`},
		{AskForApprovalOnRequest, `"on-request"`},
		{AskForApprovalNever, `"never"`},
	}

	for _, tt := range tests {
		t.Run(string(tt.value), func(t *testing.T) {
			b, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(b) != tt.want {
				t.Errorf("json = %s, want %s", b, tt.want)
			}
		})
	}
}

// TestAccessModeHelpers verifies the access-mode predicate helpers.
func TestAccessModeHelpers(t *testing.T) {
	tests := []struct {
		mode     FileSystemAccessMode
		canRead  bool
		canWrite bool
	}{
		{FileSystemAccessRead, true, false},
		{FileSystemAccessWrite, true, true},
		{FileSystemAccessDeny, false, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := tt.mode.CanRead(); got != tt.canRead {
				t.Errorf("CanRead() = %v, want %v", got, tt.canRead)
			}
			if got := tt.mode.CanWrite(); got != tt.canWrite {
				t.Errorf("CanWrite() = %v, want %v", got, tt.canWrite)
			}
		})
	}
}

// TestGlobScanMaxDepthZeroRejected verifies the guard against an invalid
// (zero) non-zero field at marshal time.
func TestGlobScanMaxDepthZeroRejected(t *testing.T) {
	zero := uint(0)
	m := ManagedFileSystemPermissions{
		Type:             ManagedFileSystemRestricted,
		GlobScanMaxDepth: &zero,
	}
	if _, err := json.Marshal(m); err == nil {
		t.Fatal("expected error marshaling zero glob_scan_max_depth, got nil")
	}

	nonzero := uint(3)
	m.GlobScanMaxDepth = &nonzero
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"type":"restricted","glob_scan_max_depth":3}`
	if string(b) != want {
		t.Errorf("json = %s, want %s", b, want)
	}
}
