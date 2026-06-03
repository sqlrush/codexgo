package appserver

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// defaultThreadIDFactory returns a [core.ThreadIDFactory] that mints unique,
// monotonically-increasing thread ids. The Rust engine uses UUIDv7; here we use
// a process-unique prefix plus an atomic counter so ids are unique within a
// process. Swap for a real UUIDv7 generator when one lands in a shared util.
func defaultThreadIDFactory() core.ThreadIDFactory {
	var counter atomic.Uint64
	return func() protocol.ThreadID {
		n := counter.Add(1)
		return protocol.NewThreadID(fmt.Sprintf("thread-%020d", n))
	}
}

// staticModelsManager is a minimal [core.ModelsManager] backed by a single
// default model slug. It satisfies the small DI interface core needs for the
// turn-running path without pulling in the full models catalog. The real
// internal/modelsmanager managers can be injected through
// [AssemblyConfig.ToolRouterFactory]-style seams when richer metadata is needed.
type staticModelsManager struct {
	defaultSlug string
}

// newStaticModelsManager builds a models manager with the given default slug.
func newStaticModelsManager(defaultSlug string) *staticModelsManager {
	return &staticModelsManager{defaultSlug: defaultSlug}
}

// compile-time assertion that staticModelsManager satisfies core.ModelsManager.
var _ core.ModelsManager = (*staticModelsManager)(nil)

// ModelInfo returns opaque model metadata for slug. Core stores the value on the
// turn context without interpreting it, so a nil payload is acceptable here.
func (m *staticModelsManager) ModelInfo(_ context.Context, slug string) (any, error) {
	return map[string]string{"slug": slug}, nil
}

// DefaultModelSlug returns the configured default model slug.
func (m *staticModelsManager) DefaultModelSlug() string { return m.defaultSlug }
