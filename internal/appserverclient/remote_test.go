package appserverclient

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// startStdioServer wires an appserver stdio transport to in-memory pipes and
// returns a remote client driving it. The server reads client requests from one
// pipe and writes responses/notifications to the other.
func startStdioServer(t *testing.T, ctx context.Context, turns ...core.MockTurn) *RemoteAppServerClient {
	t.Helper()
	asm, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
			slug := cfg.Model()
			if slug == "" {
				slug = "gpt-test"
			}
			return core.NewMockModelClient(slug, nil, turns...), nil
		},
		CodexHome:    "/home/.codex",
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// clientToServer carries client requests; serverToClient carries server
	// responses/notifications.
	c2sRead, c2sWrite := io.Pipe()
	s2cRead, s2cWrite := io.Pipe()

	go func() {
		_ = appserver.ServeStdioWithProcessor(ctx, asm, appserver.Defaults{
			Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "codex-go-remote-test",
		}, c2sRead, s2cWrite)
		_ = s2cWrite.Close()
	}()

	client := StartRemote(s2cRead, c2sWrite)
	t.Cleanup(func() {
		client.Shutdown()
		_ = c2sWrite.Close()
	})
	return client
}

func TestRemoteFullTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := startStdioServer(t, ctx, completedTurn("Hi from remote"))

	// initialize
	var initResp appserverproto.InitializeResponse
	if err := client.RequestTyped(ctx, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: "remote", Version: "1.0"},
	}, &initResp); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if initResp.CodexHome != protocol.AbsolutePath("/home/.codex") {
		t.Fatalf("CodexHome = %q", initResp.CodexHome)
	}

	// thread/start
	var startResp appserverproto.ThreadStartResponse
	if err := client.RequestTyped(ctx, "thread/start", appserverproto.ThreadStartParams{}, &startResp); err != nil {
		t.Fatalf("thread/start: %v", err)
	}
	var agg struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(startResp.Thread, &agg); err != nil {
		t.Fatalf("decode thread aggregate: %v", err)
	}

	// Consume SessionConfigured.
	waitForCodexEvent(t, ctx, client, protocol.EventMsgKindSessionConfigured)

	// turn/start
	var turnResp appserverproto.TurnStartResponse
	if err := client.RequestTyped(ctx, "turn/start", appserverproto.TurnStartParams{
		ThreadID: agg.ID,
		Input:    []appserverproto.UserInput{{Kind: appserverproto.UserInputKindText, Text: "hi"}},
	}, &turnResp); err != nil {
		t.Fatalf("turn/start: %v", err)
	}

	// Observe the turn completing.
	waitForCodexEvent(t, ctx, client, protocol.EventMsgKindTurnComplete)
}

func TestRemoteUnknownMethodReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := startStdioServer(t, ctx)
	if err := client.RequestTyped(ctx, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: "remote", Version: "1.0"},
	}, &appserverproto.InitializeResponse{}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	res, err := client.Request(ctx, "no/such/method", nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !res.IsError() {
		t.Fatal("unknown method should error")
	}
	if res.Error.Code != appserver.MethodNotFoundCode {
		t.Fatalf("want method-not-found, got %d", res.Error.Code)
	}
}

// waitForCodexEvent blocks until the client emits a codex/event of the target
// kind or ctx ends.
func waitForCodexEvent(t *testing.T, ctx context.Context, client *RemoteAppServerClient, target protocol.EventMsgKind) {
	t.Helper()
	for {
		ev, ok := client.NextEvent(ctx)
		if !ok {
			t.Fatalf("event channel closed before %s", target)
		}
		if !ev.IsCodexEvent() {
			continue
		}
		var coreEvent protocol.Event
		if err := json.Unmarshal(ev.Notification.Params, &coreEvent); err != nil {
			t.Fatalf("decode codex event: %v", err)
		}
		if coreEvent.Msg.Type == target {
			return
		}
	}
}
