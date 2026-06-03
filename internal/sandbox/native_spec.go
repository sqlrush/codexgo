package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// errWSL1Unsupported reports that the Linux sandbox cannot run under WSL1, which
// cannot create the required user namespaces. Mirrors WSL1_BWRAP_WARNING.
var errWSL1Unsupported = errors.New(
	"the Linux sandbox needs user namespaces, which WSL1 cannot create; use WSL2 for sandboxed shell commands",
)

// Environment-variable names that wire the in-process sandbox helper. The Linux
// and Windows backends re-execute the current binary with a sentinel argv[1] and
// pass the fully-resolved sandbox spec via NativeSandboxSpecEnv (JSON). This
// mirrors how the macOS backend wraps the command with /usr/bin/sandbox-exec,
// except the "wrapper" is this same binary running its sandbox-helper entrypoint.
const (
	// NativeSandboxArgv0 is the sentinel argv[1] that selects the sandbox helper
	// entrypoint. A binary's main() must detect it (before normal CLI parsing)
	// and dispatch to RunLinuxSandboxMain / RunWindowsSandboxMain.
	NativeSandboxArgv0 = "__codex_native_sandbox__"
	// NativeSandboxSpecEnv carries the JSON-encoded NativeSandboxSpec to the
	// helper. It is removed from the child's environment before exec so the
	// sandboxed command never sees it.
	NativeSandboxSpecEnv = "CODEX_NATIVE_SANDBOX_SPEC"
	// nativeSandboxStageEnv tracks which stage of the Linux helper is running.
	// The Linux backend uses a two-stage re-exec so the sandboxed command can run
	// as PID 1 in a fresh PID namespace without an unsafe raw fork from the Go
	// runtime: the outer stage sets up namespaces then re-execs itself as the
	// inner stage, which establishes the mount overlay and exec's the command.
	nativeSandboxStageEnv = "CODEX_NATIVE_SANDBOX_STAGE"
	// nativeStageOuter sets up user/mount/(net)/pid namespaces.
	nativeStageOuter = "outer"
	// nativeStageInner is PID 1 in the new PID namespace; it mounts the overlay
	// and exec's the command.
	nativeStageInner = "inner"
	// nativeSandboxPIDNSEnv is set by the outer stage on the inner re-exec when a
	// new PID namespace was created, so the inner stage knows it should mount a
	// fresh /proc reflecting that namespace.
	nativeSandboxPIDNSEnv = "CODEX_NATIVE_SANDBOX_PIDNS"
)

// NetworkSeccompMode selects how the Linux network seccomp filter restricts
// socket creation. Mirrors NetworkSeccompMode in linux-sandbox/src/landlock.rs.
type NetworkSeccompMode string

const (
	// NetworkSeccompModeNone installs no network filter (full network access).
	NetworkSeccompModeNone NetworkSeccompMode = ""
	// NetworkSeccompModeRestricted denies all outbound networking except AF_UNIX
	// sockets.
	NetworkSeccompModeRestricted NetworkSeccompMode = "restricted"
	// NetworkSeccompModeProxyRouted permits AF_INET/AF_INET6 sockets (so the
	// child can reach the local proxy bridge) while denying AF_UNIX and all other
	// families.
	NetworkSeccompModeProxyRouted NetworkSeccompMode = "proxy_routed"
)

// NativeSandboxSpec is the immutable, JSON-serializable description of the
// sandbox to apply to a re-executed command. It is produced by the platform
// backend in the parent process and consumed by the helper entrypoint in the
// child. Keeping the contract explicit (rather than rebuilding policy in the
// child) keeps the child small and deterministic and makes the setup testable.
type NativeSandboxSpec struct {
	// Command is the program followed by its arguments to exec after setup.
	Command []string `json:"command"`
	// Cwd is the working directory for the sandboxed command.
	Cwd string `json:"cwd"`
	// FullDiskWriteAccess reports whether filesystem writes are unrestricted; when
	// true no read-only root remount / Landlock write narrowing is applied.
	FullDiskWriteAccess bool `json:"full_disk_write_access"`
	// FullDiskReadAccess reports whether filesystem reads are unrestricted.
	FullDiskReadAccess bool `json:"full_disk_read_access"`
	// WritableRoots are the absolute roots the command may write to.
	WritableRoots []string `json:"writable_roots"`
	// ReadOnlySubpaths are absolute paths under a writable root that must stay
	// read-only (e.g. resolved deny entries).
	ReadOnlySubpaths []string `json:"read_only_subpaths"`
	// ProtectedSubpaths are absolute protected-metadata paths (.git/.codex/.agents)
	// that are re-applied read-only over their writable root.
	ProtectedSubpaths []string `json:"protected_subpaths"`
	// ReadableRoots are absolute roots the command may read when reads are not
	// unrestricted. Empty with FullDiskReadAccess=false means "no extra reads".
	ReadableRoots []string `json:"readable_roots"`
	// UnreadableRoots are absolute deny-read roots to hide.
	UnreadableRoots []string `json:"unreadable_roots"`
	// NetworkSeccompMode selects the network seccomp filter (Linux only).
	NetworkSeccompMode NetworkSeccompMode `json:"network_seccomp_mode"`
	// DenyReadPaths are absolute paths to deny read access (Windows ACL backend).
	DenyReadPaths []string `json:"deny_read_paths"`
	// WindowsSandboxLevel selects the Windows enforcement tier.
	WindowsSandboxLevel protocol.WindowsSandboxLevel `json:"windows_sandbox_level"`
}

// EncodeNativeSandboxSpec serializes a spec to compact JSON for transport via the
// helper environment variable.
func EncodeNativeSandboxSpec(spec NativeSandboxSpec) (string, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("sandbox: encode native spec: %w", err)
	}
	return string(data), nil
}

// DecodeNativeSandboxSpec parses a JSON-encoded spec produced by
// EncodeNativeSandboxSpec.
func DecodeNativeSandboxSpec(encoded string) (NativeSandboxSpec, error) {
	var spec NativeSandboxSpec
	if err := json.Unmarshal([]byte(encoded), &spec); err != nil {
		return NativeSandboxSpec{}, fmt.Errorf("sandbox: decode native spec: %w", err)
	}
	if len(spec.Command) == 0 {
		return NativeSandboxSpec{}, fmt.Errorf("sandbox: native spec has empty command")
	}
	return spec, nil
}

// networkSeccompModeFor mirrors network_seccomp_mode in
// linux-sandbox/src/landlock.rs. It returns the seccomp mode to install given
// the resolved network policy, whether proxy access is permitted, and whether
// the network is routed through the local proxy bridge.
func networkSeccompModeFor(
	network protocol.NetworkSandboxPolicy,
	allowNetworkForProxy bool,
	proxyRoutedNetwork bool,
) NetworkSeccompMode {
	if !shouldInstallNetworkSeccomp(network, allowNetworkForProxy) {
		return NetworkSeccompModeNone
	}
	if proxyRoutedNetwork {
		return NetworkSeccompModeProxyRouted
	}
	return NetworkSeccompModeRestricted
}

// shouldInstallNetworkSeccomp mirrors should_install_network_seccomp. Managed
// network sessions stay fail-closed even for policies that would otherwise grant
// full network access.
func shouldInstallNetworkSeccomp(network protocol.NetworkSandboxPolicy, allowNetworkForProxy bool) bool {
	return !network.IsEnabled() || allowNetworkForProxy
}

// procVersionIndicatesWSL1 mirrors proc_version_indicates_wsl1 in
// sandboxing/src/bwrap.rs: it inspects the contents of /proc/version to decide
// whether the kernel is WSL1 (which cannot create the user namespaces the Linux
// sandbox relies on). It is written without build tags so it is unit-testable on
// any host.
func procVersionIndicatesWSL1(procVersion string) bool {
	lower := strings.ToLower(procVersion)
	remaining := lower
	for {
		marker := strings.Index(remaining, "wsl")
		if marker < 0 {
			break
		}
		versionStart := marker + len("wsl")
		digits := leadingDigits(remaining[versionStart:])
		if digits != "" {
			if v, err := parseUint32(digits); err == nil {
				return v == 1
			}
		}
		remaining = remaining[versionStart:]
	}
	return strings.Contains(lower, "microsoft") && !strings.Contains(lower, "microsoft-standard")
}

// leadingDigits returns the longest ASCII-digit prefix of s.
func leadingDigits(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return s[:i]
		}
	}
	return s
}

// parseUint32 parses an ASCII base-10 string into a uint32, rejecting overflow.
func parseUint32(s string) (uint32, error) {
	var v uint64
	for i := 0; i < len(s); i++ {
		v = v*10 + uint64(s[i]-'0')
		if v > 0xffffffff {
			return 0, fmt.Errorf("sandbox: uint32 overflow parsing %q", s)
		}
	}
	return uint32(v), nil
}
