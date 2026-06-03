package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sqlrush/codexgo/internal/applypatch"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// Busybox-style alias names recognized by arg0 dispatch. These mirror the
// constants in codex-rs/arg0/src/lib.rs and the helper crates it imports.
const (
	// applyPatchArg0 routes to the apply_patch helper.
	applyPatchArg0 = "apply_patch"
	// misspelledApplyPatchArg0 is the legacy/typo alias also routed to apply_patch.
	misspelledApplyPatchArg0 = "applypatch"
	// linuxSandboxArg0 routes to the Linux sandbox backend (Linux only).
	linuxSandboxArg0 = "codex-linux-sandbox"
	// execveWrapperArg0 routes to the shell-escalation execve wrapper (unix only).
	execveWrapperArg0 = "codex-execve-wrapper"

	// fsHelperArg1 routes to the exec-server filesystem helper (matches
	// CODEX_FS_HELPER_ARG1).
	fsHelperArg1 = "--codex-run-as-fs-helper"
	// coreApplyPatchArg1 routes to the core apply_patch helper (matches
	// CODEX_CORE_APPLY_PATCH_ARG1), used by the Windows batch shim.
	coreApplyPatchArg1 = "--codex-run-as-apply-patch"
)

// Arg0Streams bundles the I/O the arg0 helpers write to. Injecting them keeps
// DispatchArg0 testable without touching the real os streams.
type Arg0Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// DispatchArg0 implements the busybox-style multitool dispatch: it inspects the
// program name (argv[0]) and the first argument (argv[1]) and, when one of the
// recognized aliases matches, runs that helper and returns (exitCode, true). When
// no alias matches it returns (0, false) so the caller proceeds with normal
// `codex` subcommand dispatch.
//
// It mirrors arg0_dispatch in codex-rs/arg0/src/lib.rs: argv[0] aliases
// (apply_patch / applypatch, codex-linux-sandbox, codex-execve-wrapper) are
// checked first, then the argv[1] markers (--codex-run-as-fs-helper,
// --codex-run-as-apply-patch). The Linux sandbox and exec-server FS helper are
// platform/spec-gated; this port routes the apply_patch family (the portable,
// in-tree backend) and reports clear errors for the not-yet-ported helpers.
func DispatchArg0(args []string, streams Arg0Streams) (int, bool) {
	streams = withArg0Defaults(streams)
	if len(args) == 0 {
		return 0, false
	}

	exeName := filepath.Base(args[0])
	switch exeName {
	case applyPatchArg0, misspelledApplyPatchArg0:
		return runApplyPatchHelper(args[1:], streams), true
	case linuxSandboxArg0:
		return runLinuxSandboxHelper(streams), true
	case execveWrapperArg0:
		if runtime.GOOS == "windows" {
			break
		}
		return runExecveWrapperHelper(streams), true
	}

	if len(args) >= 2 {
		switch args[1] {
		case fsHelperArg1:
			return runFsHelper(streams), true
		case coreApplyPatchArg1:
			return runCoreApplyPatchHelper(args[2:], streams), true
		}
	}

	return 0, false
}

// runApplyPatchHelper applies a single patch passed as the sole argument, mirroring
// codex_apply_patch::main (the apply_patch alias entry point).
func runApplyPatchHelper(args []string, streams Arg0Streams) int {
	if len(args) != 1 {
		fmt.Fprintln(streams.Stderr, "usage: apply_patch <PATCH>")
		return 1
	}
	return applyPatchText(args[0], streams)
}

// runCoreApplyPatchHelper applies a patch passed after the --codex-run-as-apply-patch
// marker, mirroring the CODEX_CORE_APPLY_PATCH_ARG1 branch.
func runCoreApplyPatchHelper(args []string, streams Arg0Streams) int {
	if len(args) < 1 {
		fmt.Fprintf(streams.Stderr, "Error: %s requires a PATCH argument.\n", coreApplyPatchArg1)
		return 1
	}
	return applyPatchText(args[0], streams)
}

// applyPatchText runs applypatch.ApplyPatch against the current working directory.
func applyPatchText(patch string, streams Arg0Streams) int {
	cwd, err := abspath.CurrentDir()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "apply_patch: %v\n", err)
		return 1
	}
	if _, err := applypatch.ApplyPatch(patch, cwd, streams.Stdout, streams.Stderr, applypatch.OSFileSystem{}); err != nil {
		return 1
	}
	return 0
}

// runLinuxSandboxHelper reports that the Linux sandbox backend (a separate spec)
// is not yet available via the arg0 alias on this build.
func runLinuxSandboxHelper(streams Arg0Streams) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(streams.Stderr, "codex-linux-sandbox: the Linux sandbox helper is only available on Linux")
		return 1
	}
	fmt.Fprintln(streams.Stderr, "codex-linux-sandbox: the Linux sandbox backend is not yet implemented (see ROADMAP)")
	return 1
}

// runExecveWrapperHelper reports that the shell-escalation execve wrapper is not
// yet ported.
func runExecveWrapperHelper(streams Arg0Streams) int {
	fmt.Fprintln(streams.Stderr, "codex-execve-wrapper: the execve wrapper is not yet implemented (see ROADMAP)")
	return 1
}

// runFsHelper reports that the exec-server filesystem helper is not yet ported.
func runFsHelper(streams Arg0Streams) int {
	fmt.Fprintln(streams.Stderr, "codex fs-helper: the exec-server filesystem helper is not yet implemented (see ROADMAP)")
	return 1
}

// withArg0Defaults fills any unset stream with the real os stream.
func withArg0Defaults(streams Arg0Streams) Arg0Streams {
	if streams.Stdin == nil {
		streams.Stdin = os.Stdin
	}
	if streams.Stdout == nil {
		streams.Stdout = os.Stdout
	}
	if streams.Stderr == nil {
		streams.Stderr = os.Stderr
	}
	return streams
}
