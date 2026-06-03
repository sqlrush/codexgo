package realtimeconv

import (
	"context"
	"fmt"
	"strings"
)

// activeResponseErrorPrefix is the server error prefix that signals a
// response.create raced an already-active response. Mirrors the Rust
// REALTIME_ACTIVE_RESPONSE_ERROR_PREFIX.
const activeResponseErrorPrefix = "Conversation already has an active response in progress:"

// responseCreateQueue serializes V2 response.create requests so at most one
// default response is in flight; a request issued while one is active is
// deferred until the active response finishes. Mirrors the Rust
// RealtimeResponseCreateQueue.
//
// It is not safe for concurrent use; the conversation loop owns it and accesses
// it from a single goroutine.
type responseCreateQueue struct {
	activeDefaultResponse bool
	pendingCreate         bool
}

// requestCreate asks for a response.create. If a default response is already
// active the request is deferred; otherwise it is sent immediately. Mirrors the
// Rust request_create.
func (q *responseCreateQueue) requestCreate(
	ctx context.Context,
	writer Writer,
	eventsTx chan<- Event,
	reason string,
) error {
	if q.activeDefaultResponse {
		q.pendingCreate = true
		return nil
	}
	return q.sendCreateNow(ctx, writer, eventsTx, reason)
}

// markStarted records that a response is now active. Mirrors the Rust
// mark_started.
func (q *responseCreateQueue) markStarted() {
	q.activeDefaultResponse = true
}

// markFinished records that the active response ended and flushes any deferred
// request. Mirrors the Rust mark_finished.
func (q *responseCreateQueue) markFinished(
	ctx context.Context,
	writer Writer,
	eventsTx chan<- Event,
	reason string,
) error {
	q.activeDefaultResponse = false
	if !q.pendingCreate {
		return nil
	}
	q.pendingCreate = false
	return q.sendCreateNow(ctx, writer, eventsTx, reason)
}

// sendCreateNow issues a response.create, handling the active-response race by
// re-deferring rather than failing. Mirrors the Rust send_create_now. On any
// other send failure it emits an Error event and returns the error.
func (q *responseCreateQueue) sendCreateNow(
	ctx context.Context,
	writer Writer,
	eventsTx chan<- Event,
	reason string,
) error {
	if err := writer.SendResponseCreate(ctx); err != nil {
		message := err.Error()
		if strings.HasPrefix(message, activeResponseErrorPrefix) {
			// Raced an active response; defer and treat as success.
			q.activeDefaultResponse = true
			q.pendingCreate = true
			return nil
		}
		sendEvent(ctx, eventsTx, NewError(message))
		return fmt.Errorf("send %s response.create: %w", reason, err)
	}
	q.activeDefaultResponse = true
	return nil
}
