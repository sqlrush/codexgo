package cli

import (
	"context"
	"fmt"
	"io"
)

// helpPrinters maps each canonical subcommand name to a function that writes that
// subcommand's help text. It is the registry `codex help <cmd>` consults. Each
// printer mirrors the corresponding subcommand's own -h/--help output.
var helpPrinters = map[string]func(io.Writer){
	"exec":           printExecHelp,
	"review":         printReviewHelp,
	"login":          printLoginHelp,
	"logout":         printLogoutHelp,
	"mcp":            printMcpHelp,
	"plugin":         printPluginHelp,
	"mcp-server":     printMcpServerHelp,
	"app-server":     printAppServerHelp,
	"remote-control": printRemoteControlHelp,
	"app":            printAppHelp,
	"completion":     printCompletionHelp,
	"update":         printUpdateHelp,
	"doctor":         printDoctorHelp,
	"sandbox":        printSandboxHelp,
	"debug":          printDebugHelp,
	"apply":          printApplyHelp,
	"resume":         func(w io.Writer) { printSessionHelp(w, "resume", true) },
	"fork":           func(w io.Writer) { printSessionHelp(w, "fork", true) },
	"archive":        func(w io.Writer) { printSessionHelp(w, "archive", false) },
	"unarchive":      func(w io.Writer) { printSessionHelp(w, "unarchive", false) },
	"cloud":          printCloudHelp,
	"exec-server":    printExecServerHelp,
	"features":       printFeaturesHelp,
	"help":           printHelpHelp,
}

// printExecHelp writes the `codex exec` help. The exec package parses its own
// args and does not embed help text, so this summary lives here for the
// `codex help exec` path.
func printExecHelp(w io.Writer) {
	fmt.Fprintln(w, "Run Codex non-interactively (alias: e)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex exec [OPTIONS] [PROMPT]")
	fmt.Fprintln(w, "       codex exec resume [SESSION_ID] [PROMPT]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "      --json                       Emit JSONL events instead of human text")
	fmt.Fprintln(w, "  -o, --output-last-message <FILE> Write the final agent message to FILE")
	fmt.Fprintln(w, "      --output-schema <FILE>       JSON Schema constraining the final response")
	fmt.Fprintln(w, "  -i, --image <PATH>               Attach a local image to the prompt (repeatable)")
	fmt.Fprintln(w, "  -h, --help                       Print help")
}

// runHelpSubcommand handles `codex help [COMMAND]`: with no argument it prints
// the top-level help (same as `codex --help`); with a subcommand argument it
// prints that subcommand's help. Mirrors clap's built-in `help` command.
func runHelpSubcommand(_ context.Context, parsed ParsedCommandLine, streams Streams) int {
	if len(parsed.SubcommandArgs) == 0 {
		printTopLevelHelp(streams.Stdout)
		return 0
	}

	token := parsed.SubcommandArgs[0]
	if token == "-h" || token == "--help" {
		printHelpHelp(streams.Stdout)
		return 0
	}

	name, ok := canonicalSubcommand(token)
	if !ok {
		fmt.Fprintf(streams.Stderr, "error: unknown command %q\n", token)
		return 1
	}
	printer, ok := helpPrinters[name]
	if !ok {
		// Known subcommand without a registered help printer: fall back to the
		// top-level help rather than producing nothing.
		printTopLevelHelp(streams.Stdout)
		return 0
	}
	printer(streams.Stdout)
	return 0
}

func printHelpHelp(w io.Writer) {
	fmt.Fprintln(w, "Print this message or the help of the given subcommand(s)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex help [COMMAND]")
}
