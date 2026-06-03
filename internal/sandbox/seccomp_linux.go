//go:build linux

package sandbox

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// errUnsupportedSeccompArch is returned when the running architecture has no
// network seccomp filter mapping. Mirrors the unimplemented!() arm in the Rust
// filter, which only supports x86_64 and aarch64.
var errUnsupportedSeccompArch = fmt.Errorf("sandbox: network seccomp filter unsupported on arch %q", runtime.GOARCH)

// auditArchForGOARCH returns the AUDIT_ARCH_* value used to gate the seccomp
// filter to the running architecture.
func auditArchForGOARCH() (uint32, error) {
	switch runtime.GOARCH {
	case "amd64":
		return auditArchX8664, nil
	case "arm64":
		return auditArchAARCH64, nil
	default:
		return 0, errUnsupportedSeccompArch
	}
}

// networkSeccompSyscallsForGOARCH returns the syscall-number table for the
// running architecture. Numbers come from golang.org/x/sys/unix, which is
// arch-specialized at build time, so this stays cgo-free.
func networkSeccompSyscallsForGOARCH() (networkSeccompSyscalls, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return networkSeccompSyscalls{
			ptrace:          uint32(unix.SYS_PTRACE),
			processVMReadv:  uint32(unix.SYS_PROCESS_VM_READV),
			processVMWritev: uint32(unix.SYS_PROCESS_VM_WRITEV),
			ioUringSetup:    uint32(unix.SYS_IO_URING_SETUP),
			ioUringEnter:    uint32(unix.SYS_IO_URING_ENTER),
			ioUringRegister: uint32(unix.SYS_IO_URING_REGISTER),
			connect:         uint32(unix.SYS_CONNECT),
			accept:          uint32(unix.SYS_ACCEPT),
			accept4:         uint32(unix.SYS_ACCEPT4),
			bind:            uint32(unix.SYS_BIND),
			listen:          uint32(unix.SYS_LISTEN),
			getpeername:     uint32(unix.SYS_GETPEERNAME),
			getsockname:     uint32(unix.SYS_GETSOCKNAME),
			shutdown:        uint32(unix.SYS_SHUTDOWN),
			sendto:          uint32(unix.SYS_SENDTO),
			sendmmsg:        uint32(unix.SYS_SENDMMSG),
			recvmmsg:        uint32(unix.SYS_RECVMMSG),
			getsockopt:      uint32(unix.SYS_GETSOCKOPT),
			setsockopt:      uint32(unix.SYS_SETSOCKOPT),
			socket:          uint32(unix.SYS_SOCKET),
			socketpair:      uint32(unix.SYS_SOCKETPAIR),
		}, nil
	default:
		return networkSeccompSyscalls{}, errUnsupportedSeccompArch
	}
}

// installNetworkSeccompFilter installs the classic-BPF network filter on the
// current thread for the given mode. A mode of NetworkSeccompModeNone is a no-op.
// The caller must have set PR_SET_NO_NEW_PRIVS first.
//
// Mirrors install_network_seccomp_filter_on_current_thread.
func installNetworkSeccompFilter(mode NetworkSeccompMode) error {
	if mode == NetworkSeccompModeNone {
		return nil
	}
	auditArch, err := auditArchForGOARCH()
	if err != nil {
		return err
	}
	syscalls, err := networkSeccompSyscallsForGOARCH()
	if err != nil {
		return err
	}

	prog := buildNetworkSeccompProgram(mode, auditArch, syscalls)
	if len(prog) == 0 {
		return nil
	}
	if len(prog) > 0xffff {
		return fmt.Errorf("sandbox: seccomp program too long (%d instructions)", len(prog))
	}

	filters := make([]unix.SockFilter, len(prog))
	for i, ins := range prog {
		filters[i] = unix.SockFilter{Code: ins.Code, Jt: ins.Jt, Jf: ins.Jf, K: ins.K}
	}
	fprog := unix.SockFprog{
		Len:    uint16(len(filters)),
		Filter: &filters[0],
	}

	_, _, errno := unix.Syscall(
		unix.SYS_SECCOMP,
		uintptr(unix.SECCOMP_SET_MODE_FILTER),
		0,
		uintptr(unsafe.Pointer(&fprog)),
	)
	if errno != 0 {
		// Fall back to the legacy prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, ...)
		// path on kernels that lack the seccomp() syscall.
		if errno == unix.ENOSYS {
			if perr := unix.Prctl(
				unix.PR_SET_SECCOMP,
				uintptr(unix.SECCOMP_MODE_FILTER),
				uintptr(unsafe.Pointer(&fprog)),
				0, 0,
			); perr != nil {
				return fmt.Errorf("sandbox: prctl(PR_SET_SECCOMP): %w", perr)
			}
			return nil
		}
		return fmt.Errorf("sandbox: seccomp(SET_MODE_FILTER): %w", errno)
	}
	return nil
}

// setNoNewPrivs enables PR_SET_NO_NEW_PRIVS, required before installing a seccomp
// filter or a Landlock ruleset. Mirrors set_no_new_privs.
func setNoNewPrivs() error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("sandbox: prctl(PR_SET_NO_NEW_PRIVS): %w", err)
	}
	return nil
}
