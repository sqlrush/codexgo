// Command codex-tui launches the Codex terminal UI against an in-process engine.
//
// By default it wires a mock model so the TUI is runnable end-to-end without any
// network or credentials: typing a message streams a canned agent reply. The
// mock path exercises the streaming-markdown, history-cell, and composer surfaces
// the chat area implements. A real model can be wired by replacing the
// ModelClientFactory.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverclient"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "codex-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build a mock-backed in-process engine. Replace the factory with a real
	// model client to drive a live conversation.
	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
			slug := cfg.Model()
			if slug == "" {
				slug = "gpt-mock"
			}
			return core.NewMockModelClient(slug, nil, mockTurns()...), nil
		},
		CodexHome:    codexHome(),
		DefaultModel: "gpt-mock",
	})
	if err != nil {
		return fmt.Errorf("assemble engine: %w", err)
	}

	client := appserverclient.StartInProcess(ctx, appserverclient.InProcessStartArgs{
		Assembly: asm,
		Defaults: appserver.Defaults{
			Model:      "gpt-mock",
			ProviderID: "openai",
			Cwd:        workdir(),
			UserAgent:  "codex-go-tui/0.136.0",
		},
	})
	defer client.Shutdown(context.WithoutCancel(ctx))

	if err := tui.Run(ctx, tui.RunConfig{
		Client:  client,
		Workdir: workdir(),
	}); err != nil {
		return fmt.Errorf("run tui: %w", err)
	}
	return nil
}

// mockTurns returns a streamed canned agent reply with a small markdown body so
// the chat surface exercises streaming, headings, lists, and a fenced code block.
func mockTurns() []core.MockTurn {
	reply := strings.Join([]string{
		"## Hello from the mock model\n\n",
		"Here is a short list:\n\n",
		"- first item\n",
		"- second item\n\n",
		"And some code:\n\n",
		"```go\n",
		"func main() {\n",
		"\tfmt.Println(\"hi\")\n",
		"}\n",
		"```\n",
	}, "")

	var events []api.ResponseEvent
	for _, chunk := range chunkBy(reply, 8) {
		events = append(events, api.ResponseEvent{Kind: api.ResponseEventOutputTextDelta, Delta: chunk})
	}
	events = append(events, api.ResponseEvent{Kind: api.ResponseEventCompleted, ResponseID: "mock-turn"})

	// A second identical turn lets the user send more than one message.
	turn := core.MockTurn{Events: events}
	return []core.MockTurn{turn, turn, turn, turn}
}

// chunkBy splits s into chunks of at most n runes, keeping multibyte runes whole.
func chunkBy(s string, n int) []string {
	runes := []rune(s)
	var out []string
	for i := 0; i < len(runes); i += n {
		end := i + n
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

func workdir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func codexHome() string {
	if home := os.Getenv("CODEXGO_HOME"); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.codexgo"
	}
	return "."
}
