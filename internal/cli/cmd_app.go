package cli

import (
	"context"
	"fmt"
	"io"
	"runtime"
)

// runAppSubcommand handles `codex app`: launch the Codex desktop app (opening the
// installer if missing).
//
// In the reference codex this variant is compiled only on macOS and Windows
// (#[cfg(any(target_os = "macos", target_os = "windows"))]) and shells out to the
// desktop_app launcher. This port registers the subcommand on all platforms for
// parity of the command set, but the launcher integration is platform-specific
// and not wired here: it prints a clear platform notice and exits non-zero on
// unsupported platforms. The subcommand and its --help are always registered.
func runAppSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	for _, arg := range parsed.SubcommandArgs {
		if arg == "-h" || arg == "--help" {
			printAppHelp(streams.Stdout)
			return 0
		}
	}

	switch runtime.GOOS {
	case "darwin", "windows":
		fmt.Fprintf(streams.Stderr,
			"the Codex desktop app launcher is not wired in this build on %s; install and open Codex Desktop manually\n",
			runtime.GOOS)
		return 1
	default:
		fmt.Fprintf(streams.Stderr,
			"`codex app` is only available on macOS and Windows (current platform: %s)\n",
			runtime.GOOS)
		return 1
	}
}

func printAppHelp(w io.Writer) {
	fmt.Fprintln(w, "Launch the Codex desktop app (opens the app installer if missing)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex app [PATH]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Arguments:")
	fmt.Fprintln(w, "  [PATH]    Workspace path to open in Codex Desktop (default: .)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "      --download-url <URL>   Override the app installer download URL (advanced)")
	fmt.Fprintln(w, "  -h, --help                 Print help")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Note: only available on macOS and Windows.")
}
