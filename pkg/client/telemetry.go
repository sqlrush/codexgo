package client

import "time"

// RequestTelemetry observes per-attempt request outcomes. It mirrors the Rust
// `RequestTelemetry` trait. status is the HTTP status when available (0 when
// unknown), and err is the transport error when the attempt failed.
type RequestTelemetry interface {
	OnRequest(attempt uint64, status int, err *TransportError, duration time.Duration)
}
