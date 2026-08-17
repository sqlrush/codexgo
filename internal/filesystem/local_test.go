package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestLocalFileSystemUnsandboxedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fsys := NewLocalFileSystem()
	ctx := context.Background()

	file := filepath.Join(dir, "f.txt")
	if err := fsys.WriteFile(ctx, ap(file), []byte("local"), nil); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := fsys.ReadFile(ctx, ap(file), nil)
	if err != nil || string(got) != "local" {
		t.Fatalf("ReadFile = %q (err %v)", got, err)
	}
}

func TestLocalFileSystemRejectsSandboxRequiringContext(t *testing.T) {
	dir := t.TempDir()
	fsys := NewLocalFileSystem()
	ctx := context.Background()

	// A restricted-empty managed profile requires a platform sandbox, which is
	// unavailable in this port: the operation must fail with InvalidInput.
	profile := protocol.NewManagedPermissionProfile(
		protocol.NewRestrictedManagedFileSystem(nil, nil),
		protocol.NetworkSandboxPolicyRestricted,
	)
	sandbox := FromPermissionProfile(profile)
	if !sandbox.ShouldRunInSandbox() {
		t.Fatal("test precondition: profile should require sandbox")
	}

	_, err := fsys.ReadFile(ctx, ap(filepath.Join(dir, "f.txt")), &sandbox)
	if err == nil {
		t.Fatal("expected sandbox-required rejection")
	}
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidInputError, got %T: %v", err, err)
	}
}

func TestLocalFileSystemUnsandboxedNonSandboxedContextAllowed(t *testing.T) {
	dir := t.TempDir()
	fsys := NewLocalFileSystem()
	ctx := context.Background()

	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A disabled profile is unrestricted, so it does not require a sandbox and
	// the operation is routed to the unsandboxed implementation.
	sandbox := FromPermissionProfile(protocol.NewDisabledPermissionProfile())
	if sandbox.ShouldRunInSandbox() {
		t.Fatal("disabled profile should not require sandbox")
	}
	got, err := fsys.ReadFile(ctx, ap(file), &sandbox)
	if err != nil {
		t.Fatalf("ReadFile with non-sandboxed context: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("ReadFile = %q", got)
	}
}

func TestUnsandboxedRejectsPlatformSandboxContext(t *testing.T) {
	dir := t.TempDir()
	fsys := NewUnsandboxedFileSystem()
	ctx := context.Background()

	profile := protocol.NewManagedPermissionProfile(
		protocol.NewRestrictedManagedFileSystem(nil, nil),
		protocol.NetworkSandboxPolicyRestricted,
	)
	sandbox := FromPermissionProfile(profile)
	err := fsys.WriteFile(ctx, ap(filepath.Join(dir, "x")), []byte("y"), &sandbox)
	if err == nil {
		t.Fatal("expected platform sandbox rejection")
	}
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidInputError, got %T", err)
	}
}

func TestLocalFSHelper(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	file := filepath.Join(dir, "f.txt")
	if err := LocalFS().WriteFile(ctx, ap(file), []byte("z"), nil); err != nil {
		t.Fatalf("LocalFS WriteFile: %v", err)
	}
}
