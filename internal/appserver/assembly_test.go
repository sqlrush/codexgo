package appserver

import (
	"context"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// mockClientFactory returns a ModelClientFactory that hands out a fresh
// MockModelClient replaying the given turns for every spawned thread.
func mockClientFactory(turns ...core.MockTurn) ModelClientFactory {
	return func(_ context.Context, _ protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error) {
		slug := cfg.Model()
		if slug == "" {
			slug = "gpt-test"
		}
		return core.NewMockModelClient(slug, nil, turns...), nil
	}
}

// completedTurn builds a one-shot scripted assistant turn that streams a single
// message and ends the turn.
func completedTurn(text string) core.MockTurn {
	mid := "m1"
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
		{Kind: api.ResponseEventCompleted, EndTurn: boolPtr(true)},
	}}
}

func boolPtr(b bool) *bool { return &b }

func TestAssembleValidatesModelClientFactory(t *testing.T) {
	if _, err := Assemble(AssemblyConfig{}); err == nil {
		t.Fatal("Assemble with nil ModelClientFactory should fail")
	}
}

func TestAssembleBuildsThreadManager(t *testing.T) {
	asm, err := Assemble(AssemblyConfig{
		ModelClientFactory: mockClientFactory(),
		CodexHome:          "/home/.codex",
		DefaultModel:       "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if asm.ThreadManager == nil {
		t.Fatal("ThreadManager is nil")
	}
	if asm.ModelsManager == nil {
		t.Fatal("ModelsManager is nil")
	}
	if asm.ModelsManager.DefaultModelSlug() != "gpt-test" {
		t.Fatalf("DefaultModelSlug = %q, want gpt-test", asm.ModelsManager.DefaultModelSlug())
	}
	if asm.CodexHome != "/home/.codex" {
		t.Fatalf("CodexHome = %q", asm.CodexHome)
	}
}

func TestAssembleSpawnsRunnableThread(t *testing.T) {
	asm, err := Assemble(AssemblyConfig{
		ModelClientFactory: mockClientFactory(completedTurn("hi")),
		DefaultModel:       "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	cfg := core.SessionConfiguration{
		ProviderID:        "openai",
		CollaborationMode: protocol.CollaborationMode{Settings: protocol.Settings{Model: "gpt-test"}},
		Cwd:               "/work",
		BaseInstructions:  "be helpful",
	}
	newThread, err := asm.ThreadManager.StartThread(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartThread: %v", err)
	}
	if newThread.Thread == nil {
		t.Fatal("spawned thread is nil")
	}
	// The thread manager consumes SessionConfigured during spawn; it is returned
	// on the ack rather than left on the event queue.
	if newThread.SessionConfigured.Model != "gpt-test" {
		t.Fatalf("SessionConfigured.Model = %q, want gpt-test", newThread.SessionConfigured.Model)
	}

	// Submitting user input drives a turn; the scripted mock completes it, so we
	// should observe TurnComplete.
	if _, err := newThread.Thread.Submit(protocol.Op{
		Type:  protocol.OpUserInput,
		Items: []protocol.UserInput{{Type: protocol.UserInputKindText, Text: "hi"}},
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sawComplete := false
	for !sawComplete {
		ev, err := newThread.Thread.NextEvent(ctx)
		if err != nil {
			t.Fatalf("NextEvent: %v", err)
		}
		if ev.Msg.Type == protocol.EventMsgKindTurnComplete {
			sawComplete = true
		}
	}
	_ = newThread.Thread.ShutdownAndWait(context.Background())
}
