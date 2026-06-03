package sandbox

import "testing"

// seccompData is the subset of struct seccomp_data the test VM models: the
// syscall number, the architecture, and the low 32 bits of args[0].
type seccompData struct {
	nr   uint32
	arch uint32
	arg0 uint32
}

// runBPF is a minimal classic-BPF interpreter sufficient to evaluate the
// network seccomp programs this package emits. It supports the LD|W|ABS, JMP|JEQ,
// and RET|K instructions used by the builder and returns the SECCOMP_RET_* action
// the program yields for data. It fails the test on malformed jumps so encoding
// bugs surface deterministically.
func runBPF(t *testing.T, prog []sockFilterInstr, data seccompData) uint32 {
	t.Helper()
	const maxSteps = 10000
	var a uint32
	pc := 0
	for steps := 0; steps < maxSteps; steps++ {
		if pc < 0 || pc >= len(prog) {
			t.Fatalf("bpf pc out of range: %d (len=%d)", pc, len(prog))
		}
		ins := prog[pc]
		switch ins.Code {
		case bpfLD | bpfW | bpfABS:
			switch ins.K {
			case seccompDataNROffset:
				a = data.nr
			case seccompDataArchOffset:
				a = data.arch
			case seccompDataArg0LowOffset:
				a = data.arg0
			default:
				t.Fatalf("bpf LD from unexpected offset %d", ins.K)
			}
			pc++
		case bpfJMP | bpfJEQ | bpfK:
			if a == ins.K {
				pc += 1 + int(ins.Jt)
			} else {
				pc += 1 + int(ins.Jf)
			}
		case bpfRET | bpfK:
			return ins.K
		default:
			t.Fatalf("bpf unsupported opcode 0x%x at pc=%d", ins.Code, pc)
		}
	}
	t.Fatalf("bpf program did not terminate within %d steps", maxSteps)
	return 0
}

// testSyscalls returns a syscall-number table with distinct synthetic values so
// the VM can tell each branch apart regardless of real architecture.
func testSyscalls() networkSeccompSyscalls {
	return networkSeccompSyscalls{
		ptrace:          100,
		processVMReadv:  101,
		processVMWritev: 102,
		ioUringSetup:    103,
		ioUringEnter:    104,
		ioUringRegister: 105,
		connect:         110,
		accept:          111,
		accept4:         112,
		bind:            113,
		listen:          114,
		getpeername:     115,
		getsockname:     116,
		shutdown:        117,
		sendto:          118,
		sendmmsg:        119,
		recvmmsg:        120,
		getsockopt:      121,
		setsockopt:      122,
		socket:          200,
		socketpair:      201,
	}
}

func TestBuildNetworkSeccompProgram_None(t *testing.T) {
	if prog := buildNetworkSeccompProgram(NetworkSeccompModeNone, auditArchX8664, testSyscalls()); prog != nil {
		t.Fatalf("expected nil program for None mode, got %d instructions", len(prog))
	}
}

func TestNetworkSeccomp_ArchMismatchKills(t *testing.T) {
	sc := testSyscalls()
	prog := buildNetworkSeccompProgram(NetworkSeccompModeRestricted, auditArchX8664, sc)
	got := runBPF(t, prog, seccompData{nr: sc.socket, arch: auditArchAARCH64, arg0: afUNIX})
	if got != seccompRetKillProc {
		t.Fatalf("arch mismatch: got action 0x%x, want kill 0x%x", got, seccompRetKillProc)
	}
}

func TestNetworkSeccomp_RestrictedMode(t *testing.T) {
	sc := testSyscalls()
	prog := buildNetworkSeccompProgram(NetworkSeccompModeRestricted, auditArchX8664, sc)

	tests := []struct {
		name string
		data seccompData
		want uint32
	}{
		{"ptrace denied", seccompData{nr: sc.ptrace, arch: auditArchX8664}, seccompRetErrno(errnoEPERM)},
		{"connect denied", seccompData{nr: sc.connect, arch: auditArchX8664}, seccompRetErrno(errnoEPERM)},
		{"bind denied", seccompData{nr: sc.bind, arch: auditArchX8664}, seccompRetErrno(errnoEPERM)},
		{"setsockopt denied", seccompData{nr: sc.setsockopt, arch: auditArchX8664}, seccompRetErrno(errnoEPERM)},
		{"recvmmsg denied", seccompData{nr: sc.recvmmsg, arch: auditArchX8664}, seccompRetErrno(errnoEPERM)},
		{"io_uring_setup denied", seccompData{nr: sc.ioUringSetup, arch: auditArchX8664}, seccompRetErrno(errnoEPERM)},
		{"socket AF_UNIX allowed", seccompData{nr: sc.socket, arch: auditArchX8664, arg0: afUNIX}, seccompRetAllow},
		{"socket AF_INET denied", seccompData{nr: sc.socket, arch: auditArchX8664, arg0: afINET}, seccompRetErrno(errnoEPERM)},
		{"socketpair AF_UNIX allowed", seccompData{nr: sc.socketpair, arch: auditArchX8664, arg0: afUNIX}, seccompRetAllow},
		{"socketpair AF_INET denied", seccompData{nr: sc.socketpair, arch: auditArchX8664, arg0: afINET}, seccompRetErrno(errnoEPERM)},
		{"unrelated syscall allowed", seccompData{nr: 9999, arch: auditArchX8664}, seccompRetAllow},
		// recvfrom is intentionally NOT in the denied set (matches Rust comment).
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runBPF(t, prog, tt.data); got != tt.want {
				t.Fatalf("got action 0x%x, want 0x%x", got, tt.want)
			}
		})
	}
}

func TestNetworkSeccomp_ProxyRoutedMode(t *testing.T) {
	sc := testSyscalls()
	prog := buildNetworkSeccompProgram(NetworkSeccompModeProxyRouted, auditArchAARCH64, sc)

	tests := []struct {
		name string
		data seccompData
		want uint32
	}{
		// In proxy-routed mode connect/bind/etc are NOT unconditionally denied.
		{"connect allowed", seccompData{nr: sc.connect, arch: auditArchAARCH64}, seccompRetAllow},
		{"ptrace still denied", seccompData{nr: sc.ptrace, arch: auditArchAARCH64}, seccompRetErrno(errnoEPERM)},
		{"socket AF_INET allowed", seccompData{nr: sc.socket, arch: auditArchAARCH64, arg0: afINET}, seccompRetAllow},
		{"socket AF_INET6 allowed", seccompData{nr: sc.socket, arch: auditArchAARCH64, arg0: afINET6}, seccompRetAllow},
		{"socket AF_UNIX denied", seccompData{nr: sc.socket, arch: auditArchAARCH64, arg0: afUNIX}, seccompRetErrno(errnoEPERM)},
		{"socket AF_PACKET denied", seccompData{nr: sc.socket, arch: auditArchAARCH64, arg0: 17}, seccompRetErrno(errnoEPERM)},
		{"socketpair AF_UNIX denied", seccompData{nr: sc.socketpair, arch: auditArchAARCH64, arg0: afUNIX}, seccompRetErrno(errnoEPERM)},
		{"socketpair AF_INET allowed", seccompData{nr: sc.socketpair, arch: auditArchAARCH64, arg0: afINET}, seccompRetAllow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runBPF(t, prog, tt.data); got != tt.want {
				t.Fatalf("got action 0x%x, want 0x%x", got, tt.want)
			}
		})
	}
}

func TestSeccompRetErrno(t *testing.T) {
	got := seccompRetErrno(errnoEPERM)
	if want := uint32(0x00050001); got != want {
		t.Fatalf("seccompRetErrno(EPERM) = 0x%x, want 0x%x", got, want)
	}
}
