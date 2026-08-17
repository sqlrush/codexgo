package extensionapi

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// fullContributor implements every contributor interface by embedding the base
// types and the C-typed lifecycle/config contributors.
type fullContributor struct {
	BaseThreadLifecycleContributor[struct{}]
	BaseTurnLifecycleContributor
	BaseConfigContributor[struct{}]
	BaseTokenUsageContributor
	BaseToolLifecycleContributor
}

func (fullContributor) Contribute(context.Context, *ExtensionData, *ExtensionData) []PromptFragment {
	return []PromptFragment{DeveloperPolicyFragment("p")}
}

func (fullContributor) Tools(*ExtensionData, *ExtensionData) []tools.ToolExecutor[tools.ToolCall] {
	return nil
}

type fullTurnItemContributor struct{}

func (fullTurnItemContributor) Contribute(context.Context, *ExtensionData, *ExtensionData, *protocol.TurnItem) error {
	return nil
}

func TestRegistryRegistersAndExposesEveryContributorKind(t *testing.T) {
	c := fullContributor{}
	b := NewExtensionRegistryBuilder[struct{}]()
	b.AddThreadLifecycleContributor(c)
	b.AddTurnLifecycleContributor(c)
	b.AddConfigContributor(c)
	b.AddTokenUsageContributor(c)
	b.AddPromptContributor(c)
	b.AddToolContributor(c)
	b.AddToolLifecycleContributor(c)
	b.AddTurnItemContributor(fullTurnItemContributor{})
	reg := b.Build()

	if len(reg.ThreadLifecycleContributors()) != 1 {
		t.Error("thread lifecycle")
	}
	if len(reg.TurnLifecycleContributors()) != 1 {
		t.Error("turn lifecycle")
	}
	if len(reg.ConfigContributors()) != 1 {
		t.Error("config")
	}
	if len(reg.TokenUsageContributors()) != 1 {
		t.Error("token usage")
	}
	if len(reg.ContextContributors()) != 1 {
		t.Error("context")
	}
	if len(reg.ToolContributors()) != 1 {
		t.Error("tool")
	}
	if len(reg.ToolLifecycleContributors()) != 1 {
		t.Error("tool lifecycle")
	}
	if len(reg.TurnItemContributors()) != 1 {
		t.Error("turn item")
	}
}

func TestBaseContributorsAreNoOps(t *testing.T) {
	c := fullContributor{}
	ctx := context.Background()
	s := NewExtensionData("s")
	// None of these should panic; they are the inherited no-op defaults.
	c.OnThreadStart(ctx, ThreadStartInput[struct{}]{ThreadStore: s})
	c.OnThreadResume(ctx, ThreadResumeInput{ThreadStore: s})
	c.OnThreadIdle(ctx, ThreadIdleInput{ThreadStore: s})
	c.OnThreadStop(ctx, ThreadStopInput{ThreadStore: s})
	c.OnTurnStart(ctx, TurnStartInput{ThreadStore: s})
	c.OnTurnStop(ctx, TurnStopInput{ThreadStore: s})
	c.OnTurnAbort(ctx, TurnAbortInput{ThreadStore: s})
	c.OnTurnError(ctx, TurnErrorInput{ThreadStore: s})
	c.OnConfigChanged(s, s, struct{}{}, struct{}{})
	c.OnTokenUsage(ctx, s, s, s, protocol.TokenUsageInfo{})
	c.OnToolStart(ctx, ToolStartInput{ThreadStore: s})
	c.OnToolFinish(ctx, ToolFinishInput{ThreadStore: s})

	// Verify it satisfies all the interfaces it embeds.
	var (
		_ ThreadLifecycleContributor[struct{}] = c
		_ TurnLifecycleContributor             = c
		_ ConfigContributor[struct{}]          = c
		_ TokenUsageContributor                = c
		_ ContextContributor                   = c
		_ ToolContributor                      = c
		_ ToolLifecycleContributor             = c
	)
}

func TestToolCallSourceConstructors(t *testing.T) {
	direct := DirectToolCallSource()
	if direct.Kind != ToolCallSourceDirect {
		t.Error("direct kind")
	}
	code := CodeModeToolCallSource("cell", "rtid")
	if code.Kind != ToolCallSourceCodeMode || code.CellID != "cell" || code.RuntimeToolCallID != "rtid" {
		t.Errorf("code-mode source = %+v", code)
	}
}

func TestToolCallOutcomeConstructors(t *testing.T) {
	if o := CompletedToolCallOutcome(true); o.Kind != ToolCallOutcomeCompleted || !o.Success {
		t.Errorf("completed = %+v", o)
	}
	if o := BlockedToolCallOutcome(); o.Kind != ToolCallOutcomeBlocked {
		t.Errorf("blocked = %+v", o)
	}
	if o := FailedToolCallOutcome(true); o.Kind != ToolCallOutcomeFailed || !o.HandlerExecuted {
		t.Errorf("failed = %+v", o)
	}
	if o := AbortedToolCallOutcome(); o.Kind != ToolCallOutcomeAborted {
		t.Errorf("aborted = %+v", o)
	}
}

func TestAgentSpawnerFunc(t *testing.T) {
	var gotThread protocol.ThreadID
	spawner := AgentSpawnerFunc[string, int](func(_ context.Context, threadID protocol.ThreadID, req string) (int, error) {
		gotThread = threadID
		return len(req), nil
	})
	out, err := spawner.SpawnSubagent(context.Background(), protocol.NewThreadID("tid"), "abcd")
	if err != nil || out != 4 {
		t.Fatalf("out=%d err=%v", out, err)
	}
	if gotThread.String() != "tid" {
		t.Errorf("thread = %q", gotThread)
	}
}
