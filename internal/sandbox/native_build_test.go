package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// restrictedWorkspacePolicy returns a Restricted policy that grants read to the
// filesystem root and write to the workspace root (cwd).
func restrictedWorkspacePolicy() protocol.FileSystemSandboxPolicy {
	return protocol.FileSystemSandboxPolicy{
		Kind: protocol.FileSystemSandboxKindRestricted,
		Entries: []protocol.FileSystemSandboxEntry{
			{
				Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}),
				Access: protocol.FileSystemAccessModeRead,
			},
			{
				Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindProjectRoots}),
				Access: protocol.FileSystemAccessModeWrite,
			},
		},
	}
}

func TestBuildLinuxSandboxSpec_WorkspaceWrite(t *testing.T) {
	workspace := t.TempDir()
	// Resolve symlinks so the expected writable root matches the spec's
	// resolved-and-deduped form (macOS /var -> /private/var, etc).
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(resolved, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	req := SpawnRequest{
		Command:                 []string{"/bin/echo", "hi"},
		Cwd:                     resolved,
		FileSystemSandboxPolicy: restrictedWorkspacePolicy(),
		NetworkSandboxPolicy:    protocol.NetworkSandboxPolicyRestricted,
		SandboxPolicyCwd:        resolved,
	}

	spec := buildLinuxSandboxSpec(req)

	if spec.FullDiskWriteAccess {
		t.Fatal("expected restricted write access")
	}
	if !spec.FullDiskReadAccess {
		t.Fatal("expected full disk read access (root read entry)")
	}
	if !containsString(spec.WritableRoots, resolved) {
		t.Fatalf("writable roots %v should contain workspace %q", spec.WritableRoots, resolved)
	}
	gitPath := filepath.Join(resolved, ".git")
	if !containsString(spec.ProtectedSubpaths, gitPath) {
		t.Fatalf("protected subpaths %v should contain %q", spec.ProtectedSubpaths, gitPath)
	}
	if spec.NetworkSeccompMode != NetworkSeccompModeRestricted {
		t.Fatalf("network mode = %q, want restricted", spec.NetworkSeccompMode)
	}
	if len(spec.Command) != 2 || spec.Command[0] != "/bin/echo" {
		t.Fatalf("unexpected command: %v", spec.Command)
	}
}

func TestBuildLinuxSandboxSpec_FullAccess(t *testing.T) {
	req := SpawnRequest{
		Command:                 []string{"/bin/true"},
		Cwd:                     "/",
		FileSystemSandboxPolicy: protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindUnrestricted},
		NetworkSandboxPolicy:    protocol.NetworkSandboxPolicyEnabled,
	}
	spec := buildLinuxSandboxSpec(req)

	if !spec.FullDiskWriteAccess || !spec.FullDiskReadAccess {
		t.Fatal("unrestricted policy should grant full disk read+write")
	}
	if len(spec.WritableRoots) != 0 {
		t.Fatalf("full write access should not list writable roots, got %v", spec.WritableRoots)
	}
	if spec.NetworkSeccompMode != NetworkSeccompModeNone {
		t.Fatalf("enabled network without proxy should install no filter, got %q", spec.NetworkSeccompMode)
	}
}

func TestBuildWindowsSandboxSpec_DenyReadAndLevel(t *testing.T) {
	workspace := t.TempDir()
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	denied := filepath.Join(resolved, "secret")
	if err := os.MkdirAll(denied, 0o755); err != nil {
		t.Fatalf("mkdir secret: %v", err)
	}

	policy := restrictedWorkspacePolicy()
	policy.Entries = append(policy.Entries, protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemPath(protocol.AbsolutePath(denied)),
		Access: protocol.FileSystemAccessModeDeny,
	})

	req := SpawnRequest{
		Command:                 []string{"cmd.exe", "/c", "dir"},
		Cwd:                     resolved,
		FileSystemSandboxPolicy: policy,
		NetworkSandboxPolicy:    protocol.NetworkSandboxPolicyRestricted,
		SandboxPolicyCwd:        resolved,
	}

	spec := buildWindowsSandboxSpec(req, protocol.WindowsSandboxLevelRestrictedToken)

	if spec.WindowsSandboxLevel != protocol.WindowsSandboxLevelRestrictedToken {
		t.Fatalf("windows level = %q, want restricted-token", spec.WindowsSandboxLevel)
	}
	if spec.NetworkSeccompMode != NetworkSeccompModeNone {
		t.Fatalf("windows spec should clear network seccomp mode, got %q", spec.NetworkSeccompMode)
	}
	if !containsString(spec.DenyReadPaths, denied) {
		t.Fatalf("deny read paths %v should contain %q", spec.DenyReadPaths, denied)
	}
}
