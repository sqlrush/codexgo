package cli

import (
	"context"
	"fmt"
	"io"
)

// runUpdateSubcommand handles `codex update`: update Codex to the latest version.
//
// In the reference codex, self-update is driven by the release-build distribution
// mechanism (npm/homebrew/binary download metadata baked into release builds).
// This source build has no embedded release channel, so it prints a clear notice
// and exits non-zero rather than pretending to update. The subcommand and its
// --help are always registered.
func runUpdateSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	for _, arg := range parsed.SubcommandArgs {
		if arg == "-h" || arg == "--help" {
			printUpdateHelp(streams.Stdout)
			return 0
		}
	}
	fmt.Fprintln(streams.Stderr,
		"self-update is not available in this build; update via your package manager "+
			"(npm/homebrew) or rebuild from source")
	return 1
}

func printUpdateHelp(w io.Writer) {
	fmt.Fprintln(w, "Update Codex to the latest version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex update")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Note: self-update is only available in release builds; this source build cannot self-update.")
}
