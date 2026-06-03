package execserver

import "context"

// NotificationSender delivers JSON-RPC notifications produced by the process
// backend (process/output, process/exited, process/closed) to a connected
// client.
//
// Rust: `RpcNotificationSender`. The Go port models only the notify path used by
// the in-process backend; transport wiring lives in out-of-scope modules. An
// implementation must be safe for concurrent use. params is one of the
// notification payload structs (for example [ExecOutputDeltaNotification]); the
// implementation is responsible for serializing it.
type NotificationSender interface {
	// Notify sends a notification with the given method and params payload.
	Notify(ctx context.Context, method string, params any) error
}

// discardNotificationSender drops every notification. It is the default sender
// when a backend is created without a transport, matching the Rust
// `LocalProcess::default` which spawns a task draining the outgoing channel.
type discardNotificationSender struct{}

// Notify implements NotificationSender by discarding the notification.
func (discardNotificationSender) Notify(context.Context, string, any) error { return nil }
