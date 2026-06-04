package appserver

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
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
// default model slug and an optional model catalog. It satisfies the small DI
// interface core needs for the turn-running path; with a catalog supplied it
// resolves the same full per-model metadata the model-client factory uses, so
// the per-turn tool selection (shell_type) sees the real model capabilities.
type staticModelsManager struct {
	defaultSlug string
	catalog     []modelsmanager.ModelInfo
}

// newStaticModelsManager builds a models manager with the given default slug
// and catalog (nil catalog falls back to slug-derived metadata).
func newStaticModelsManager(defaultSlug string, catalog []modelsmanager.ModelInfo) *staticModelsManager {
	return &staticModelsManager{defaultSlug: defaultSlug, catalog: catalog}
}

// compile-time assertion that staticModelsManager satisfies core.ModelsManager.
var _ core.ModelsManager = (*staticModelsManager)(nil)

// ModelInfo returns the typed [modelsmanager.ModelInfo] for slug, resolved from
// the catalog with a slug-derived fallback. Core stores the value opaquely on
// the turn context; the tool-selection helpers type-assert it back.
func (m *staticModelsManager) ModelInfo(_ context.Context, slug string) (any, error) {
	return resolveModelInfo(slug, m.catalog), nil
}

// DefaultModelSlug returns the configured default model slug.
func (m *staticModelsManager) DefaultModelSlug() string { return m.defaultSlug }
