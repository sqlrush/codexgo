package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/sqlrush/codexgo/internal/exec"
)

// runReviewSubcommand handles `codex review`: run a code review non-interactively.
//
// In the reference codex, `codex review` is implemented as `codex exec` with the
// Review subcommand (main.rs constructs an ExecCli with ExecCommand::Review). This
// port mirrors that: it forwards the review args to the exec package's parser
// (prefixing the "review" subcommand token) and runs the turn against the
// assembled engine.
func runReviewSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	for _, arg := range parsed.SubcommandArgs {
		if arg == "-h" || arg == "--help" {
			printReviewHelp(streams.Stdout)
			return 0
		}
	}

	// Prefix the "review" token so exec.ParseArgs selects SubcommandReview, the
	// same flattening main.rs performs (ExecCommand::Review).
	reviewArgs := append([]string{"review"}, parsed.SubcommandArgs...)
	cli, err := exec.ParseArgs(reviewArgs)
	if err != nil {
		fmt.Fprintln(streams.Stderr, err)
		return 2
	}

	asm, defaults, err := buildAssemblyWithDefaults()
	if err != nil {
		fmt.Fprintln(streams.Stderr, "codex review:", err)
		return 1
	}

	env := exec.Environment{
		Stdin:           streams.Stdin,
		Stdout:          streams.Stdout,
		Stderr:          streams.Stderr,
		StdinIsTerminal: streams.StdinIsTerminal,
		Assembly:        asm,
		Defaults:        defaults,
	}

	return exec.Run(ctx, cli, env)
}

func printReviewHelp(w io.Writer) {
	fmt.Fprintln(w, "Run a code review non-interactively")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex review [OPTIONS] [PROMPT]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "      --uncommitted    Review uncommitted changes in the working tree")
	fmt.Fprintln(w, "      --base <BRANCH>  Review changes relative to the given base branch")
	fmt.Fprintln(w, "      --commit <REF>   Review the changes introduced by the given commit")
	fmt.Fprintln(w, "      --title <TITLE>  Title for the review")
	fmt.Fprintln(w, "  -h, --help           Print help")
}
