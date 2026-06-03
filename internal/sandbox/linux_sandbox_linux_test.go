//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNSSetupFromSpec(t *testing.T) {
	tests := []struct {
		name           string
		mode           NetworkSeccompMode
		wantUnshareNet bool
	}{
		{"restricted unshares net", NetworkSeccompModeRestricted, true},
		{"proxy routed keeps net", NetworkSeccompModeProxyRouted, false},
		{"none keeps net", NetworkSeccompModeNone, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := NativeSandboxSpec{
				WritableRoots:      []string{"/work"},
				NetworkSeccompMode: tt.mode,
			}
			setup := nsSetupFromSpec(spec)
			if setup.unshareNet != tt.wantUnshareNet {
				t.Fatalf("unshareNet = %v, want %v", setup.unshareNet, tt.wantUnshareNet)
			}
			if len(setup.writableRoots) != 1 || setup.writableRoots[0] != "/work" {
				t.Fatalf("writableRoots not propagated: %v", setup.writableRoots)
			}
		})
	}
}

func TestNetworkSeccompSyscallsForGOARCH(t *testing.T) {
	sc, err := networkSeccompSyscallsForGOARCH()
	if err != nil {
		t.Skipf("unsupported test arch: %v", err)
	}
	// Syscall numbers must match the x/sys/unix constants for this arch.
	if sc.socket != uint32(unix.SYS_SOCKET) {
		t.Fatalf("socket syscall = %d, want %d", sc.socket, unix.SYS_SOCKET)
	}
	if sc.connect != uint32(unix.SYS_CONNECT) {
		t.Fatalf("connect syscall = %d, want %d", sc.connect, unix.SYS_CONNECT)
	}
	if sc.ptrace != uint32(unix.SYS_PTRACE) {
		t.Fatalf("ptrace syscall = %d, want %d", sc.ptrace, unix.SYS_PTRACE)
	}

	arch, err := auditArchForGOARCH()
	if err != nil {
		t.Fatalf("auditArchForGOARCH: %v", err)
	}
	// The program must be installable end-to-end with real syscall numbers and
	// terminate for a representative request.
	prog := buildNetworkSeccompProgram(NetworkSeccompModeRestricted, arch, sc)
	if len(prog) == 0 {
		t.Fatal("expected a non-empty seccomp program")
	}
	if got := runBPF(t, prog, seccompData{nr: sc.socket, arch: arch, arg0: afUNIX}); got != seccompRetAllow {
		t.Fatalf("AF_UNIX socket should be allowed, got 0x%x", got)
	}
}

func TestExistingPathsAndSortByDepth(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	abc := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(abc, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	missing := filepath.Join(dir, "missing")

	got := existingPaths([]string{abc, missing, a, a})
	// missing dropped, duplicates collapsed, order preserved.
	want := []string{abc, a}
	if len(got) != len(want) {
		t.Fatalf("existingPaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("existingPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	sorted := sortByDepth([]string{abc, a})
	if sorted[0] != a || sorted[1] != abc {
		t.Fatalf("sortByDepth = %v, want shallow-to-deep [%q %q]", sorted, a, abc)
	}
}

func TestLandlockAccessConstants(t *testing.T) {
	// Read rights must be a subset of the full rights, and full must add write.
	if landlockAccessFSRead&landlockAccessFSWrite != 0 {
		t.Fatal("read and write right sets should be disjoint")
	}
	if landlockAccessFSAll != landlockAccessFSRead|landlockAccessFSWrite {
		t.Fatal("all rights must be the union of read and write")
	}
	read, all := supportedLandlockAccessFS(1)
	if read == 0 || all == 0 || read == all {
		t.Fatalf("supportedLandlockAccessFS(1) = (0x%x, 0x%x), want non-zero read subset of all", read, all)
	}
	if r2, a2 := supportedLandlockAccessFS(0); r2 != 0 || a2 != 0 {
		t.Fatalf("supportedLandlockAccessFS(0) = (0x%x, 0x%x), want (0,0)", r2, a2)
	}
}
