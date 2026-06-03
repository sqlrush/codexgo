package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// sessionArgs holds the parsed flags shared by the resume/fork/archive/unarchive
// session subcommands.
type sessionArgs struct {
	// target is the session id (UUID) or session name positional, when present.
	target string
	last   bool
	all    bool
	help   bool
}

// runResumeSubcommand handles `codex resume [SESSION_ID] [--last] [--all]`.
//
// Resume is an interactive TUI operation that replays a stored session; the
// thread read path (internal/threadstore read/list) and the TUI are later
// roadmap phases. This handler validates the arguments and emits a clear notice.
func runResumeSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	return runSessionTuiStub(parsed, streams, "resume",
		"Resuming a session requires the interactive TUI (see ROADMAP Phase 9).")
}

// runForkSubcommand handles `codex fork [SESSION_ID] [--last] [--all]`.
func runForkSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	return runSessionTuiStub(parsed, streams, "fork",
		"Forking a session requires the interactive TUI (see ROADMAP Phase 9).")
}

// runArchiveSubcommand handles `codex archive <SESSION>`.
func runArchiveSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	return runSessionArchiveStub(parsed, streams, "archive")
}

// runUnarchiveSubcommand handles `codex unarchive <SESSION>`.
func runUnarchiveSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	return runSessionArchiveStub(parsed, streams, "unarchive")
}

// runSessionTuiStub validates resume/fork args and reports the TUI dependency.
func runSessionTuiStub(parsed ParsedCommandLine, streams Streams, name, notice string) int {
	args, err := parseSessionArgs(parsed.SubcommandArgs)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	if args.help {
		printSessionHelp(streams.Stdout, name, true)
		return 0
	}
	fmt.Fprintln(streams.Stderr, notice)
	return 1
}

// runSessionArchiveStub validates archive/unarchive args and reports that the
// thread store read/metadata path (internal/threadstore) is a later phase.
func runSessionArchiveStub(parsed ParsedCommandLine, streams Streams, name string) int {
	args, err := parseSessionArgs(parsed.SubcommandArgs)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	if args.help {
		printSessionHelp(streams.Stdout, name, false)
		return 0
	}
	if args.target == "" {
		fmt.Fprintf(streams.Stderr, "error: %s requires a SESSION id or name\n", name)
		return 1
	}
	fmt.Fprintf(streams.Stderr,
		"`codex %s` requires the thread store read/metadata path (internal/threadstore), which is a later roadmap phase.\n",
		name)
	return 1
}

// parseSessionArgs parses the shared session flags and optional positional target.
func parseSessionArgs(args []string) (sessionArgs, error) {
	var out sessionArgs
	for _, arg := range args {
		switch arg {
		case "--last":
			out.last = true
		case "--all":
			out.all = true
		case "--include-non-interactive":
			// Accepted for compatibility with the resume picker surface.
		case "-h", "--help":
			out.help = true
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return sessionArgs{}, fmt.Errorf("unexpected flag: %s", arg)
			}
			if out.target != "" {
				return sessionArgs{}, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			out.target = arg
		}
	}
	return out, nil
}

func printSessionHelp(w io.Writer, name string, picker bool) {
	fmt.Fprintf(w, "Codex %s\n\n", name)
	if picker {
		fmt.Fprintf(w, "Usage: codex %s [SESSION_ID] [OPTIONS]\n\n", name)
		fmt.Fprintln(w, "Options:")
		fmt.Fprintln(w, "      --last   Select the most recent session without the picker")
		fmt.Fprintln(w, "      --all    Show all sessions (disables cwd filtering)")
	} else {
		fmt.Fprintf(w, "Usage: codex %s <SESSION>\n", name)
	}
}
