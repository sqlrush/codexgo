package cli

import (
	"sync"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// goalEventSink is the headless host's [extensionapi.ExtensionEventSink] for the
// goal extension: it routes a goal-accounting event (thread_goal_updated) to the
// owning thread's session event stream. It is the Go analogue of the Rust
// app-server AppServerExtensionEventSink, which forwards ThreadGoalUpdated to the
// outgoing notification channel; here the headless exec path's transport is the
// session event channel, so the event is delivered via Session.EmitEvent.
//
// The goal tool executors are constructed per-thread in the CLI
// ToolRouterFactory BEFORE the session exists, so the sink resolves the session
// lazily at Emit time through a late-bound ThreadManager (the same holder pattern
// used for the multi-agent collabEngine). Until the manager is published, or
// before the thread's session is registered, Emit is a no-op — matching the
// fire-and-forget contract of the sink (the Rust try_send drops on a closed
// channel too).
type goalEventSink struct {
	threadID protocol.ThreadID
	holder   *threadManagerHolder
}

// threadManagerHolder is the guarded late-binding cell for the process-wide
// ThreadManager. The manager only exists after appserver.Assemble returns, while
// the per-thread sink (built inside ToolRouterFactory) closes over the holder.
// It mirrors the collabEngine holder in buildAssemblyWithDefaults.
type threadManagerHolder struct {
	mu  sync.Mutex
	mgr *core.ThreadManager
}

// set publishes the assembled ThreadManager so late-bound sinks can resolve it.
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

// newGoalEventSink builds a late-binding sink for one thread over the shared
// ThreadManager holder.
func newGoalEventSink(threadID protocol.ThreadID, holder *threadManagerHolder) goalEventSink {
	return goalEventSink{threadID: threadID, holder: holder}
}

// Emit routes a pre-built extension event to the thread's session event stream.
// It resolves the session via the late-bound ThreadManager and forwards the
// event verbatim (preserving its correlation id) through Session.EmitEvent. A
// missing manager / thread / session drops the event, matching the
// fire-and-forget sink contract.
func (s goalEventSink) Emit(event protocol.Event) {
	mgr := s.holder.get()
	if mgr == nil {
		return
	}
	thread, err := mgr.GetThread(s.threadID)
	if err != nil || thread == nil {
		return
	}
	sess := thread.Codex().Session()
	if sess == nil {
		return
	}
	sess.EmitEvent(event)
}
