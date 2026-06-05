package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// completionShells lists the shells `codex completion` can generate scripts for,
// in clap_complete::Shell value order (bash, elvish, fish, powershell, zsh). This
// order matches the `[possible values: ...]` list clap prints in help and errors.
var completionShells = []string{"bash", "elvish", "fish", "powershell", "zsh"}

// runCompletionSubcommand handles `codex completion [SHELL]`, generating a shell
// completion script. The default shell is bash, matching the Rust
// CompletionCommand default (`#[arg(value_enum, default_value_t = Shell::Bash)]`).
//
// Each shell's script is byte-identical to clap_complete v4.5.65's output for
// codex: bash is a faithful template port driven by completion_tree.go, while
// zsh/fish/elvish/powershell are the deterministic generated scripts vendored as
// parity assets (see their respective completion_*.go files and DEVIATIONS.md).
func runCompletionSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	shell := "bash"
	for _, arg := range parsed.SubcommandArgs {
		switch arg {
		case "-h", "--help":
			printCompletionHelp(streams.Stdout)
			return 0
		default:
			if strings.HasPrefix(arg, "-") {
				fmt.Fprintf(streams.Stderr, "error: unexpected flag: %s\n", arg)
				return 1
			}
			shell = arg
		}
	}

	script, ok := completionScript(shell)
	if !ok {
		writeInvalidShellError(streams.Stderr, shell)
		return 2
	}
	fmt.Fprint(streams.Stdout, script)
	return 0
}

// writeInvalidShellError renders clap's invalid-value diagnostic for an
// unrecognized SHELL argument (exit code 2), mirroring clap's error layout:
//
//	error: invalid value '<v>' for '[SHELL]'
//	  [possible values: bash, elvish, fish, powershell, zsh]
//
//	For more information, try '--help'.
//
// clap additionally prints a "tip: a similar value exists: '<x>'" line for
// likely typos, derived from its internal Jaro + substring "did you mean"
// heuristic. Reproducing that suggestion byte-for-byte is not worth a precise
// port (a wrong suggestion is worse than none), so the tip line is intentionally
// omitted; see DEVIATIONS.md (completion row).
func writeInvalidShellError(w io.Writer, value string) {
	fmt.Fprintf(w, "error: invalid value '%s' for '[SHELL]'\n", value)
	fmt.Fprintf(w, "  [possible values: %s]\n", strings.Join(completionShells, ", "))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "For more information, try '--help'.")
}

// completionScript returns the completion script for the given shell, plus
// whether the shell is recognized. Each generator emits output byte-identical to
// clap_complete v4.5.65 for codex.
func completionScript(shell string) (string, bool) {
	switch shell {
	case "bash":
		return bashCompletionScript(), true
	case "zsh":
		return zshCompletionScript(), true
	case "fish":
		return fishCompletionScript(), true
	case "powershell":
		return powershellCompletionScript(), true
	case "elvish":
		return elvishCompletionScript(), true
	default:
		return "", false
	}
}

func printCompletionHelp(w io.Writer) {
	fmt.Fprintln(w, "Generate shell completion scripts")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex completion [SHELL]")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Shells: %s (default: bash)\n", strings.Join(completionShells, ", "))
}
