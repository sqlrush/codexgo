package extensionapi

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// AgentSpawner is the constructor-injected host helper for extensions that need
// to spawn subagents. Mirrors the Rust `AgentSpawner<R>` trait.
//
// The extension owns the request shape (R) and resulting handle type (S); the
// host provides the implementation when it constructs the extension. The Rust
// associated `Error` type is represented by Go's idiomatic error return.
type AgentSpawner[R any, S any] interface {
	// SpawnSubagent delegates one guardian-owned subagent spawn request to the
	// host helper.
	SpawnSubagent(ctx context.Context, forkedFromThreadID protocol.ThreadID, request R) (S, error)
}

// AgentSpawnerFunc adapts a function to the AgentSpawner interface, mirroring
// the Rust blanket impl for `Fn(ThreadId, R) -> AgentSpawnFuture`.
type AgentSpawnerFunc[R any, S any] func(ctx context.Context, forkedFromThreadID protocol.ThreadID, request R) (S, error)

// SpawnSubagent calls the wrapped function.
func (f AgentSpawnerFunc[R, S]) SpawnSubagent(ctx context.Context, forkedFromThreadID protocol.ThreadID, request R) (S, error) {
	return f(ctx, forkedFromThreadID, request)
}

// ExtensionEventSink is the host-provided fire-and-forget sink for
// extension-generated events. Mirrors the Rust `ExtensionEventSink` trait.
//
// Extensions construct protocol events with the correlation id appropriate for
// the callback they are handling, then leave persistence, ordering, transport
// fanout, and logging decisions to the host.
type ExtensionEventSink interface {
	// Emit queues one protocol event for host-owned delivery.
	Emit(event protocol.Event)
}

// NoopExtensionEventSink is the event sink used when the host does not expose
// extension event emission. Mirrors Rust `NoopExtensionEventSink`.
type NoopExtensionEventSink struct{}

// Emit does nothing.
func (NoopExtensionEventSink) Emit(protocol.Event) {}

// ResponseItemInjector is the host-provided helper for extensions that need to
// steer the active model turn. Mirrors the Rust `ResponseItemInjector` trait.
//
// Implementations should inject the supplied response items into the active turn
// when one can accept same-turn model input. If injection is unavailable, they
// return the unchanged items to the caller (as the error value), mirroring the
// Rust `Result<(), Vec<ResponseInputItem>>`.
type ResponseItemInjector interface {
	// InjectResponseItems injects model-visible input into the active turn,
	// returning the unchanged items when injection is unavailable.
	InjectResponseItems(ctx context.Context, items []tools.ResponseInputItem) ([]tools.ResponseInputItem, error)
}

// NoopResponseItemInjector is the injector used when a host does not expose
// same-turn model steering. Mirrors Rust `NoopResponseItemInjector`.
type NoopResponseItemInjector struct{}

// InjectResponseItems always reports injection unavailable, returning the items
// unchanged alongside ErrInjectionUnavailable.
func (NoopResponseItemInjector) InjectResponseItems(_ context.Context, items []tools.ResponseInputItem) ([]tools.ResponseInputItem, error) {
	return items, ErrInjectionUnavailable
}
