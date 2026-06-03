package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/sqlrush/codexgo/internal/gitutils"
)

// applyArgs holds the parsed `codex apply` flags.
type applyArgs struct {
	// patchPath is a file to read the diff from ("-" or empty means stdin).
	patchPath string
	// revert applies the patch in reverse (git apply -R).
	revert bool
	// preflight runs git apply --check without modifying the tree.
	preflight bool
	help      bool
}

// runApplySubcommand handles `codex apply [OPTIONS] [PATCH_FILE]` (alias `a`). It
// applies a unified diff to the current working tree via `git apply --3way`,
// reading the diff from a file argument or stdin. This is the git-apply backend
// the task specifies (internal/applypatch/gitutils as git apply); fetching a diff
// from a Codex Cloud task id is a separate, network-backed feature.
func runApplySubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	args, err := parseApplyArgs(parsed.SubcommandArgs)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	if args.help {
		printApplyHelp(streams.Stdout)
		return 0
	}

	diff, err := readDiff(args.patchPath, streams)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}

	res, err := gitutils.ApplyGitPatch(gitutils.ApplyGitRequest{
		Cwd:       cwd,
		Diff:      diff,
		Revert:    args.revert,
		Preflight: args.preflight,
	})
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: git apply: %v\n", err)
		return 1
	}
	if res.ExitCode != 0 {
		fmt.Fprintf(streams.Stderr,
			"Git apply failed (applied=%d, skipped=%d, conflicts=%d)\nstdout:\n%s\nstderr:\n%s\n",
			len(res.AppliedPaths), len(res.SkippedPaths), len(res.ConflictedPaths), res.Stdout, res.Stderr)
		return 1
	}

	fmt.Fprintf(streams.Stdout, "Applied %d file(s).\n", len(res.AppliedPaths))
	return 0
}

// readDiff reads the diff text from the given file path, or from stdin when the
// path is empty or "-".
func readDiff(patchPath string, streams Streams) (string, error) {
	if patchPath == "" || patchPath == "-" {
		data, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading diff from stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(patchPath)
	if err != nil {
		return "", fmt.Errorf("reading diff file %q: %w", patchPath, err)
	}
	return string(data), nil
}

// parseApplyArgs parses the apply flags and the optional patch-file positional.
func parseApplyArgs(args []string) (applyArgs, error) {
	var out applyArgs
	for _, arg := range args {
		switch arg {
		case "-R", "--revert":
			out.revert = true
		case "--check", "--preflight":
			out.preflight = true
		case "-h", "--help":
			out.help = true
		default:
			if len(arg) > 1 && arg[0] == '-' && arg != "-" {
				return applyArgs{}, fmt.Errorf("unexpected flag: %s", arg)
			}
			if out.patchPath != "" {
				return applyArgs{}, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			out.patchPath = arg
		}
	}
	return out, nil
}

func printApplyHelp(w io.Writer) {
	fmt.Fprintln(w, "Apply a unified diff to the working tree via `git apply` (alias: a)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex apply [OPTIONS] [PATCH_FILE]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "When PATCH_FILE is omitted or '-', the diff is read from stdin.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -R, --revert     Apply the patch in reverse")
	fmt.Fprintln(w, "      --check      Dry-run (git apply --check) without modifying the tree")
	fmt.Fprintln(w, "  -h, --help       Print help")
}
