package api

import (
	"time"

	"github.com/sqlrush/codexgo/internal/client"
)

// RequestTelemetry is re-exported from the client package for convenience, so API
// callers can configure per-attempt request telemetry without importing the
// client package directly. It mirrors the Rust re-export of `RequestTelemetry`.
type RequestTelemetry = client.RequestTelemetry

// SSETelemetry observes SSE poll outcomes. It mirrors the Rust `SseTelemetry`
// trait, adapted to Go: hadEvent reports whether the poll produced an event, err
// carries any stream error, and duration is the poll latency.
type SSETelemetry interface {
	OnSSEPoll(hadEvent bool, err *client.StreamError, duration time.Duration)
}

// WebsocketTelemetry observes WebSocket request and event outcomes. It mirrors
// the Rust `WebsocketTelemetry` trait.
type WebsocketTelemetry interface {
	OnWSRequest(duration time.Duration, err *APIError, connectionReused bool)
	OnWSEvent(err *APIError, duration time.Duration)
}
