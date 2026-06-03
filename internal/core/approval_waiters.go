package core

import (
	"sync"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file owns the auxiliary pending-waiter registries used by the approval
// routing path for the request flows that the foundation's [TurnState] does not
// already cover. The foundation wires the exec/patch [protocol.ReviewDecision]
// waiter map directly (TurnState.InsertPendingApproval / ResolvePendingApproval);
// here we add equivalent, independently-synchronized registries for the
// request_permissions, request_user_input, and elicitation flows.
//
// Rather than mutating the foundation [Session] / [ActiveTurn] / [TurnState]
// structs (owned by sibling agents), the registries are stored in a
// package-level table keyed by the *Session pointer. This keeps the approval
// area self-contained while preserving the Rust ordering guarantees: a waiter is
// registered before the request event is emitted, and a response either resolves
// the matching waiter or is dropped with a no-op when none is pending.

// waiterRegistry holds the per-session pending waiters for the three auxiliary
// approval request flows. Each map is keyed by the same correlation key the Rust
// engine uses (call_id for permissions, sub_id for user input, and a
// server+request-id pair for elicitations).
//
// All access is serialized by mu. Channels are buffered (capacity 1) so a
// resolver never blocks delivering a single response, mirroring the Rust
// oneshot::channel send semantics.
type waiterRegistry struct {
	mu sync.Mutex

	permissions map[string]chan protocol.RequestPermissionsResponse
	userInput   map[string]chan protocol.RequestUserInputResponse
	elicitation map[string]chan ElicitationResponse
}

// newWaiterRegistry builds an empty registry with all maps initialized.
func newWaiterRegistry() *waiterRegistry {
	return &waiterRegistry{
		permissions: make(map[string]chan protocol.RequestPermissionsResponse),
		userInput:   make(map[string]chan protocol.RequestUserInputResponse),
		elicitation: make(map[string]chan ElicitationResponse),
	}
}

// sessionWaiters is the process-global table mapping a [Session] to its auxiliary
// waiter registry. It is created lazily on first use and torn down by
// dropSessionWaiters when a session shuts down.
var sessionWaiters sync.Map // map[*Session]*waiterRegistry

// waitersFor returns the registry for s, creating it on first use.
func waitersFor(s *Session) *waiterRegistry {
	if existing, ok := sessionWaiters.Load(s); ok {
		return existing.(*waiterRegistry)
	}
	reg := newWaiterRegistry()
	actual, _ := sessionWaiters.LoadOrStore(s, reg)
	return actual.(*waiterRegistry)
}

// dropSessionWaiters removes and closes every pending waiter for s. It is safe
// to call multiple times and should be invoked during session teardown so any
// blocked routing goroutine observes a closed channel and unblocks.
func dropSessionWaiters(s *Session) {
	v, ok := sessionWaiters.LoadAndDelete(s)
	if !ok {
		return
	}
	reg := v.(*waiterRegistry)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for k, ch := range reg.permissions {
		close(ch)
		delete(reg.permissions, k)
	}
	for k, ch := range reg.userInput {
		close(ch)
		delete(reg.userInput, k)
	}
	for k, ch := range reg.elicitation {
		close(ch)
		delete(reg.elicitation, k)
	}
}

// elicitationKey builds the composite key used to correlate an elicitation
// request with its response, mirroring the Rust (server_name, request_id) pair.
func elicitationKey(serverName string, id protocol.RequestId) string {
	return serverName + "\x00" + id.String()
}

// --- request_permissions waiters -------------------------------------------

// insertPendingPermissions registers a waiter for a permissions request keyed by
// callID and returns the receive channel plus any replaced channel (so the
// caller can warn about an overwrite, matching the Rust behavior).
func (r *waiterRegistry) insertPendingPermissions(callID string) (<-chan protocol.RequestPermissionsResponse, chan protocol.RequestPermissionsResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var replaced chan protocol.RequestPermissionsResponse
	if prev, ok := r.permissions[callID]; ok {
		replaced = prev
	}
	ch := make(chan protocol.RequestPermissionsResponse, 1)
	r.permissions[callID] = ch
	return ch, replaced
}

// removePendingPermissions detaches and returns the waiter for callID, if any.
func (r *waiterRegistry) removePendingPermissions(callID string) (chan protocol.RequestPermissionsResponse, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.permissions[callID]
	if ok {
		delete(r.permissions, callID)
	}
	return ch, ok
}

// --- request_user_input waiters --------------------------------------------

// insertPendingUserInput registers a waiter for a user-input request keyed by
// subID.
func (r *waiterRegistry) insertPendingUserInput(subID string) (<-chan protocol.RequestUserInputResponse, chan protocol.RequestUserInputResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var replaced chan protocol.RequestUserInputResponse
	if prev, ok := r.userInput[subID]; ok {
		replaced = prev
	}
	ch := make(chan protocol.RequestUserInputResponse, 1)
	r.userInput[subID] = ch
	return ch, replaced
}

// removePendingUserInput detaches and returns the waiter for subID, if any.
func (r *waiterRegistry) removePendingUserInput(subID string) (chan protocol.RequestUserInputResponse, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.userInput[subID]
	if ok {
		delete(r.userInput, subID)
	}
	return ch, ok
}

// --- elicitation waiters ---------------------------------------------------

// insertPendingElicitation registers a waiter for an elicitation request keyed
// by the composite server+request-id key.
func (r *waiterRegistry) insertPendingElicitation(key string) (<-chan ElicitationResponse, chan ElicitationResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var replaced chan ElicitationResponse
	if prev, ok := r.elicitation[key]; ok {
		replaced = prev
	}
	ch := make(chan ElicitationResponse, 1)
	r.elicitation[key] = ch
	return ch, replaced
}

// removePendingElicitation detaches and returns the waiter for key, if any.
func (r *waiterRegistry) removePendingElicitation(key string) (chan ElicitationResponse, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.elicitation[key]
	if ok {
		delete(r.elicitation, key)
	}
	return ch, ok
}
