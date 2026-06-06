package cli

import (
	"context"
	"fmt"
	"io"
	"os"
)

// Version is the codexgo build version reported by `codex --version`. Parity
// target: codex 0.136.0. It defaults to a dev placeholder and is stamped at
// release-build time via:
//
//	-ldflags "-X github.com/sqlrush/codexgo/internal/cli.Version=<semver>"
var Version = "0.0.0-dev"

// BuildCommit is the source commit baked into the build, or "unknown" when not
// provided. It mirrors the Rust build_commit() helper (CODEX_BUILD_COMMIT /
// GIT_COMMIT compile-time env), and can be set via -ldflags at build time.
var BuildCommit = "unknown"

// Streams bundles the process I/O the CLI reads from and writes to. Injecting
// them keeps Run testable with in-memory buffers.
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// StdinIsTerminal reports whether Stdin is an interactive terminal.
	StdinIsTerminal bool
	// StderrIsTerminal reports whether Stderr is an interactive terminal.
	StderrIsTerminal bool
}

// withDefaults fills any unset stream with the real os stream.
func (s Streams) withDefaults() Streams {
	if s.Stdin == nil {
		s.Stdin = os.Stdin
	}
	if s.Stdout == nil {
		s.Stdout = os.Stdout
	}
	if s.Stderr == nil {
		s.Stderr = os.Stderr
	}
	return s
}

// Run is the top-level entry point for the `codex` binary. It first attempts the
// busybox-style arg0 dispatch (using the full argv including argv[0]); when no
// alias matches it parses the top-level flags, routes the selected subcommand,
// and returns the process exit code.
//
// argv must be the full process arguments including argv[0]. ctx is propagated to
// subcommands that run long-lived work (exec, mcp-server, app-server, sandbox).
func Run(ctx context.Context, argv []string, streams Streams) int {
	streams = streams.withDefaults()

	if code, handled := DispatchArg0(argv, Arg0Streams{
		Stdin:  streams.Stdin,
		Stdout: streams.Stdout,
		Stderr: streams.Stderr,
	}); handled {
		return code
	}

	args := argv
	if len(args) > 0 {
		args = args[1:]
	}

	parsed, err := ParseCommandLine(args)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}

	if parsed.Root.ShowVersion {
		fmt.Fprintf(streams.Stdout, "codex %s\n", Version)
		return 0
	}
	if parsed.Root.ShowHelp && parsed.Subcommand == "" {
		printTopLevelHelp(streams.Stdout)
		return 0
	}

	return dispatchSubcommand(ctx, parsed, streams)
}

// dispatchSubcommand routes the parsed command line to the selected subcommand's
// handler, applying the remote-mode rejection guard for non-interactive
// subcommands first (matching reject_remote_mode_for_subcommand in main.rs).
func dispatchSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	if parsed.Subcommand == "" {
		return runDefaultNoSubcommand(ctx, parsed, streams)
	}

	if err := rejectRemoteMode(parsed.Root, parsed.Subcommand); err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}

	handler, ok := handlers[parsed.Subcommand]
	if !ok {
		fmt.Fprintf(streams.Stderr, "error: unknown subcommand %q\n", parsed.Subcommand)
		return 1
	}
	return handler(ctx, parsed, streams)
}

// subcommandHandler runs a single subcommand and returns its process exit code.
type subcommandHandler func(ctx context.Context, parsed ParsedCommandLine, streams Streams) int

// handlers is the subcommand dispatch table. Each entry maps a canonical
// subcommand name to its handler. The handlers live in their own files
// (cmd_*.go) for cohesion.
var handlers = map[string]subcommandHandler{
	"exec":           runExecSubcommand,
	"review":         runReviewSubcommand,
	"login":          runLoginSubcommand,
	"logout":         runLogoutSubcommand,
	"mcp":            runMcpSubcommand,
	"plugin":         runPluginSubcommand,
	"mcp-server":     runMcpServerSubcommand,
	"app-server":     runAppServerSubcommand,
	"remote-control": runRemoteControlSubcommand,
	"app":            runAppSubcommand,
	"apply":          runApplySubcommand,
	"resume":         runResumeSubcommand,
	"fork":           runForkSubcommand,
	"archive":        runArchiveSubcommand,
	"unarchive":      runUnarchiveSubcommand,
	"cloud":          runCloudSubcommand,
	"exec-server":    runExecServerSubcommand,
	"features":       runFeaturesSubcommand,
	"completion":     runCompletionSubcommand,
	"update":         runUpdateSubcommand,
	"sandbox":        runSandboxSubcommand,
	"doctor":         runDoctorSubcommand,
	"debug":          runDebugSubcommand,
	"help":           runHelpSubcommand,
}

// rejectRemoteMode rejects --remote / --remote-auth-token-env for subcommands
// that are not the interactive TUI. Mirrors reject_remote_mode_for_subcommand.
func rejectRemoteMode(root RootOptions, subcommand string) error {
	if root.Remote != "" {
		return fmt.Errorf("`--remote %s` is only supported for interactive TUI commands, not `codex %s`", root.Remote, subcommand)
	}
	if root.RemoteAuthTokenEnv != "" {
		return fmt.Errorf("`--remote-auth-token-env` is only supported for interactive TUI commands, not `codex %s`", subcommand)
	}
	return nil
}

// printTopLevelHelp writes the top-level usage and subcommand list.
func printTopLevelHelp(w io.Writer) {
	fmt.Fprintln(w, "Codex CLI")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex [OPTIONS] [PROMPT]")
	fmt.Fprintln(w, "       codex [OPTIONS] <COMMAND> [ARGS]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, s := range subcommandSummaries {
		fmt.Fprintf(w, "  %-15s %s\n", s.Name, s.Summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -c, --config <key=value>        Override a configuration value (repeatable)")
	fmt.Fprintln(w, "  -p, --profile <NAME>            Layer the named profile config on top of the base config")
	fmt.Fprintln(w, "      --enable <FEATURE>          Enable a feature (repeatable)")
	fmt.Fprintln(w, "      --disable <FEATURE>         Disable a feature (repeatable)")
	fmt.Fprintln(w, "      --remote <ADDR>             Connect to a remote app server (interactive only)")
	fmt.Fprintln(w, "      --remote-auth-token-env <ENV_VAR>  Env var holding the remote bearer token")
	fmt.Fprintln(w, "      --strict-config             Error out on unrecognized config fields")
	fmt.Fprintln(w, "  -h, --help                      Print help")
	fmt.Fprintln(w, "  -V, --version                   Print version")
}
