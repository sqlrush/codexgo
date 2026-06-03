package cli

import (
	"context"
	"os"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// defaultMockReply is the canned assistant reply used when CODEX_EXEC_MOCK_REPLY
// is unset, matching the existing codex-exec binary so the engine produces a
// complete turn out of the box without credentials.
const defaultMockReply = "Hello from codex (mock model). Set a real model client to run live."

// buildAssembly constructs the Codex engine wired to a scripted mock model. This
// mirrors the wiring in cmd/codex-exec: a real provider-backed model client is
// owned by the api/models-manager area and is injected by replacing the
// ModelClientFactory once it lands. The mock replays a one-shot turn so the exec
// and mcp-server/app-server paths run end-to-end.
func buildAssembly() (*appserver.Assembly, error) {
	reply := os.Getenv("CODEX_EXEC_MOCK_REPLY")
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
		CodexHome:    resolveCodexHome(),
		DefaultModel: "gpt-mock",
	})
}

// mockTurn builds a scripted assistant turn that emits a single message and ends,
// matching the codex-exec binary's mockTurn.
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

// resolveCodexHome resolves the Codex configuration directory, falling back to
// ".codex" when neither CODEX_HOME nor the home directory is available.
func resolveCodexHome() string {
	if home, err := config.FindCodexHome(); err == nil {
		return home
	}
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	return ".codex"
}

// resolveCwd returns the current working directory, defaulting to "." when it
// cannot be determined.
func resolveCwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
