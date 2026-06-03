//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// RunLinuxSandboxMain is the in-process Linux sandbox helper entrypoint. A
// binary's main() detects NativeSandboxArgv0 as argv[1] and calls this function,
// which reads the NativeSandboxSpec from NativeSandboxSpecEnv, establishes the
// sandbox (namespaces, mount overlay, fresh /proc, Landlock, seccomp), and then
// exec's the target command. On success the helper returns the sandboxed
// command's exit code; on setup failure it writes a diagnostic to errOut and
// returns a non-zero code so main() can os.Exit with it.
//
// This mirrors codex-linux-sandbox's run_main, performed natively (no bwrap).
// To run the command as PID 1 in fresh user/mount/pid/(net) namespaces without
// the multithreaded-process restriction on unshare(CLONE_NEWUSER), the helper
// runs in two stages selected by nativeSandboxStageEnv:
//
//	outer: launch the binary again as the inner stage via os/exec with
//	       SysProcAttr.Cloneflags + Uid/GidMappings, so the Go runtime performs
//	       the clone + user-namespace id-mapping atomically in the child before
//	       the runtime starts threads. The outer stage waits and propagates the
//	       child's exit code.
//	inner: apply the mount overlay + fresh /proc + Landlock + seccomp, then exec
//	       the command (it is PID 1 in the new PID namespace).
func RunLinuxSandboxMain(errOut io.Writer) int {
	runtime.LockOSThread()

	encoded, ok := os.LookupEnv(NativeSandboxSpecEnv)
	if !ok {
		fmt.Fprintf(errOut, "linux sandbox helper: missing %s\n", NativeSandboxSpecEnv)
		return 1
	}
	spec, err := DecodeNativeSandboxSpec(encoded)
	if err != nil {
		fmt.Fprintf(errOut, "linux sandbox helper: %v\n", err)
		return 1
	}

	stage := os.Getenv(nativeSandboxStageEnv)
	if stage == "" {
		stage = nativeStageOuter
	}

	if stage == nativeStageInner {
		// The inner stage exec's the command and only returns on error.
		if err := runInnerStage(spec); err != nil {
			fmt.Fprintf(errOut, "linux sandbox helper: %v\n", err)
			return 1
		}
		return 0
	}

	code, err := runOuterStage(spec)
	if err != nil {
		fmt.Fprintf(errOut, "linux sandbox helper: %v\n", err)
		return 1
	}
	return code
}

// runOuterStage launches the inner stage in fresh namespaces and returns its
// exit code. The user/mount/pid/(net) namespaces and the uid/gid identity
// mapping are requested via SysProcAttr so the Go runtime applies them in the
// child correctly.
func runOuterStage(spec NativeSandboxSpec) (int, error) {
	if procVersionIndicatesWSL1(readProcVersion()) {
		return 1, errWSL1Unsupported
	}

	setup := nsSetupFromSpec(spec)

	self, err := os.Executable()
	if err != nil {
		return 1, fmt.Errorf("resolve self for inner stage: %w", err)
	}

	cloneFlags := uintptr(unix.CLONE_NEWUSER | unix.CLONE_NEWNS)
	if setup.unshareNet {
		cloneFlags |= unix.CLONE_NEWNET
	}
	usePID := !isRunningInContainer()
	if usePID {
		cloneFlags |= unix.CLONE_NEWPID
	}

	cmd := exec.Command(self, NativeSandboxArgv0)
	cmd.Env = innerStageEnv(usePID)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 cloneFlags,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: os.Getuid(), HostID: os.Getuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: os.Getgid(), HostID: os.Getgid(), Size: 1}},
		GidMappingsEnableSetgroups: false,
	}

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EINVAL) {
			return 1, fmt.Errorf("the Linux sandbox needs permission to create user namespaces: %w", err)
		}
		return 1, fmt.Errorf("run sandboxed command: %w", err)
	}
	return 0, nil
}

// innerStageEnv builds the environment for the inner stage: the current env plus
// the stage marker and (when a PID namespace was requested) the /proc marker so
// the inner stage mounts a fresh /proc.
func innerStageEnv(usePID bool) []string {
	env := os.Environ()
	env = setEnv(env, nativeSandboxStageEnv, nativeStageInner)
	if usePID {
		env = setEnv(env, nativeSandboxPIDNSEnv, "1")
	} else {
		env = removeEnv(env, nativeSandboxPIDNSEnv)
	}
	return env
}

// runInnerStage establishes the mount overlay, fresh /proc, Landlock and seccomp
// inside the (already-created) namespaces, then exec's the command. It only
// returns on error.
func runInnerStage(spec NativeSandboxSpec) error {
	setup := nsSetupFromSpec(spec)

	// no_new_privs is required for seccomp and Landlock; set it whenever we will
	// install either. It is inherited across the final exec.
	needNoNewPrivs := spec.NetworkSeccompMode != NetworkSeccompModeNone || !spec.FullDiskWriteAccess
	if needNoNewPrivs {
		if err := setNoNewPrivs(); err != nil {
			return err
		}
	}

	if err := applyMountOverlay(setup); err != nil {
		return err
	}

	// Only mount a fresh /proc when a new PID namespace was created (signaled via
	// nativeSandboxPIDNSEnv); the mount is skipped gracefully in containers that
	// forbid it.
	if os.Getenv(nativeSandboxPIDNSEnv) != "" {
		if _, err := mountFreshProc(); err != nil {
			return err
		}
	}

	if !spec.FullDiskWriteAccess {
		readOnly := append(append([]string{}, spec.ProtectedSubpaths...), spec.ReadOnlySubpaths...)
		if err := installFilesystemLandlock(spec.WritableRoots, readOnly); err != nil && err != landlockUnsupported {
			return err
		}
	}

	if err := installNetworkSeccompFilter(spec.NetworkSeccompMode); err != nil {
		return err
	}

	if err := execCommand(spec); err != nil {
		return fmt.Errorf("exec %q: %w", spec.Command[0], err)
	}
	return nil
}

// readProcVersion returns the contents of /proc/version, or "" when unreadable.
func readProcVersion() string {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return ""
	}
	return string(data)
}

// execCommand replaces the current process image with the spec's command,
// resolving it against PATH when it is not an absolute path. The sandbox env
// vars are removed so the child never sees them.
func execCommand(spec NativeSandboxSpec) error {
	if spec.Cwd != "" {
		if err := unix.Chdir(spec.Cwd); err != nil {
			return fmt.Errorf("chdir %q: %w", spec.Cwd, err)
		}
	}

	program := spec.Command[0]
	resolved, err := lookPathSandbox(program)
	if err != nil {
		return err
	}

	env := os.Environ()
	env = removeEnv(env, NativeSandboxSpecEnv)
	env = removeEnv(env, nativeSandboxStageEnv)
	env = removeEnv(env, nativeSandboxPIDNSEnv)

	return unix.Exec(resolved, spec.Command, env)
}
