package cli

import (
	"sync"

	"github.com/sqlrush/codexgo/internal/core"
)

// threadManagerHolder is the guarded late-binding cell for the process-wide
// ThreadManager. The manager only exists after appserver.Assemble returns, while
// the per-thread tool router factory (collab control plane) closes over the
// holder. It mirrors the collabEngine holder in buildAssemblyWithDefaults.
type threadManagerHolder struct {
	mu  sync.Mutex
	mgr *core.ThreadManager
}

// set publishes the assembled ThreadManager so late-bound consumers can resolve it.
func (h *threadManagerHolder) set(mgr *core.ThreadManager) {
	h.mu.Lock()
	h.mgr = mgr
	h.mu.Unlock()
}

// get returns the published ThreadManager, or nil before assembly completes.
func (h *threadManagerHolder) get() *core.ThreadManager {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.mgr
}
