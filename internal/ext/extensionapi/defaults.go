package extensionapi

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// BaseThreadLifecycleContributor provides no-op defaults for every
// [ThreadLifecycleContributor] callback, mirroring the empty default method
// bodies on the Rust trait. Embed it and override only the callbacks an
// extension needs.
type BaseThreadLifecycleContributor[C any] struct{}

// OnThreadStart does nothing.
func (BaseThreadLifecycleContributor[C]) OnThreadStart(context.Context, ThreadStartInput[C]) {}

// OnThreadResume does nothing.
func (BaseThreadLifecycleContributor[C]) OnThreadResume(context.Context, ThreadResumeInput) {}

// OnThreadIdle does nothing.
func (BaseThreadLifecycleContributor[C]) OnThreadIdle(context.Context, ThreadIdleInput) {}

// OnThreadStop does nothing.
func (BaseThreadLifecycleContributor[C]) OnThreadStop(context.Context, ThreadStopInput) {}

// BaseTurnLifecycleContributor provides no-op defaults for every
// [TurnLifecycleContributor] callback, mirroring the empty default method bodies
// on the Rust trait.
type BaseTurnLifecycleContributor struct{}

// OnTurnStart does nothing.
func (BaseTurnLifecycleContributor) OnTurnStart(context.Context, TurnStartInput) {}

// OnTurnStop does nothing.
func (BaseTurnLifecycleContributor) OnTurnStop(context.Context, TurnStopInput) {}

// OnTurnAbort does nothing.
func (BaseTurnLifecycleContributor) OnTurnAbort(context.Context, TurnAbortInput) {}

// OnTurnError does nothing.
func (BaseTurnLifecycleContributor) OnTurnError(context.Context, TurnErrorInput) {}

// BaseConfigContributor provides a no-op default for [ConfigContributor],
// mirroring the empty default method body on the Rust trait.
type BaseConfigContributor[C any] struct{}

// OnConfigChanged does nothing.
func (BaseConfigContributor[C]) OnConfigChanged(*ExtensionData, *ExtensionData, C, C) {}

// BaseTokenUsageContributor provides a no-op default for
// [TokenUsageContributor], mirroring the empty default method body on the Rust
// trait.
type BaseTokenUsageContributor struct{}

// OnTokenUsage does nothing.
func (BaseTokenUsageContributor) OnTokenUsage(context.Context, *ExtensionData, *ExtensionData, *ExtensionData, protocol.TokenUsageInfo) {
}

// BaseToolLifecycleContributor provides no-op defaults for the
// [ToolLifecycleContributor] callbacks, mirroring the default method bodies on
// the Rust trait (both default to a ready future).
type BaseToolLifecycleContributor struct{}

// OnToolStart does nothing.
func (BaseToolLifecycleContributor) OnToolStart(context.Context, ToolStartInput) {}

// OnToolFinish does nothing.
func (BaseToolLifecycleContributor) OnToolFinish(context.Context, ToolFinishInput) {}
