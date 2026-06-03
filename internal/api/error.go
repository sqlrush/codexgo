// Package api is a faithful Go port of the Rust `codex-api` crate. It implements
// the Codex Responses API on top of the HTTP transport in internal/client:
// request shaping (ResponsesApiRequest), the SSE response-event parser, rate
// limit / header parsing, the WebSocket transport, and the AuthProvider
// abstraction.
package api

import (
	"fmt"
	"time"

	"github.com/sqlrush/codexgo/internal/client"
)

// APIErrorKind discriminates the variants of APIError, mirroring the Rust
// `ApiError` enum.
type APIErrorKind int

const (
	// APIErrorTransport wraps a transport-level error.
	APIErrorTransport APIErrorKind = iota
	// APIErrorAPI is a structured API error with a status and message.
	APIErrorAPI
	// APIErrorStream is a streaming error.
	APIErrorStream
	// APIErrorContextWindowExceeded indicates the input exceeded the context window.
	APIErrorContextWindowExceeded
	// APIErrorQuotaExceeded indicates the account quota was exceeded.
	APIErrorQuotaExceeded
	// APIErrorUsageNotIncluded indicates usage is not included on the plan.
	APIErrorUsageNotIncluded
	// APIErrorRetryable is a retryable error with an optional server-suggested delay.
	APIErrorRetryable
	// APIErrorRateLimit is a rate-limit error.
	APIErrorRateLimit
	// APIErrorInvalidRequest indicates an invalid request.
	APIErrorInvalidRequest
	// APIErrorCyberPolicy indicates the request was flagged for cyber policy.
	APIErrorCyberPolicy
	// APIErrorServerOverloaded indicates the server is overloaded.
	APIErrorServerOverloaded
)

// APIError is the error type returned by the API layer. It mirrors the Rust
// `ApiError` enum.
type APIError struct {
	Kind APIErrorKind

	// Transport is set when Kind == APIErrorTransport.
	Transport *client.TransportError

	// Status is set for APIErrorAPI.
	Status int

	// Message carries the human-readable detail for several variants.
	Message string

	// Delay is the optional server-suggested retry delay for APIErrorRetryable.
	Delay *time.Duration
}

// Error implements the error interface, matching the Rust Display formatting.
func (e *APIError) Error() string {
	switch e.Kind {
	case APIErrorTransport:
		if e.Transport != nil {
			return e.Transport.Error()
		}
		return "transport error"
	case APIErrorAPI:
		return fmt.Sprintf("api error %d: %s", e.Status, e.Message)
	case APIErrorStream:
		return fmt.Sprintf("stream error: %s", e.Message)
	case APIErrorContextWindowExceeded:
		return "context window exceeded"
	case APIErrorQuotaExceeded:
		return "quota exceeded"
	case APIErrorUsageNotIncluded:
		return "usage not included"
	case APIErrorRetryable:
		return fmt.Sprintf("retryable error: %s", e.Message)
	case APIErrorRateLimit:
		return fmt.Sprintf("rate limit: %s", e.Message)
	case APIErrorInvalidRequest:
		return fmt.Sprintf("invalid request: %s", e.Message)
	case APIErrorCyberPolicy:
		return fmt.Sprintf("cyber policy: %s", e.Message)
	case APIErrorServerOverloaded:
		return "server overloaded"
	default:
		return "api error"
	}
}

// Unwrap exposes the wrapped transport error for errors.Is/As.
func (e *APIError) Unwrap() error {
	if e.Kind == APIErrorTransport && e.Transport != nil {
		return e.Transport
	}
	return nil
}

// NewTransportError wraps a client.TransportError as an APIError.
func NewTransportError(err *client.TransportError) *APIError {
	return &APIError{Kind: APIErrorTransport, Transport: err}
}

// NewStreamError builds an APIErrorStream with the given message.
func NewStreamError(msg string) *APIError {
	return &APIError{Kind: APIErrorStream, Message: msg}
}

// NewRetryableError builds an APIErrorRetryable with an optional delay.
func NewRetryableError(msg string, delay *time.Duration) *APIError {
	return &APIError{Kind: APIErrorRetryable, Message: msg, Delay: delay}
}
