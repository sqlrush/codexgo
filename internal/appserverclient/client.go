package appserverclient

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// ErrClientClosed is returned when an operation is attempted on a client whose
// connection has been shut down.
var ErrClientClosed = errors.New("appserverclient: client is closed")

// RequestResult is the outcome of a client request: either a successful result
// payload or a JSON-RPC error. Exactly one of Result / Error is set.
type RequestResult struct {
	// Result holds the raw response result on success.
	Result json.RawMessage
	// Error holds the JSON-RPC error body on failure.
	Error *appserverproto.JSONRPCErrorBody
}

// IsError reports whether the request failed.
func (r RequestResult) IsError() bool { return r.Error != nil }

// Decode unmarshals a successful result into out. It returns an error when the
// request failed or the payload cannot be decoded.
func (r RequestResult) Decode(out any) error {
	if r.Error != nil {
		return fmt.Errorf("appserverclient: request failed: code=%d message=%s", r.Error.Code, r.Error.Message)
	}
	if len(r.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.Result, out); err != nil {
		return fmt.Errorf("appserverclient: decode result: %w", err)
	}
	return nil
}

// ServerEvent is a server-to-client message surfaced to the caller: a
// notification (including the `codex/event` agent stream) or a standalone error.
// Exactly one of Notification / Error is set.
type ServerEvent struct {
	// Notification holds a server notification (method + params).
	Notification *appserverproto.JSONRPCNotification
	// Error holds a server-initiated error not tied to a pending request.
	Error *appserverproto.JSONRPCError
}

// IsCodexEvent reports whether this event is a forwarded core engine event
// (`codex/event`).
func (e ServerEvent) IsCodexEvent() bool {
	return e.Notification != nil && e.Notification.Method == "codex/event"
}

// Method returns the notification method, or "" for a non-notification event.
func (e ServerEvent) Method() string {
	if e.Notification == nil {
		return ""
	}
	return e.Notification.Method
}
