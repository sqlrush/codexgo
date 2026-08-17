package sandbox

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// TestToLegacySandboxPolicyExternal verifies the ExternalSandbox arm maps onto
// the legacy external-sandbox policy, carrying the network decision.
func TestToLegacySandboxPolicyExternal(t *testing.T) {
	cwd := t.TempDir()

	enabled, err := toLegacySandboxPolicy(externalSandboxFS(), protocol.NetworkSandboxPolicyEnabled, cwd)
	if err != nil {
		t.Fatalf("external enabled: %v", err)
	}
	if enabled.Type != protocol.SandboxPolicyTypeExternalSandbox || enabled.NetworkAccess != protocol.NetworkAccessEnabled {
		t.Fatalf("external enabled = %+v, want external-sandbox/enabled", enabled)
	}

	restrictedPolicy, err := toLegacySandboxPolicy(externalSandboxFS(), protocol.NetworkSandboxPolicyRestricted, cwd)
	if err != nil {
		t.Fatalf("external restricted: %v", err)
	}
	if restrictedPolicy.NetworkAccess != protocol.NetworkAccessRestricted {
		t.Fatalf("external restricted network = %q, want restricted", restrictedPolicy.NetworkAccess)
	}
}

// TestToLegacySandboxPolicyUnrestricted verifies the Unrestricted arm: enabled
// network => danger-full-access; restricted network => external-sandbox.
func TestToLegacySandboxPolicyUnrestricted(t *testing.T) {
	cwd := t.TempDir()

	enabled, err := toLegacySandboxPolicy(unrestricted(), protocol.NetworkSandboxPolicyEnabled, cwd)
	if err != nil {
		t.Fatalf("unrestricted enabled: %v", err)
	}
	if enabled.Type != protocol.SandboxPolicyTypeDangerFullAccess {
		t.Fatalf("unrestricted+enabled = %+v, want danger-full-access", enabled)
	}

	restrictedPolicy, err := toLegacySandboxPolicy(unrestricted(), protocol.NetworkSandboxPolicyRestricted, cwd)
	if err != nil {
		t.Fatalf("unrestricted restricted: %v", err)
	}
	if restrictedPolicy.Type != protocol.SandboxPolicyTypeExternalSandbox {
		t.Fatalf("unrestricted+restricted = %+v, want external-sandbox", restrictedPolicy)
	}
}

// TestToLegacySandboxPolicyReadOnly verifies that a read-only restricted policy
// (no writable entries) maps onto the legacy read-only policy.
func TestToLegacySandboxPolicyReadOnly(t *testing.T) {
	cwd := t.TempDir()
	policy := restricted(rootEntry(protocol.FileSystemAccessModeRead))

	readonly, err := toLegacySandboxPolicy(policy, protocol.NetworkSandboxPolicyRestricted, cwd)
	if err != nil {
		t.Fatalf("read-only: %v", err)
	}
	if readonly.Type != protocol.SandboxPolicyTypeReadOnly || readonly.NetworkAccessBool {
		t.Fatalf("read-only = %+v, want read-only/network=false", readonly)
	}

	withNet, err := toLegacySandboxPolicy(policy, protocol.NetworkSandboxPolicyEnabled, cwd)
	if err != nil {
		t.Fatalf("read-only with net: %v", err)
	}
	if withNet.Type != protocol.SandboxPolicyTypeReadOnly || !withNet.NetworkAccessBool {
		t.Fatalf("read-only with net = %+v, want read-only/network=true", withNet)
	}
}

// TestToLegacySandboxPolicyWorkspaceWrite verifies a writable-cwd project-roots
// policy projects to legacy workspace-write with the tmpdir/slash-tmp exclusions.
func TestToLegacySandboxPolicyWorkspaceWrite(t *testing.T) {
	cwd := t.TempDir()
	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		projectRootsEntry(protocol.FileSystemAccessModeWrite),
	)

	ws, err := toLegacySandboxPolicy(policy, protocol.NetworkSandboxPolicyRestricted, cwd)
	if err != nil {
		t.Fatalf("workspace-write: %v", err)
	}
	if ws.Type != protocol.SandboxPolicyTypeWorkspaceWrite {
		t.Fatalf("type = %v, want workspace-write", ws.Type)
	}
	if ws.NetworkAccessBool {
		t.Errorf("network = true, want false")
	}
	// Neither $TMPDIR nor /tmp was granted, so both are excluded.
	if !ws.ExcludeTmpdirEnvVar {
		t.Errorf("ExcludeTmpdirEnvVar = false, want true")
	}
	if !ws.ExcludeSlashTmp {
		t.Errorf("ExcludeSlashTmp = false, want true")
	}
}

// TestToLegacySandboxPolicyWriteOutsideWorkspaceErrors verifies the unbridgeable
// arms: a write to an absolute path that is not the cwd cannot be expressed.
func TestToLegacySandboxPolicyWriteOutsideWorkspaceErrors(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		pathEntry(outside, protocol.FileSystemAccessModeWrite),
	)

	if _, err := toLegacySandboxPolicy(policy, protocol.NetworkSandboxPolicyRestricted, cwd); err == nil {
		t.Fatal("expected error for write outside workspace root")
	}
}

// TestMergeGlobScanMaxDepth ports the GlobScanDepth merge rules: depth only
// counts when a deny-glob entry is present; unbounded wins; otherwise max.
func TestMergeGlobScanMaxDepth(t *testing.T) {
	denyGlob := protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemGlobPattern("**/*.env"),
		Access: protocol.FileSystemAccessModeDeny,
	}
	plainRead := rootEntry(protocol.FileSystemAccessModeRead)
	d2, d4 := uint(2), uint(4)

	cases := []struct {
		name         string
		leftEntries  []protocol.FileSystemSandboxEntry
		leftDepth    *uint
		rightEntries []protocol.FileSystemSandboxEntry
		rightDepth   *uint
		want         *uint
	}{
		{
			// No deny glob on either side => no depth.
			name:         "no-deny-glob",
			leftEntries:  []protocol.FileSystemSandboxEntry{plainRead},
			rightEntries: []protocol.FileSystemSandboxEntry{plainRead},
			want:         nil,
		},
		{
			// Bounded on both sides => max.
			name:         "both-bounded-max",
			leftEntries:  []protocol.FileSystemSandboxEntry{denyGlob},
			leftDepth:    &d2,
			rightEntries: []protocol.FileSystemSandboxEntry{denyGlob},
			rightDepth:   &d4,
			want:         &d4,
		},
		{
			// One side unbounded (deny glob, nil depth) => unbounded wins (nil).
			name:         "unbounded-wins",
			leftEntries:  []protocol.FileSystemSandboxEntry{denyGlob},
			leftDepth:    &d2,
			rightEntries: []protocol.FileSystemSandboxEntry{denyGlob},
			rightDepth:   nil,
			want:         nil,
		},
		{
			// Only left contributes a bounded depth.
			name:         "left-bounded-only",
			leftEntries:  []protocol.FileSystemSandboxEntry{denyGlob},
			leftDepth:    &d2,
			rightEntries: []protocol.FileSystemSandboxEntry{plainRead},
			rightDepth:   &d4, // ignored: no deny glob on the right
			want:         &d2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeGlobScanMaxDepth(tc.leftEntries, tc.leftDepth, tc.rightEntries, tc.rightDepth)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("depth = %d, want nil", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("depth = nil, want %d", *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Fatalf("depth = %d, want %d", *got, *tc.want)
			}
		})
	}
}

// TestRegexEscape verifies the regex meta-character escaping used by .sbpl path
// rules, mirroring regex_lite::escape.
func TestRegexEscape(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"a.b", `a\.b`},
		{"a+b*c?", `a\+b\*c\?`},
		{"[set]", `\[set\]`},
		{"(group)", `\(group\)`},
		{"a-b~c#d&e", `a\-b\~c\#d\&e`},
		{"$^|", `\$\^\|`},
		{"{n}", `\{n\}`},
		{`back\slash`, `back\\slash`},
		{"unicode_é", "unicode_é"},
	}
	for _, tc := range cases {
		if got := regexEscape(tc.in); got != tc.want {
			t.Errorf("regexEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestPathStartsWith verifies the component-aware prefix check.
func TestPathStartsWith(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/a/b", true},
		{"/a/bc", "/a/b", false}, // not a component boundary
		{"/x", "/", true},
		{"relative", "", false},
	}
	for _, tc := range cases {
		if got := pathStartsWith(tc.child, tc.parent); got != tc.want {
			t.Errorf("pathStartsWith(%q,%q) = %v, want %v", tc.child, tc.parent, got, tc.want)
		}
	}
}

// TestComponentCount verifies component counting matches Rust Path::components.
func TestComponentCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"/", 1},
		{"/a", 2},
		{"/a/b/c", 4},
		{"/a/b/", 3},
	}
	for _, tc := range cases {
		if got := componentCount(tc.in); got != tc.want {
			t.Errorf("componentCount(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
