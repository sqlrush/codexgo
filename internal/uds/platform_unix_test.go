//go:build unix

package uds

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestPreparePrivateSocketDirectorySetsExistingPermissionsToOwnerOnly ports the
// Rust prepare_private_socket_directory_sets_existing_permissions_to_owner_only
// test: an existing directory with insecure (0755) or unusable owner-only
// (0600) permissions is forced to exactly 0700.
func TestPreparePrivateSocketDirectorySetsExistingPermissionsToOwnerOnly(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	tests := []struct {
		name string
		mode fs.FileMode
	}{
		{name: "insecure-0755", mode: 0o755},
		{name: "unusable-owner-only-0600", mode: 0o600},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			socketDir := filepath.Join(base, "app-server-control-"+tc.name)
			if err := os.Mkdir(socketDir, 0o700); err != nil {
				t.Fatalf("socket dir should be created: %v", err)
			}
			if err := os.Chmod(socketDir, tc.mode); err != nil {
				t.Fatalf("socket dir permissions should be changed: %v", err)
			}

			if err := PreparePrivateSocketDirectory(socketDir); err != nil {
				t.Fatalf("socket dir permissions should be set exactly: %v", err)
			}

			info, err := os.Stat(socketDir)
			if err != nil {
				t.Fatalf("socket dir metadata: %v", err)
			}
			if got := info.Mode().Perm() & 0o777; got != 0o700 {
				t.Fatalf("mode & 0o777 = %#o, want 0o700", got)
			}
		})
	}
}

// TestPreparePrivateSocketDirectoryCreatesWithOwnerOnlyMode verifies that a
// freshly created directory is owner-only regardless of the process umask,
// matching the Rust DirBuilder.mode(0o700) behavior.
func TestPreparePrivateSocketDirectoryCreatesWithOwnerOnlyMode(t *testing.T) {
	socketDir := filepath.Join(t.TempDir(), "fresh")
	if err := PreparePrivateSocketDirectory(socketDir); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	info, err := os.Stat(socketDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm() & 0o777; got != 0o700 {
		t.Fatalf("mode & 0o777 = %#o, want 0o700", got)
	}
}

// TestPreparePrivateSocketDirectoryRejectsNonDirectory verifies that an existing
// non-directory path is rejected, mirroring the Rust "exists and is not a
// directory" error branch.
func TestPreparePrivateSocketDirectoryRejectsNonDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := PreparePrivateSocketDirectory(path)
	if err == nil {
		t.Fatalf("expected error for non-directory path")
	}
}
