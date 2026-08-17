package remotecompact

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/client"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// compactEndpointPath is the relative path of the v1 compaction endpoint. It
// mirrors the Rust `CompactClient::path`.
const compactEndpointPath = "responses/compact"

// CompactRequestTimeoutIdleMultiplier scales a provider's stream idle timeout
// into the full-response timeout for a v1 compaction call. `/responses/compact`
// is unary, so the timeout must cover the entire response rather than a single
// idle gap. It mirrors the Rust `COMPACT_REQUEST_TIMEOUT_IDLE_MULTIPLIER`.
const CompactRequestTimeoutIdleMultiplier = 4

// CompactClient performs unary v1 compaction requests against the
// "/responses/compact" endpoint for a single provider. It mirrors the Rust
// `codex_api::CompactClient`.
//
// It is constructed from the public api/client building blocks (a Provider, an
// AuthProvider, and an HTTPTransport) so it never depends on the unexported
// endpoint session machinery in internal/api.
type CompactClient struct {
	transport client.HTTPTransport
	provider  api.Provider
	auth      api.AuthProvider
	telemetry client.RequestTelemetry
}

// NewCompactClient builds a CompactClient for the given transport, provider, and
// auth source. It mirrors the Rust `CompactClient::new`.
func NewCompactClient(transport client.HTTPTransport, provider api.Provider, auth api.AuthProvider) *CompactClient {
	return &CompactClient{transport: transport, provider: provider, auth: auth}
}

// WithTelemetry attaches per-attempt request telemetry and returns the same
// client for chaining. It mirrors the Rust `CompactClient::with_telemetry`.
func (c *CompactClient) WithTelemetry(telemetry client.RequestTelemetry) *CompactClient {
	c.telemetry = telemetry
	return c
}

// CompactInput encodes a CompactionInput and posts it to the compaction
// endpoint, returning the compacted transcript. It mirrors the Rust
// `CompactClient::compact_input`.
func (c *CompactClient) CompactInput(ctx context.Context, input CompactionInput, extraHeaders http.Header, requestTimeout time.Duration) ([]protocol.ResponseItem, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to encode compaction input: %w", err)
	}
	return c.Compact(ctx, body, extraHeaders, requestTimeout)
}

// Compact posts a raw JSON body to the compaction endpoint and parses the
// `output` array of the response into a list of ResponseItems. It mirrors the
// Rust `CompactClient::compact`.
//
// The request is run under the provider's retry policy. The full-response
// timeout is applied per attempt via the request's Timeout field.
func (c *CompactClient) Compact(ctx context.Context, body json.RawMessage, extraHeaders http.Header, requestTimeout time.Duration) ([]protocol.ResponseItem, error) {
	resp, apiErr := c.execute(ctx, body, extraHeaders, requestTimeout)
	if apiErr != nil {
		return nil, apiErr
	}

	var parsed compactHistoryResponse
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return nil, api.NewStreamError(err.Error())
	}
	return parsed.Output, nil
}

// execute runs a single unary POST under the provider retry policy, applying
// auth per attempt. It is the remotecompact analogue of the api package's
// unexported endpointSession.executeWith, reusing only the public surface.
func (c *CompactClient) execute(ctx context.Context, body json.RawMessage, extraHeaders http.Header, requestTimeout time.Duration) (client.Response, *api.APIError) {
	makeReq := func() client.Request {
		req := c.provider.BuildRequest(http.MethodPost, compactEndpointPath)
		for name, values := range extraHeaders {
			for _, v := range values {
				req.Headers.Add(name, v)
			}
		}
		req.Body = &client.RequestBody{Kind: client.RequestBodyJSON, JSON: body}
		if requestTimeout > 0 {
			req.Timeout = requestTimeout
		}
		return req
	}

	resp, terr := runWithRequestTelemetry(
		ctx,
		c.provider.Retry.ToPolicy(),
		c.telemetry,
		makeReq,
		func(ctx context.Context, req client.Request) (client.Response, *client.TransportError) {
			authed, authErr := c.auth.ApplyAuth(ctx, req)
			if authErr != nil {
				return client.Response{}, authErr.ToTransportError()
			}
			r, execErr := c.transport.Execute(ctx, authed)
			return r, asTransportError(execErr)
		},
	)
	if terr != nil {
		return client.Response{}, api.NewTransportError(terr)
	}
	return resp, nil
}

// compactHistoryResponse is the deserialized body of a v1 compaction response.
// It mirrors the Rust private `CompactHistoryResponse`.
type compactHistoryResponse struct {
	Output []protocol.ResponseItem `json:"output"`
}

// runWithRequestTelemetry wraps client.RunWithRetry to attach per-attempt
// request telemetry, mirroring the api package's runWithRequestTelemetry helper
// (re-implemented here against the public client API).
func runWithRequestTelemetry(
	ctx context.Context,
	policy client.RetryPolicy,
	telemetry client.RequestTelemetry,
	makeReq client.MakeRequest,
	send func(context.Context, client.Request) (client.Response, *client.TransportError),
) (client.Response, *client.TransportError) {
	result, err := client.RunWithRetry(ctx, policy, makeReq,
		func(ctx context.Context, req client.Request, attempt uint64) (client.Response, error) {
			start := time.Now()
			res, terr := send(ctx, req)
			if telemetry != nil {
				status := 0
				if terr == nil {
					status = res.Status
				} else if terr.Kind == client.TransportErrorHTTP {
					status = terr.Status
				}
				telemetry.OnRequest(attempt, status, terr, time.Since(start))
			}
			if terr != nil {
				return res, terr
			}
			return res, nil
		},
	)
	if err != nil {
		return result, asTransportError(err)
	}
	return result, nil
}

// asTransportError coerces an error into a *client.TransportError, wrapping
// unknown errors as network errors. It mirrors the api package helper.
func asTransportError(err error) *client.TransportError {
	if err == nil {
		return nil
	}
	if te, ok := err.(*client.TransportError); ok {
		return te
	}
	return client.NewNetworkError(err.Error())
}
