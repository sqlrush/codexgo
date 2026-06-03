package cli

import "fmt"

// runDefaultNoSubcommand handles `codex` with no subcommand. The interactive TUI
// is a later roadmap phase (Phase 9); until then this prints a clear notice and
// exits non-zero so wrappers and scripts can detect that no work ran.
func runDefaultNoSubcommand(_ ParsedCommandLine, streams Streams) int {
	fmt.Fprintf(streams.Stderr, "codex %s — parity target: codex 0.136.0\n", Version)
	fmt.Fprintln(streams.Stderr, "interactive TUI not yet implemented (see ROADMAP Phase 9); use `codex exec` to run non-interactively")
	return 1
}
