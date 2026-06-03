package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sqlrush/codexgo/internal/appserver"
)

// runAppServerSubcommand handles `codex app-server`: run the app server over
// stdio. The full Rust app-server supports multiple transports (unix://, ws://)
// and a daemon lifecycle; this port wires the default stdio transport, which is
// the primary one used by the IDE/CLI integrations.
func runAppServerSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	for _, arg := range parsed.SubcommandArgs {
		switch arg {
		case "-h", "--help":
			printAppServerHelp(streams.Stdout)
			return 0
		case "--stdio", "--strict-config":
			// Accepted: stdio is the wired transport; strict-config is owned by the
			// config loader.
		default:
			fmt.Fprintf(streams.Stderr, "error: unsupported app-server argument %q (only the default stdio transport is wired in this build)\n", arg)
			return 1
		}
	}

	asm, err := buildAssembly()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "codex app-server: %v\n", err)
		return 1
	}

	defaults := appserver.Defaults{
		Model:      "gpt-mock",
		ProviderID: "openai",
		Cwd:        resolveCwd(),
		UserAgent:  "codex-cli-go",
	}

	if err := appserver.ServeStdioWithProcessor(ctx, asm, defaults, streams.Stdin, streams.Stdout); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return 0
		}
		fmt.Fprintf(streams.Stderr, "codex app-server: %v\n", err)
		return 1
	}
	return 0
}

func printAppServerHelp(w io.Writer) {
	fmt.Fprintln(w, "Run the app server (stdio)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex app-server [--stdio]")
}
