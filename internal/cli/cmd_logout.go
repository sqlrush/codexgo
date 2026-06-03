package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/sqlrush/codexgo/internal/login"
)

// runLogoutSubcommand handles `codex logout`, removing stored credentials. It
// mirrors run_logout: success prints "Successfully logged out" and exits 0; a
// missing login prints "Not logged in" and still exits 0.
func runLogoutSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	for _, arg := range parsed.SubcommandArgs {
		if arg == "-h" || arg == "--help" {
			printLogoutHelp(streams.Stdout)
			return 0
		}
		fmt.Fprintf(streams.Stderr, "error: unexpected argument: %s\n", arg)
		return 1
	}

	cfg, err := loadConfig(parsed.Root)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "Error %v\n", err)
		return 1
	}

	loggedOut, err := login.LogoutAllStores(cfg.CodexHome, cfg.StoreMode)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "Error logging out: %v\n", err)
		return 1
	}
	if loggedOut {
		fmt.Fprintln(streams.Stderr, "Successfully logged out")
	} else {
		fmt.Fprintln(streams.Stderr, "Not logged in")
	}
	return 0
}

func printLogoutHelp(w io.Writer) {
	fmt.Fprintln(w, "Remove stored authentication credentials")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex logout")
}
