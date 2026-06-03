// Command codex-mcp-server runs Codex as a Model Context Protocol (MCP) server
// over stdio. It is the Go entry point equivalent to the Rust
// codex-rs/mcp-server binary: clients connect over standard MCP (JSON-RPC 2.0,
// line-delimited) and control a local Codex engine via the codex/codex-reply
// tools and the v2/v1 method surface.
//
// The engine is assembled exactly as the app-server assembles it. This binary
// wires a minimal assembly so the server runs; richer config/model resolution is
// owned by the config and models-manager area agents and is injected through the
// app-server assembly seams as it lands.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/sqlrush/codexgo/internal/mcpserver"
)

// version is the codexgo build version. Parity target: codex 0.136.0.
const version = "0.0.0-dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := mcpserver.Run(ctx, mcpserver.RunConfig{
		ServerVersion: version,
		Stdin:         os.Stdin,
		Stdout:        os.Stdout,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "codex-mcp-server: %v\n", err)
		os.Exit(1)
	}
}
