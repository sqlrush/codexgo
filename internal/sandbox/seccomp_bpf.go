package sandbox

// This file builds the classic-BPF program that backs the Linux network seccomp
// filter. It is deliberately free of build tags and of any Linux-only imports so
// the program-construction logic is unit-testable on any host with -race. The
// Linux backend (seccomp_linux.go) installs the resulting program via
// seccomp(SECCOMP_SET_MODE_FILTER).
//
// The filter mirrors install_network_seccomp_filter_on_current_thread in
// linux-sandbox/src/landlock.rs: a default-allow program that returns EPERM for
// a fixed set of syscalls and, for socket()/socketpair(), inspects the first
// argument (the address family) to allow or deny by family.

// sockFilterInstr is a single classic-BPF instruction. It mirrors the layout of
// struct sock_filter (and unix.SockFilter) without depending on a Linux-only
// type, so the builder compiles and tests everywhere.
type sockFilterInstr struct {
	Code uint16
	Jt   uint8
	Jf   uint8
	K    uint32
}

// Classic BPF opcodes and seccomp_data accessors. These are stable kernel ABI
// values, reproduced here so the builder needs no Linux-only constants.
const (
	bpfLD  = 0x00
	bpfJMP = 0x05
	bpfRET = 0x06

	bpfW   = 0x00 // word (32-bit) operand size
	bpfABS = 0x20 // absolute load from the packet (seccomp_data)
	bpfK   = 0x00 // immediate operand in K

	bpfJEQ = 0x10 // jump if A == K
	bpfJGE = 0x20 // jump if A >= K

	// Offsets into struct seccomp_data.
	seccompDataNROffset      = 0  // int nr (syscall number)
	seccompDataArchOffset    = 4  // __u32 arch
	seccompDataArg0LowOffset = 16 // low 32 bits of args[0]

	// seccomp filter return actions (SECCOMP_RET_*).
	seccompRetAllow    = 0x7fff0000
	seccompRetErrnoTop = 0x00050000 // SECCOMP_RET_ERRNO base
	seccompRetKillProc = 0x80000000

	// AUDIT_ARCH_* values used to gate the filter to the running architecture so
	// a foreign-architecture call cannot bypass argument checks.
	auditArchX8664   = 0xC000003E
	auditArchAARCH64 = 0xC00000B7

	// Address families inspected for socket()/socketpair().
	afUNIX  = 1
	afINET  = 2
	afINET6 = 10

	errnoEPERM = 1
)

// networkSeccompSyscalls holds the architecture-specific syscall numbers the
// filter references. Mirrors the libc::SYS_* constants used in the Rust filter.
type networkSeccompSyscalls struct {
	ptrace          uint32
	processVMReadv  uint32
	processVMWritev uint32
	ioUringSetup    uint32
	ioUringEnter    uint32
	ioUringRegister uint32
	connect         uint32
	accept          uint32
	accept4         uint32
	bind            uint32
	listen          uint32
	getpeername     uint32
	getsockname     uint32
	shutdown        uint32
	sendto          uint32
	sendmmsg        uint32
	recvmmsg        uint32
	getsockopt      uint32
	setsockopt      uint32
	socket          uint32
	socketpair      uint32
}

// buildNetworkSeccompProgram assembles the classic-BPF program for the given
// network mode, architecture audit value, and syscall numbers. It returns nil
// when mode is NetworkSeccompModeNone (no filter). The program is a flat list of
// instructions evaluated top to bottom.
func buildNetworkSeccompProgram(
	mode NetworkSeccompMode,
	auditArch uint32,
	sc networkSeccompSyscalls,
) []sockFilterInstr {
	if mode == NetworkSeccompModeNone {
		return nil
	}

	b := &bpfBuilder{}

	// 1. Validate architecture: load seccomp_data.arch; if it does not match the
	//    expected audit arch, kill the process. This prevents x32/foreign-ABI
	//    syscall-number confusion from bypassing the argument checks below.
	b.stmt(bpfLD|bpfW|bpfABS, seccompDataArchOffset)
	b.jump(bpfJMP|bpfJEQ|bpfK, auditArch, 1, 0)
	b.stmt(bpfRET|bpfK, seccompRetKillProc)

	// 2. Load the syscall number for the dispatch table below.
	b.stmt(bpfLD|bpfW|bpfABS, seccompDataNROffset)

	// 3. Unconditionally denied syscalls (return EPERM), shared by both modes.
	denied := []uint32{
		sc.ptrace, sc.processVMReadv, sc.processVMWritev,
		sc.ioUringSetup, sc.ioUringEnter, sc.ioUringRegister,
	}
	if mode == NetworkSeccompModeRestricted {
		denied = append(denied,
			sc.connect, sc.accept, sc.accept4, sc.bind, sc.listen,
			sc.getpeername, sc.getsockname, sc.shutdown, sc.sendto,
			sc.sendmmsg, sc.recvmmsg, sc.getsockopt, sc.setsockopt,
		)
	}
	for _, nr := range denied {
		// if nr == A: return EPERM (skip next instr on no match).
		b.jump(bpfJMP|bpfJEQ|bpfK, nr, 0, 1)
		b.stmt(bpfRET|bpfK, seccompRetErrno(errnoEPERM))
	}

	// 4. Family-gated socket()/socketpair() handling. The address family is the
	//    first argument; load its low 32 bits before each check, then re-load the
	//    syscall number is not needed because we branch to dedicated blocks.
	switch mode {
	case NetworkSeccompModeRestricted:
		// Allow only AF_UNIX; deny every other family with EPERM.
		b.emitSocketFamilyGate(sc.socket, allowFamilyEqual, afUNIX)
		b.emitSocketFamilyGate(sc.socketpair, allowFamilyEqual, afUNIX)
	case NetworkSeccompModeProxyRouted:
		// Allow AF_INET/AF_INET6 (to reach the proxy bridge); deny AF_UNIX and
		// everything else.
		b.emitSocketFamilyGate(sc.socket, allowFamilyInetOnly, 0)
		b.emitSocketFamilyGate(sc.socketpair, denyFamilyUnix, 0)
	}

	// 5. Default: allow.
	b.stmt(bpfRET|bpfK, seccompRetAllow)

	return b.instrs
}

// socketGateKind selects how a socket family gate decides allow vs deny.
type socketGateKind int

const (
	// allowFamilyEqual allows when args[0] == family, denies otherwise.
	allowFamilyEqual socketGateKind = iota
	// allowFamilyInetOnly allows when args[0] is AF_INET or AF_INET6, denies
	// otherwise.
	allowFamilyInetOnly
	// denyFamilyUnix denies when args[0] == AF_UNIX, allows otherwise.
	denyFamilyUnix
)

// bpfBuilder accumulates instructions and resolves relative jumps. Classic BPF
// jumps are forward-only and relative; we compute offsets as we emit because the
// gate blocks have fixed, small sizes.
type bpfBuilder struct {
	instrs []sockFilterInstr
}

func (b *bpfBuilder) stmt(code uint16, k uint32) {
	b.instrs = append(b.instrs, sockFilterInstr{Code: code, K: k})
}

func (b *bpfBuilder) jump(code uint16, k uint32, jt, jf uint8) {
	b.instrs = append(b.instrs, sockFilterInstr{Code: code, K: k, Jt: jt, Jf: jf})
}

// emitSocketFamilyGate emits a block that, when the current syscall number (in
// A) equals nr, inspects the address-family argument and returns either allow or
// EPERM; when nr does not match, control falls through to the next block. Each
// block re-loads the syscall number at the end so subsequent blocks compare
// against the correct value.
func (b *bpfBuilder) emitSocketFamilyGate(nr uint32, kind socketGateKind, family uint32) {
	switch kind {
	case allowFamilyEqual:
		b.emitAllowEqualBlock(nr, family)
	case allowFamilyInetOnly:
		b.emitAllowInetBlock(nr)
	case denyFamilyUnix:
		b.emitDenyUnixBlock(nr)
	}
}

// emitAllowEqualBlock: if syscall==nr, allow iff args[0]==family else EPERM;
// otherwise fall through. Block layout (relative jumps computed by length):
//
//	[0] JEQ nr ? continue : skip(len-1)
//	[1] LD args0
//	[2] JEQ family ? allow(skip to allow) : EPERM
//	[3] RET ALLOW
//	[4] RET EPERM
//	[5] LD nr (restore A for next block)
func (b *bpfBuilder) emitAllowEqualBlock(nr, family uint32) {
	b.jump(bpfJMP|bpfJEQ|bpfK, nr, 0, 5) // not nr -> skip 5 (to the restore-LD)
	b.stmt(bpfLD|bpfW|bpfABS, seccompDataArg0LowOffset)
	b.jump(bpfJMP|bpfJEQ|bpfK, family, 1, 0) // family match -> +1 (RET ALLOW); else next (RET EPERM)
	b.stmt(bpfRET|bpfK, seccompRetErrno(errnoEPERM))
	b.stmt(bpfRET|bpfK, seccompRetAllow)
	b.stmt(bpfLD|bpfW|bpfABS, seccompDataNROffset) // restore A = nr for next block
}

// emitAllowInetBlock: if syscall==nr, allow iff args[0]∈{AF_INET,AF_INET6} else
// EPERM; otherwise fall through.
//
//	[0] JEQ nr ? continue : skip to restore-LD
//	[1] LD args0
//	[2] JEQ AF_INET  -> RET ALLOW
//	[3] JEQ AF_INET6 -> RET ALLOW
//	[4] RET EPERM
//	[5] RET ALLOW
//	[6] LD nr (restore)
func (b *bpfBuilder) emitAllowInetBlock(nr uint32) {
	b.jump(bpfJMP|bpfJEQ|bpfK, nr, 0, 6) // not nr -> skip to restore-LD
	b.stmt(bpfLD|bpfW|bpfABS, seccompDataArg0LowOffset)
	b.jump(bpfJMP|bpfJEQ|bpfK, afINET, 2, 0)  // AF_INET -> +2 (RET ALLOW)
	b.jump(bpfJMP|bpfJEQ|bpfK, afINET6, 1, 0) // AF_INET6 -> +1 (RET ALLOW)
	b.stmt(bpfRET|bpfK, seccompRetErrno(errnoEPERM))
	b.stmt(bpfRET|bpfK, seccompRetAllow)
	b.stmt(bpfLD|bpfW|bpfABS, seccompDataNROffset) // restore
}

// emitDenyUnixBlock: if syscall==nr, deny (EPERM) when args[0]==AF_UNIX else
// allow; otherwise fall through.
//
//	[0] JEQ nr ? continue : skip to restore-LD
//	[1] LD args0
//	[2] JEQ AF_UNIX -> RET EPERM ; else RET ALLOW
//	[3] RET ALLOW
//	[4] RET EPERM
//	[5] LD nr (restore)
func (b *bpfBuilder) emitDenyUnixBlock(nr uint32) {
	b.jump(bpfJMP|bpfJEQ|bpfK, nr, 0, 5) // not nr -> skip to restore-LD
	b.stmt(bpfLD|bpfW|bpfABS, seccompDataArg0LowOffset)
	b.jump(bpfJMP|bpfJEQ|bpfK, afUNIX, 1, 0) // AF_UNIX -> +1 (RET EPERM); else RET ALLOW
	b.stmt(bpfRET|bpfK, seccompRetAllow)
	b.stmt(bpfRET|bpfK, seccompRetErrno(errnoEPERM))
	b.stmt(bpfLD|bpfW|bpfABS, seccompDataNROffset) // restore
}

// seccompRetErrno builds a SECCOMP_RET_ERRNO action returning the given errno.
func seccompRetErrno(errno uint32) uint32 {
	return uint32(seccompRetErrnoTop) | (errno & 0x0000ffff)
}
