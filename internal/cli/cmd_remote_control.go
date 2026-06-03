package cli

import (
	"context"
	"fmt"
	"io"
)

// runRemoteControlSubcommand handles `codex remote-control`: manage the
// app-server daemon with remote control enabled.
//
// In the reference codex this drives the app-server daemon lifecycle
// (codex_app_server_daemon::ensure_remote_control_ready / run(Stop)). The Go
// internal/appserver package provides the stdio/uds/websocket transports but no
// daemon lifecycle manager (start/stop/ready), so the start/stop actions are
// deferred with a clear notice and a non-zero exit. The subcommand and its
// --help are always registered.
func runRemoteControlSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	action := ""
	for _, arg := range parsed.SubcommandArgs {
		switch arg {
		case "-h", "--help":
			printRemoteControlHelp(streams.Stdout)
			return 0
		case "--json":
			// Accepted for compatibility; machine-readable output is not produced
			// by the deferred path.
		case "start", "stop":
			action = arg
		default:
			fmt.Fprintf(streams.Stderr, "error: unexpected argument: %s\n", arg)
			return 2
		}
	}

	verb := "Managing"
	switch action {
	case "start":
		verb = "Starting"
	case "stop":
		verb = "Stopping"
	}
	fmt.Fprintf(streams.Stderr,
		"%s the remote-control app-server daemon requires the daemon lifecycle manager (start/stop/ready), which is not wired in this build\n",
		verb)
	return 1
}

func printRemoteControlHelp(w io.Writer) {
	fmt.Fprintln(w, "[experimental] Manage the app-server daemon with remote control enabled")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex remote-control [COMMAND]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  start          Start the app-server daemon with remote control enabled")
	fmt.Fprintln(w, "  stop           Stop the app-server daemon")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "      --json     Emit machine-readable JSON")
	fmt.Fprintln(w, "  -h, --help     Print help")
}
