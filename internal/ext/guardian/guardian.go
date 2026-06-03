// Package guardian is the faithful Go port of codex's ext/guardian crate: the
// automated security reviewer wiring. It installs a thread-lifecycle
// contributor that captures the thread a guardian subagent should fork from and
// delegates subagent spawning to a host-provided spawner.
//
// The guardian's review of tool use and patches is surfaced to the approval
// flow as protocol.GuardianAssessmentEvent values; the canonical assessment
// types live in internal/protocol and are not redefined here.
package guardian

import (
	"context"

	"github.com/sqlrush/codexgo/internal/ext/extensionapi"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// GuardianExtension holds the guardian extension dependencies supplied by the
// host at construction time. Mirrors the Rust `GuardianExtension<S>`.
//
// The type parameters mirror the Rust `AgentSpawner<R>` associated types: R is
// the host-owned subagent request shape and S is the spawned handle type. C is
// the host configuration type the registry is built with; the Rust crate
// specializes this to `codex_core::config::Config`, but the guardian's
// thread-start hook never reads the config, so it is left generic here.
type GuardianExtension[C any, R any, S any] struct {
	extensionapi.BaseThreadLifecycleContributor[C]
	agentSpawner extensionapi.AgentSpawner[R, S]
}

// NewGuardianExtension creates a guardian extension with its host-provided agent
// spawn helper. Mirrors Rust `GuardianExtension::new`.
func NewGuardianExtension[C any, R any, S any](agentSpawner extensionapi.AgentSpawner[R, S]) *GuardianExtension[C, R, S] {
	return &GuardianExtension[C, R, S]{agentSpawner: agentSpawner}
}

// SpawnSubagent delegates one guardian-owned subagent spawn request to the host
// helper. Mirrors Rust `GuardianExtension::spawn_subagent`.
func (g *GuardianExtension[C, R, S]) SpawnSubagent(ctx context.Context, forkedFromThreadID protocol.ThreadID, request R) (S, error) {
	return g.agentSpawner.SpawnSubagent(ctx, forkedFromThreadID, request)
}

// GuardianThreadContext is the thread-local guardian state captured when the
// host starts a thread. Mirrors the Rust `GuardianThreadContext`.
type GuardianThreadContext struct {
	forkedFromThreadID protocol.ThreadID
}

// ForkedFromThreadID returns the thread that future guardian subagents should
// fork from by default. Mirrors Rust `GuardianThreadContext::forked_from_thread_id`.
func (c GuardianThreadContext) ForkedFromThreadID() protocol.ThreadID {
	return c.forkedFromThreadID
}

// OnThreadStart captures the forked-from thread id into the thread store so
// later guardian subagents know which thread to fork from. Mirrors the Rust
// `ThreadLifecycleContributor::on_thread_start` impl.
//
// The remaining ThreadLifecycleContributor callbacks default to no-ops via the
// embedded BaseThreadLifecycleContributor.
func (g *GuardianExtension[C, R, S]) OnThreadStart(_ context.Context, input extensionapi.ThreadStartInput[C]) {
	levelID := input.ThreadStore.LevelID()
	if levelID == "" {
		// Mirror the Rust early return when the level id is not a thread id.
		return
	}
	forkedFromThreadID := protocol.NewThreadID(levelID)
	extensionapi.ExtensionDataInsert(input.ThreadStore, GuardianThreadContext{
		forkedFromThreadID: forkedFromThreadID,
	})
}

// Install registers the guardian contributors into the extension registry,
// constructing a GuardianExtension around the host-provided spawner. Mirrors
// the Rust `install` free function.
func Install[C any, R any, S any](registry *extensionapi.ExtensionRegistryBuilder[C], agentSpawner extensionapi.AgentSpawner[R, S]) {
	registry.AddThreadLifecycleContributor(NewGuardianExtension[C](agentSpawner))
}
