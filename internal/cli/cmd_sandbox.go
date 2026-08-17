package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/sqlrush/codexgo/internal/pty"
	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// runSandboxSubcommand handles `codex sandbox [OPTIONS] -- <COMMAND>...`: run a
// command under a Codex-provided platform sandbox. It applies a workspace-write
// policy (full-disk read, write limited to the working directory, network
// restricted) and propagates the child's exit code, mirroring the host sandbox
// commands (run_command_under_seatbelt / landlock / windows).
func runSandboxSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	command, cwd, code, done := parseSandboxArgs(parsed.SubcommandArgs, streams)
	if done {
		return code
	}

	req := sandbox.SpawnRequest{
		Command:                 command,
		Cwd:                     cwd,
		Env:                     currentEnv(),
		FileSystemSandboxPolicy: workspaceWritePolicy(cwd),
		NetworkSandboxPolicy:    protocol.NetworkSandboxPolicyRestricted,
		SandboxPolicyCwd:        cwd,
		Output:                  sandbox.OutputModePipe,
	}
	params := sandbox.SelectParams{
		FileSystemSandboxPolicy: req.FileSystemSandboxPolicy,
		NetworkSandboxPolicy:    req.NetworkSandboxPolicy,
		Preference:              sandbox.SandboxablePreferenceRequire,
	}

	proc, _, err := sandbox.Spawn(ctx, params, req)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "codex sandbox: %v\n", err)
		return 1
	}

	combined := pty.CombineOutput(proc.Stdout, proc.Stderr)
	for chunk := range combined {
		_, _ = streams.Stdout.Write(chunk)
	}
	return <-proc.Exit
}

// parseSandboxArgs splits the sandbox flags from the trailing command. The
// command begins after `--` (or the first non-flag token). The returned done
// flag reports that the caller should return the given code immediately (help or
// a parse error).
func parseSandboxArgs(args []string, streams Streams) (command []string, cwd string, code int, done bool) {
	cwd = resolveCwd()
	i := 0
	for i < len(args) {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			printSandboxHelp(streams.Stdout)
			return nil, "", 0, true
		case "--":
			command = args[i+1:]
			i = len(args)
		case "-C", "--cd":
			if i+1 >= len(args) {
				fmt.Fprintf(streams.Stderr, "error: flag %s requires a value\n", arg)
				return nil, "", 1, true
			}
			cwd = args[i+1]
			i += 2
		default:
			if len(arg) > 1 && arg[0] == '-' {
				// Tolerate other sandbox flags (e.g. --permissions-profile) by
				// consuming them; the policy here is fixed to workspace-write.
				i++
				continue
			}
			command = args[i:]
			i = len(args)
		}
	}

	if len(command) == 0 {
		fmt.Fprintln(streams.Stderr, "error: a command to run is required, e.g. `codex sandbox -- ls`")
		return nil, "", 1, true
	}
	return command, cwd, 0, false
}

// workspaceWritePolicy returns a restricted filesystem policy that grants
// full-disk read access plus write access to the working directory, matching the
// workspace-write sandbox preset.
func workspaceWritePolicy(cwd string) protocol.FileSystemSandboxPolicy {
	return protocol.FileSystemSandboxPolicy{
		Kind: protocol.FileSystemSandboxKindRestricted,
		Entries: []protocol.FileSystemSandboxEntry{
			{
				Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}),
				Access: protocol.FileSystemAccessModeRead,
			},
			{
				Path:   protocol.NewFileSystemPath(protocol.AbsolutePath(cwd)),
				Access: protocol.FileSystemAccessModeWrite,
			},
		},
	}
}

// currentEnv snapshots the process environment as a map for the spawned child.
func currentEnv() map[string]string {
	env := os.Environ()
	out := make(map[string]string, len(env))
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				out[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return out
}

func printSandboxHelp(w io.Writer) {
	fmt.Fprintln(w, "Run a command within a Codex-provided sandbox")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex sandbox [OPTIONS] -- <COMMAND>...")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -C, --cd <DIR>   Working directory for the command")
	fmt.Fprintln(w, "  -h, --help       Print help")
}
