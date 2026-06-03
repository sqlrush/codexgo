// Command codex is the codexgo entry point: a faithful, drop-in-compatible Go
// port of the OpenAI Codex 0.136.0 CLI multitool. It dispatches the full
// subcommand surface (exec, login/logout, mcp/mcp-server, app-server, apply,
// resume/fork/archive/unarchive, features, completion, sandbox, doctor, debug),
// applies the top-level flags, performs busybox-style arg0 dispatch, and maps
// child exit codes (signals 128+n on unix, raw code on Windows).
//
// All routing, parsing, and exit-code logic lives in internal/cli; this file is
// the thin os-level shim that wires real streams, signal handling, and argv.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/sqlrush/codexgo/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	code := cli.Run(ctx, os.Args, cli.Streams{
		Stdin:            os.Stdin,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		StdinIsTerminal:  isTerminal(os.Stdin),
		StderrIsTerminal: isTerminal(os.Stderr),
	})
	os.Exit(code)
}

// isTerminal reports whether f is an interactive character device.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
