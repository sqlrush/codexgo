package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sqlrush/codexgo/internal/client"
)

// ResponsesOptions configures a single Responses stream request. It mirrors the
// Rust `ResponsesOptions`.
type ResponsesOptions struct {
	SessionID     *string
	ThreadID      *string
	SessionSource *SessionSource
	ExtraHeaders  http.Header
	Compression   Compression
	// SetTurnState, if non-nil, is invoked with the x-codex-turn-state header
	// value when the server provides one.
	SetTurnState func(string)
}

// ResponsesClient streams Responses API requests for one provider. It mirrors the
// Rust `ResponsesClient`.
type ResponsesClient struct {
	session      *endpointSession
	sseTelemetry SSETelemetry
}

// NewResponsesClient builds a ResponsesClient for a transport, provider, and auth
// source.
func NewResponsesClient(transport client.HTTPTransport, provider Provider, auth AuthProvider) *ResponsesClient {
	return &ResponsesClient{session: newEndpointSession(transport, provider, auth)}
}

// WithTelemetry attaches request and SSE telemetry. It mirrors the Rust
// `with_telemetry`. It returns the same client for chaining.
func (c *ResponsesClient) WithTelemetry(request client.RequestTelemetry, sse SSETelemetry) *ResponsesClient {
	c.session = c.session.withRequestTelemetry(request)
	c.sseTelemetry = sse
	return c
}

const responsesPath = "responses"

// StreamRequest streams a typed ResponsesApiRequest. It mirrors the Rust
// `stream_request`: it serializes the body, optionally attaches item ids for
// Azure store requests, builds the session headers, and streams.
func (c *ResponsesClient) StreamRequest(ctx context.Context, request ResponsesApiRequest, options ResponsesOptions) (ResponseStream, *APIError) {
	body, err := json.Marshal(request)
	if err != nil {
		return ResponseStream{}, NewStreamError(fmt.Sprintf("failed to encode responses request: %v", err))
	}
	if request.Store && c.session.provider.IsAzureResponsesEndpoint() {
		patched, perr := attachItemIDs(body, request.Input)
		if perr == nil {
			body = patched
		}
	}

	headers := cloneHeader(options.ExtraHeaders)
	if options.ThreadID != nil {
		insertHeader(headers, "x-client-request-id", *options.ThreadID)
	}
	for name, values := range BuildSessionHeaders(options.SessionID, options.ThreadID) {
		for _, v := range values {
			headers.Add(name, v)
		}
	}
	if subagent, ok := subagentHeader(options.SessionSource); ok {
		insertHeader(headers, "x-openai-subagent", subagent)
	}

	return c.stream(ctx, body, headers, options.Compression, options.SetTurnState)
}

// stream streams a raw JSON body. It mirrors the Rust `stream`.
func (c *ResponsesClient) stream(
	ctx context.Context,
	body json.RawMessage,
	extraHeaders http.Header,
	compression Compression,
	setTurnState func(string),
) (ResponseStream, *APIError) {
	requestCompression := client.CompressionNone
	if compression == CompressionZstd {
		requestCompression = client.CompressionZstd
	}

	streamResponse, err := c.session.streamWith(
		ctx,
		http.MethodPost,
		responsesPath,
		extraHeaders,
		body,
		func(req *client.Request) {
			req.Headers.Set("Accept", "text/event-stream")
			req.Compression = requestCompression
		},
	)
	if err != nil {
		return ResponseStream{}, err
	}

	return SpawnResponseStream(
		ctx,
		streamResponse,
		c.session.provider.StreamIdleTimeout,
		c.sseTelemetry,
		setTurnState,
	), nil
}
