package extensionapi

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// ContextContributor adds prompt fragments during prompt assembly. Mirrors the
// Rust `ContextContributor` trait.
type ContextContributor interface {
	// Contribute returns the prompt fragments this contributor adds for the
	// supplied extension stores.
	Contribute(ctx context.Context, sessionStore, threadStore *ExtensionData) []PromptFragment
}

// ThreadLifecycleContributor handles host-owned thread lifecycle gates. Mirrors
// the Rust `ThreadLifecycleContributor<C>` trait.
//
// Implementations should use these callbacks to seed, rehydrate, or flush
// extension-private thread state. Heavy dependencies belong on the extension
// value created by the host, not in these inputs. The type parameter C carries
// the host configuration type so OnThreadStart observes it.
type ThreadLifecycleContributor[C any] interface {
	// OnThreadStart is called after thread-scoped extension stores are created,
	// before later contributors can read from them.
	OnThreadStart(ctx context.Context, input ThreadStartInput[C])

	// OnThreadResume is called after the host constructs a runtime from
	// persisted history.
	OnThreadResume(ctx context.Context, input ThreadResumeInput)

	// OnThreadIdle is called after the host has drained immediately pending
	// thread work.
	OnThreadIdle(ctx context.Context, input ThreadIdleInput)

	// OnThreadStop is called before the host drops the thread runtime and
	// thread-scoped store.
	OnThreadStop(ctx context.Context, input ThreadStopInput)
}

// TurnLifecycleContributor handles host-owned turn lifecycle gates. Mirrors the
// Rust `TurnLifecycleContributor` trait.
type TurnLifecycleContributor interface {
	// OnTurnStart is called after turn-scoped extension stores are created,
	// before the task for the turn starts running.
	OnTurnStart(ctx context.Context, input TurnStartInput)

	// OnTurnStop is called before the host drops the completed turn runtime and
	// turn store.
	OnTurnStop(ctx context.Context, input TurnStopInput)

	// OnTurnAbort is called after the host aborts a running turn.
	OnTurnAbort(ctx context.Context, input TurnAbortInput)

	// OnTurnError is called when the host observes an error for a running turn.
	OnTurnError(ctx context.Context, input TurnErrorInput)
}

// ConfigContributor handles host-owned configuration changes. Mirrors the Rust
// `ConfigContributor<C>` trait.
//
// Implementations should treat the supplied values as immutable before/after
// snapshots of the effective thread configuration.
type ConfigContributor[C any] interface {
	// OnConfigChanged is called after the host commits a changed thread
	// configuration.
	OnConfigChanged(sessionStore, threadStore *ExtensionData, previousConfig, newConfig C)
}

// TokenUsageContributor observes token usage checkpoints reported by the model
// provider. Mirrors the Rust `TokenUsageContributor` trait.
//
// Implementations should keep this callback cheap. The host calls it after
// updating cached token usage and before emitting the corresponding client
// token-count notification.
type TokenUsageContributor interface {
	// OnTokenUsage is called each time the host records token usage from a model
	// response.
	OnTokenUsage(ctx context.Context, sessionStore, threadStore, turnStore *ExtensionData, tokenUsage protocol.TokenUsageInfo)
}

// ToolContributor exposes native tools owned by a feature. Mirrors the Rust
// `ToolContributor` trait.
type ToolContributor interface {
	// Tools returns the native tools visible for the supplied extension stores.
	Tools(sessionStore, threadStore *ExtensionData) []tools.ToolExecutor[tools.ToolCall]
}

// ToolLifecycleContributor handles host-owned tool lifecycle gates. Mirrors the
// Rust `ToolLifecycleContributor` trait.
//
// Implementations should use these callbacks to observe tool execution without
// inspecting or rewriting tool input/output. Use [ToolContributor] for owning a
// tool implementation and hooks for policy that needs tool payloads.
type ToolLifecycleContributor interface {
	// OnToolStart is called once the host has accepted a tool call for
	// execution.
	OnToolStart(ctx context.Context, input ToolStartInput)

	// OnToolFinish is called after the tool call returns, is blocked, fails, or
	// is cancelled.
	OnToolFinish(ctx context.Context, input ToolFinishInput)
}

// ApprovalReviewContributor can claim rendered approval-review prompts. Mirrors
// the Rust `ApprovalReviewContributor` trait.
type ApprovalReviewContributor interface {
	// Contribute returns a review decision when this contributor claims the
	// rendered prompt, or ok=false to defer to later contributors.
	Contribute(ctx context.Context, sessionStore, threadStore *ExtensionData, prompt string) (protocol.ReviewDecision, bool)
}

// TurnItemContributor is an ordered post-processing contribution for one parsed
// turn item. Mirrors the Rust `TurnItemContributor` trait.
//
// Implementations may mutate the item before it is emitted and may use the
// explicitly exposed thread- and turn-lifetime stores when they need durable
// extension-private state.
type TurnItemContributor interface {
	// Contribute post-processes one turn item in place, returning an error
	// string-equivalent via the Go error.
	Contribute(ctx context.Context, threadStore, turnStore *ExtensionData, item *protocol.TurnItem) error
}
