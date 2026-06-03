package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/sqlrush/codexgo/internal/modelsmanager"
)

// runDebugSubcommand handles `codex debug <models|prompt-input>`, mirroring the
// DebugCommand surface in main.rs (the subset this port wires).
func runDebugSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	args := parsed.SubcommandArgs
	if len(args) == 0 {
		fmt.Fprintln(streams.Stderr, "error: a subcommand is required: models or prompt-input")
		return 1
	}
	if args[0] == "-h" || args[0] == "--help" {
		printDebugHelp(streams.Stdout)
		return 0
	}

	switch args[0] {
	case "models":
		return runDebugModels(streams, args[1:])
	case "prompt-input":
		fmt.Fprintln(streams.Stderr, "`codex debug prompt-input` requires the prompt-input builder (core), which is a later roadmap phase.")
		return 1
	default:
		fmt.Fprintf(streams.Stderr, "error: unknown debug subcommand %q\n", args[0])
		return 1
	}
}

// runDebugModels renders the model catalog as JSON. The live (online) catalog
// requires auth/network and is owned by the models-manager area; this port emits
// the bundled catalog shipped with the binary, matching `debug models --bundled`.
func runDebugModels(streams Streams, args []string) int {
	for _, arg := range args {
		switch arg {
		case "--bundled":
			// Always bundled in this build.
		default:
			fmt.Fprintf(streams.Stderr, "error: unexpected argument: %s\n", arg)
			return 1
		}
	}

	catalog, err := modelsmanager.BundledModelsResponse()
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintln(streams.Stdout, string(data))
	return 0
}

func printDebugHelp(w io.Writer) {
	fmt.Fprintln(w, "Debugging tools")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex debug <COMMAND>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  models          Render the model catalog as JSON")
	fmt.Fprintln(w, "  prompt-input    Render the model-visible prompt input list as JSON")
}
