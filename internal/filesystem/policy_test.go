package filesystem

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func rootEntry(access protocol.FileSystemAccessMode) protocol.FileSystemSandboxEntry {
	return protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}),
		Access: access,
	}
}

func pathEntry(path protocol.AbsolutePath, access protocol.FileSystemAccessMode) protocol.FileSystemSandboxEntry {
	return protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemPath(path),
		Access: access,
	}
}

func restricted(entries ...protocol.FileSystemSandboxEntry) protocol.FileSystemSandboxPolicy {
	return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindRestricted, Entries: entries}
}

func TestHasFullDiskWriteAccess(t *testing.T) {
	tests := []struct {
		name   string
		policy protocol.FileSystemSandboxPolicy
		want   bool
	}{
		{"unrestricted", protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindUnrestricted}, true},
		{"external sandbox", protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindExternalSandbox}, true},
		{"restricted empty", restricted(), false},
		{"restricted root write", restricted(rootEntry(protocol.FileSystemAccessModeWrite)), true},
		{"restricted root read only", restricted(rootEntry(protocol.FileSystemAccessModeRead)), false},
		{
			"root write narrowed by path deny",
			restricted(rootEntry(protocol.FileSystemAccessModeWrite), pathEntry("/secret", protocol.FileSystemAccessModeDeny)),
			false,
		},
		{
			"root write with shadowed read override stays full",
			restricted(
				rootEntry(protocol.FileSystemAccessModeWrite),
				pathEntry("/x", protocol.FileSystemAccessModeRead),
				pathEntry("/x", protocol.FileSystemAccessModeWrite),
			),
			true,
		},
		{
			"root write narrowed by glob deny",
			restricted(rootEntry(protocol.FileSystemAccessModeWrite), protocol.FileSystemSandboxEntry{
				Path:   protocol.NewFileSystemGlobPattern("*.env"),
				Access: protocol.FileSystemAccessModeDeny,
			}),
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasFullDiskWriteAccess(tc.policy); got != tc.want {
				t.Fatalf("hasFullDiskWriteAccess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldRunInSandbox(t *testing.T) {
	tests := []struct {
		name    string
		profile protocol.PermissionProfile
		want    bool
	}{
		{
			name:    "disabled profile is unrestricted",
			profile: protocol.NewDisabledPermissionProfile(),
			want:    false,
		},
		{
			name:    "external profile is external sandbox",
			profile: protocol.NewExternalPermissionProfile(protocol.NetworkSandboxPolicyRestricted),
			want:    false,
		},
		{
			name: "managed unrestricted",
			profile: protocol.NewManagedPermissionProfile(
				protocol.NewUnrestrictedManagedFileSystem(),
				protocol.NetworkSandboxPolicyRestricted,
			),
			want: false,
		},
		{
			name: "managed restricted empty needs sandbox",
			profile: protocol.NewManagedPermissionProfile(
				protocol.NewRestrictedManagedFileSystem(nil, nil),
				protocol.NetworkSandboxPolicyRestricted,
			),
			want: true,
		},
		{
			name: "managed restricted full write does not need sandbox",
			profile: protocol.NewManagedPermissionProfile(
				protocol.NewRestrictedManagedFileSystem([]protocol.FileSystemSandboxEntry{rootEntry(protocol.FileSystemAccessModeWrite)}, nil),
				protocol.NetworkSandboxPolicyRestricted,
			),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := FromPermissionProfile(tc.profile)
			if got := ctx.ShouldRunInSandbox(); got != tc.want {
				t.Fatalf("ShouldRunInSandbox = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasCwdDependentPermissionsAndDrop(t *testing.T) {
	relGlob := protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemGlobPattern("src/*.rs"),
		Access: protocol.FileSystemAccessModeDeny,
	}
	projectRoots := protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemSpecialPath(protocol.NewProjectRootsSpecialPath(nil)),
		Access: protocol.FileSystemAccessModeWrite,
	}
	absPath := pathEntry("/abs", protocol.FileSystemAccessModeWrite)

	tests := []struct {
		name      string
		entries   []protocol.FileSystemSandboxEntry
		dependent bool
	}{
		{"relative glob is cwd dependent", []protocol.FileSystemSandboxEntry{relGlob}, true},
		{"project roots is cwd dependent", []protocol.FileSystemSandboxEntry{projectRoots}, true},
		{"absolute path is not cwd dependent", []protocol.FileSystemSandboxEntry{absPath}, false},
		{
			"absolute glob is not cwd dependent",
			[]protocol.FileSystemSandboxEntry{{
				Path:   protocol.NewFileSystemGlobPattern("/etc/*.conf"),
				Access: protocol.FileSystemAccessModeDeny,
			}},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			profile := protocol.NewManagedPermissionProfile(
				protocol.NewRestrictedManagedFileSystem(tc.entries, nil),
				protocol.NetworkSandboxPolicyRestricted,
			)
			cwd := protocol.AbsolutePath("/work")
			ctx := FromPermissionProfileWithCwd(profile, cwd)
			if got := ctx.HasCwdDependentPermissions(); got != tc.dependent {
				t.Fatalf("HasCwdDependentPermissions = %v, want %v", got, tc.dependent)
			}
			dropped := ctx.DropCwdIfUnused()
			if tc.dependent {
				if dropped.Cwd == nil {
					t.Fatal("cwd should be retained for cwd-dependent policy")
				}
			} else {
				if dropped.Cwd != nil {
					t.Fatal("cwd should be dropped for non-cwd-dependent policy")
				}
				// Original context must be unchanged (immutability).
				if ctx.Cwd == nil {
					t.Fatal("DropCwdIfUnused mutated the receiver")
				}
			}
		})
	}
}
