//go:build windows

package sandbox

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RunWindowsSandboxMain is the in-process Windows sandbox helper entrypoint. A
// binary's main() detects NativeSandboxArgv0 as argv[1] and calls this function.
// It reads the NativeSandboxSpec from NativeSandboxSpecEnv, applies deny-read
// ACLs to the denied paths, builds the restricted/low-integrity token for the
// requested WindowsSandboxLevel, and launches the target command under that
// token via CreateProcessAsUser. It inherits the helper's std handles (which the
// caller wired through the pty package) so stdio behaves identically to the
// unsandboxed path. The helper waits for the child and returns its exit code; the
// deny-read ACEs added by this run are reverted before returning.
//
// Mirrors the Windows sandbox launch path (token.rs + process.rs + deny ACLs).
func RunWindowsSandboxMain(errOut io.Writer) int {
	encoded, ok := os.LookupEnv(NativeSandboxSpecEnv)
	if !ok {
		fmt.Fprintf(errOut, "windows sandbox helper: missing %s\n", NativeSandboxSpecEnv)
		return 1
	}
	spec, err := DecodeNativeSandboxSpec(encoded)
	if err != nil {
		fmt.Fprintf(errOut, "windows sandbox helper: %v\n", err)
		return 1
	}

	code, err := runWindowsSandbox(spec)
	if err != nil {
		fmt.Fprintf(errOut, "windows sandbox helper: %v\n", err)
		return 1
	}
	return code
}

// runWindowsSandbox applies the deny-read ACLs and runs the command under the
// sandbox token, returning the child's exit code.
func runWindowsSandbox(spec NativeSandboxSpec) (int, error) {
	token, err := buildSandboxToken(spec.WindowsSandboxLevel)
	if err != nil {
		return 1, err
	}
	defer token.Close()

	sid, err := sandboxPrincipalSID(token)
	if err != nil {
		return 1, err
	}

	revert, err := applyDenyReadACLs(spec.DenyReadPaths, sid)
	if err != nil {
		return 1, err
	}
	defer revert()

	return runUnderToken(token, spec)
}

// runUnderToken launches spec.Command under token using CreateProcessAsUser,
// inheriting the helper's std handles, then waits for it and returns its exit
// code. Mirrors spawn_process / CreateProcessAsUserW usage in process.rs.
func runUnderToken(token windows.Token, spec NativeSandboxSpec) (int, error) {
	commandLine, err := buildCommandLine(spec.Command)
	if err != nil {
		return 1, err
	}
	cmdLine16, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return 1, fmt.Errorf("encode command line: %w", err)
	}

	var cwd16 *uint16
	if spec.Cwd != "" {
		cwd16, err = windows.UTF16PtrFromString(spec.Cwd)
		if err != nil {
			return 1, fmt.Errorf("encode cwd: %w", err)
		}
	}

	envBlock, err := childEnvBlock()
	if err != nil {
		return 1, err
	}

	// The child inherits the helper's std handles; they must be marked inheritable
	// first or CreateProcessAsUser would wire closed/duplicated handles. Mirrors
	// ensure_inheritable_stdio in process.rs.
	stdIn := windows.Handle(os.Stdin.Fd())
	stdOut := windows.Handle(os.Stdout.Fd())
	stdErr := windows.Handle(os.Stderr.Fd())
	for _, h := range []windows.Handle{stdIn, stdOut, stdErr} {
		if err := makeHandleInheritable(h); err != nil {
			return 1, err
		}
	}

	si := windows.StartupInfo{
		Flags:     windows.STARTF_USESTDHANDLES,
		StdInput:  stdIn,
		StdOutput: stdOut,
		StdErr:    stdErr,
	}
	si.Cb = uint32(unsafe.Sizeof(si))

	var pi windows.ProcessInformation
	if err := windows.CreateProcessAsUser(
		token,
		nil,
		cmdLine16,
		nil, nil,
		true, // inherit handles (for std handles)
		windows.CREATE_UNICODE_ENVIRONMENT,
		envBlock,
		cwd16,
		&si,
		&pi,
	); err != nil {
		return 1, fmt.Errorf("CreateProcessAsUser %q: %w", spec.Command[0], err)
	}
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)

	if _, err := windows.WaitForSingleObject(pi.Process, windows.INFINITE); err != nil {
		return 1, fmt.Errorf("wait for sandboxed process: %w", err)
	}

	var exitCode uint32
	if err := windows.GetExitCodeProcess(pi.Process, &exitCode); err != nil {
		return 1, fmt.Errorf("get sandboxed process exit code: %w", err)
	}
	return int(exitCode), nil
}

// makeHandleInheritable marks h inheritable so a child created with
// inheritHandles=true receives it. Mirrors the SetHandleInformation calls in
// ensure_inheritable_stdio (process.rs).
func makeHandleInheritable(h windows.Handle) error {
	if h == 0 || h == windows.InvalidHandle {
		return fmt.Errorf("invalid std handle")
	}
	if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		return fmt.Errorf("set std handle inheritable: %w", err)
	}
	return nil
}

// buildCommandLine quotes and joins argv into a single command line per the
// Windows CommandLineToArgvW rules. It reuses the standard library's quoting so
// behavior matches os/exec.
func buildCommandLine(command []string) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("empty command")
	}
	parts := make([]string, len(command))
	for i, arg := range command {
		parts[i] = windowsEscapeArg(arg)
	}
	return strings.Join(parts, " "), nil
}

// windowsEscapeArg quotes a single argument following the CommandLineToArgvW
// backslash/quote conventions.
func windowsEscapeArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n\v\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for i := 0; i < len(arg); i++ {
		c := arg[i]
		switch c {
		case '\\':
			backslashes++
		case '"':
			for ; backslashes > 0; backslashes-- {
				b.WriteString(`\\`)
			}
			b.WriteString(`\"`)
		default:
			for ; backslashes > 0; backslashes-- {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		}
	}
	for ; backslashes > 0; backslashes-- {
		b.WriteString(`\\`)
	}
	b.WriteByte('"')
	return b.String()
}

// childEnvBlock builds a CREATE_UNICODE_ENVIRONMENT block from the current
// environment with the sandbox spec env var removed so the child never sees it.
func childEnvBlock() (*uint16, error) {
	env := removeEnv(os.Environ(), NativeSandboxSpecEnv)
	return windowsEnvBlock(env)
}

// windowsEnvBlock encodes a "KEY=VALUE" slice into a double-null-terminated
// UTF-16 environment block suitable for CreateProcessAsUser. An empty slice still
// produces a valid (single entry plus terminator) block.
func windowsEnvBlock(env []string) (*uint16, error) {
	var b []uint16
	for _, entry := range env {
		if strings.IndexByte(entry, 0) >= 0 {
			return nil, fmt.Errorf("environment entry contains NUL: %q", entry)
		}
		u16, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, fmt.Errorf("encode env entry: %w", err)
		}
		b = append(b, u16...) // includes the per-entry NUL terminator
	}
	b = append(b, 0) // final block terminator
	return &b[0], nil
}
