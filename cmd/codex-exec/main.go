// Command codex-exec is the non-interactive Codex agent runner: a Go port of
// `codex exec`. It runs a single agent turn from a prompt (argument or stdin)
// plus optional images and streams the result, either as JSONL (--json) or as a
// human-readable summary with the final message on stdout.
//
// Usage:
//
//	codex-exec [OPTIONS] [PROMPT]
//	codex-exec [OPTIONS] resume [SESSION_ID] [PROMPT]
//	codex-exec [OPTIONS] review (--uncommitted | --base BRANCH | --commit SHA | PROMPT)
//
// Options:
//
//	--json                     Emit events as JSONL (one per line).
//	-o, --output-last-message  Write the final agent message to FILE.
//	--output-schema FILE       Constrain the model's final response to a JSON Schema.
//	-i, --image FILE[,FILE...] Attach local image(s) to the prompt.
//
// Model wiring: by default codex-exec runs against a scripted mock model so the
// binary is runnable without credentials (e.g. `go run ./cmd/codex-exec --json
// "hello"`). Set CODEXGO_EXEC_MOCK_REPLY to control the canned assistant reply.
// A real provider-backed model client can be substituted by replacing the
// ModelClientFactory in buildAssembly.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/exec"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// defaultMockReply is the canned assistant reply used when CODEXGO_EXEC_MOCK_REPLY
// is unset, so the binary produces a complete turn out of the box.
const defaultMockReply = "Hello from codex-exec (mock model). Set a real model client to run live."

func main() {
	cli, err := exec.ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	asm, err := buildAssembly()
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-exec:", err)
		os.Exit(1)
	}

	env := exec.Environment{
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		StdinIsTerminal: isTerminal(os.Stdin),
		Assembly:        asm,
		Defaults: appserver.Defaults{
			Model:      "gpt-mock",
			ProviderID: "openai",
			Cwd:        mustGetwd(),
			UserAgent:  "codex-exec-go",
		},
	}

	code := exec.Run(context.Background(), cli, env)
	os.Exit(code)
}

// buildAssembly constructs the Codex engine wired to a scripted mock model. The
// mock replays a one-shot turn that streams the canned reply and ends the turn,
// which the engine surfaces as task_started / agent_message / task_complete
// events — enough to exercise the full exec JSONL path end-to-end.
func buildAssembly() (*appserver.Assembly, error) {
	reply := os.Getenv("CODEXGO_EXEC_MOCK_REPLY")
	if reply == "" {
		reply = defaultMockReply
	}
	return appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
			slug := cfg.Model()
			if slug == "" {
				slug = "gpt-mock"
			}
			return core.NewMockModelClient(slug, nil, mockTurn(reply)), nil
		},
		CodexHome:    codexHome(),
		DefaultModel: "gpt-mock",
	})
}

// mockTurn builds a scripted assistant turn that emits a single message and ends.
func mockTurn(text string) core.MockTurn {
	mid := "m1"
	end := true
	return core.MockTurn{Events: []api.ResponseEvent{
		{Kind: api.ResponseEventCreated},
		{
			Kind: api.ResponseEventOutputItemDone,
			Item: &protocol.ResponseItem{
				Type:      protocol.ResponseItemKindMessage,
				Role:      "assistant",
				MessageID: &mid,
				Content:   []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
			},
		},
		{Kind: api.ResponseEventCompleted, EndTurn: &end},
	}}
}

// isTerminal reports whether f is an interactive character device.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// mustGetwd returns the current working directory, falling back to "." when it
// cannot be determined.
func mustGetwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// codexHome resolves the codexgo configuration directory from CODEXGO_HOME or
// ~/.codexgo, defaulting to ".codexgo" when neither is available.
func codexHome() string {
	if home := os.Getenv("CODEXGO_HOME"); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.codexgo"
	}
	return ".codexgo"
}
