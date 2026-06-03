package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/sqlrush/codexgo/internal/mcpserver"
)

// runMcpServerSubcommand handles `codex mcp-server`: start Codex as an MCP server
// over stdio, mirroring codex_mcp_server::run_main. Clients connect via standard
// MCP (JSON-RPC 2.0, line-delimited) and control the local engine.
func runMcpServerSubcommand(ctx context.Context, parsed ParsedCommandLine, streams Streams) int {
	for _, arg := range parsed.SubcommandArgs {
		switch arg {
		case "-h", "--help":
			printMcpServerHelp(streams.Stdout)
			return 0
		case "--strict-config":
			// Accepted for compatibility; strict config validation is owned by the
			// config loader and applied when a real config is built.
		default:
			fmt.Fprintf(streams.Stderr, "error: unexpected argument: %s\n", arg)
			return 1
		}
	}

	if err := mcpserver.Run(ctx, mcpserver.RunConfig{
		ServerVersion: Version,
		Stdin:         streams.Stdin,
		Stdout:        streams.Stdout,
		CodexHome:     resolveCodexHome(),
		DefaultModel:  "gpt-mock",
		DefaultCwd:    resolveCwd(),
		UserAgent:     "codex-cli-go",
	}); err != nil {
		fmt.Fprintf(streams.Stderr, "codex mcp-server: %v\n", err)
		return 1
	}
	return 0
}

func printMcpServerHelp(w io.Writer) {
	fmt.Fprintln(w, "Start Codex as an MCP server (stdio)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: codex mcp-server [--strict-config]")
}
