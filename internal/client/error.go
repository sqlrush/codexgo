package client

import (
	"fmt"
	"net/http"
)

// TransportErrorKind discriminates the variants of TransportError, mirroring the
// Rust `TransportError` enum.
type TransportErrorKind int

const (
	// TransportErrorHTTP is a non-success HTTP status response.
	TransportErrorHTTP TransportErrorKind = iota
	// TransportErrorRetryLimit means the retry policy exhausted its attempts.
	TransportErrorRetryLimit
	// TransportErrorTimeout is a request timeout.
	TransportErrorTimeout
	// TransportErrorNetwork is a generic network/transport error.
	TransportErrorNetwork
	// TransportErrorBuild is a request-building error.
	TransportErrorBuild
)

// TransportError is the error type returned by the transport layer. It mirrors
// the Rust `TransportError` enum, preserving the HTTP status, URL, headers, and
// body for HTTP failures so callers (e.g. the API error bridge) can classify
// them precisely.
type TransportError struct {
	Kind TransportErrorKind

	// HTTP fields (valid when Kind == TransportErrorHTTP).
	Status  int
	URL     string
	Headers http.Header
	Body    string

	// Message carries the detail for Network and Build errors.
	Message string
}

// Error implements the error interface, matching the Rust Display formatting.
func (e *TransportError) Error() string {
	switch e.Kind {
	case TransportErrorHTTP:
		return fmt.Sprintf("http %d: %q", e.Status, e.Body)
	case TransportErrorRetryLimit:
		return "retry limit reached"
	case TransportErrorTimeout:
		return "timeout"
	case TransportErrorNetwork:
		return fmt.Sprintf("network error: %s", e.Message)
	case TransportErrorBuild:
		return fmt.Sprintf("request build error: %s", e.Message)
	default:
		return "transport error"
	}
}

// NewHTTPError builds a TransportError for a non-success HTTP response.
func NewHTTPError(status int, url string, headers http.Header, body string) *TransportError {
	return &TransportError{
		Kind:    TransportErrorHTTP,
		Status:  status,
		URL:     url,
		Headers: headers,
		Body:    body,
	}
}

// NewRetryLimitError builds a retry-limit TransportError.
func NewRetryLimitError() *TransportError {
	return &TransportError{Kind: TransportErrorRetryLimit}
}

// NewTimeoutError builds a timeout TransportError.
func NewTimeoutError() *TransportError {
	return &TransportError{Kind: TransportErrorTimeout}
}

// NewNetworkError builds a network TransportError with the given message.
func NewNetworkError(msg string) *TransportError {
	return &TransportError{Kind: TransportErrorNetwork, Message: msg}
}

// NewBuildError builds a request-build TransportError with the given message.
func NewBuildError(msg string) *TransportError {
	return &TransportError{Kind: TransportErrorBuild, Message: msg}
}

// StreamErrorKind discriminates the variants of StreamError.
type StreamErrorKind int

const (
	// StreamErrorStream is a generic streaming failure.
	StreamErrorStream StreamErrorKind = iota
	// StreamErrorTimeout is an idle timeout while streaming.
	StreamErrorTimeout
)

// StreamError mirrors the Rust `StreamError` enum used by the SSE helper.
type StreamError struct {
	Kind    StreamErrorKind
	Message string
}

// Error implements the error interface, matching the Rust Display formatting.
func (e *StreamError) Error() string {
	switch e.Kind {
	case StreamErrorStream:
		return fmt.Sprintf("stream failed: %s", e.Message)
	case StreamErrorTimeout:
		return "timeout"
	default:
		return "stream error"
	}
}

// NewStreamError builds a generic StreamError with the given message.
func NewStreamError(msg string) *StreamError {
	return &StreamError{Kind: StreamErrorStream, Message: msg}
}

// NewStreamTimeoutError builds an idle-timeout StreamError.
func NewStreamTimeoutError() *StreamError {
	return &StreamError{Kind: StreamErrorTimeout}
}
