package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sqlrush/codexgo/internal/client"
)

// endpointSession drives request building, retries, auth, and telemetry for a
// single provider. It mirrors the Rust `EndpointSession`.
type endpointSession struct {
	transport        client.HTTPTransport
	provider         Provider
	auth             AuthProvider
	requestTelemetry client.RequestTelemetry
}

func newEndpointSession(transport client.HTTPTransport, provider Provider, auth AuthProvider) *endpointSession {
	return &endpointSession{transport: transport, provider: provider, auth: auth}
}

func (s *endpointSession) withRequestTelemetry(t client.RequestTelemetry) *endpointSession {
	s.requestTelemetry = t
	return s
}

// makeRequest builds a request from the provider template plus extra headers and
// an optional JSON body. The provider headers are copied, so callers never mutate
// shared state.
func (s *endpointSession) makeRequest(method, path string, extraHeaders http.Header, body json.RawMessage) client.Request {
	req := s.provider.BuildRequest(method, path)
	for name, values := range extraHeaders {
		for _, v := range values {
			req.Headers.Add(name, v)
		}
	}
	if body != nil {
		req.Body = &client.RequestBody{Kind: client.RequestBodyJSON, JSON: body}
	}
	return req
}

// configure mutates a request in place before it is sent (e.g. to set the Accept
// header or compression). It mirrors the Rust `configure` closure.
type configure func(*client.Request)

// executeWith runs a unary request under the retry policy. It mirrors the Rust
// `execute_with`.
func (s *endpointSession) executeWith(
	ctx context.Context,
	method, path string,
	extraHeaders http.Header,
	body json.RawMessage,
	cfg configure,
) (client.Response, *APIError) {
	makeReq := func() client.Request {
		req := s.makeRequest(method, path, extraHeaders, body)
		if cfg != nil {
			cfg(&req)
		}
		return req
	}
	resp, err := runWithRequestTelemetry(
		ctx,
		s.provider.Retry.ToPolicy(),
		s.requestTelemetry,
		makeReq,
		func(ctx context.Context, req client.Request) (client.Response, *client.TransportError) {
			authed, authErr := s.auth.ApplyAuth(ctx, req)
			if authErr != nil {
				return client.Response{}, authErr.ToTransportError()
			}
			r, terr := s.transport.Execute(ctx, authed)
			return r, asTransportError(terr)
		},
		func(r client.Response) int { return r.Status },
	)
	if err != nil {
		return client.Response{}, NewTransportError(err)
	}
	return resp, nil
}

// streamWith runs a streaming request under the retry policy. It mirrors the Rust
// `stream_with`.
func (s *endpointSession) streamWith(
	ctx context.Context,
	method, path string,
	extraHeaders http.Header,
	body json.RawMessage,
	cfg configure,
) (client.StreamResponse, *APIError) {
	makeReq := func() client.Request {
		req := s.makeRequest(method, path, extraHeaders, body)
		if cfg != nil {
			cfg(&req)
		}
		return req
	}
	resp, err := runWithRequestTelemetry(
		ctx,
		s.provider.Retry.ToPolicy(),
		s.requestTelemetry,
		makeReq,
		func(ctx context.Context, req client.Request) (client.StreamResponse, *client.TransportError) {
			authed, authErr := s.auth.ApplyAuth(ctx, req)
			if authErr != nil {
				return client.StreamResponse{}, authErr.ToTransportError()
			}
			r, terr := s.transport.Stream(ctx, authed)
			return r, asTransportError(terr)
		},
		func(r client.StreamResponse) int { return r.Status },
	)
	if err != nil {
		return client.StreamResponse{}, NewTransportError(err)
	}
	return resp, nil
}

// runWithRequestTelemetry wraps client.RunWithRetry to attach per-attempt request
// telemetry. It mirrors the Rust `run_with_request_telemetry`.
func runWithRequestTelemetry[T any](
	ctx context.Context,
	policy client.RetryPolicy,
	telemetry client.RequestTelemetry,
	makeReq client.MakeRequest,
	send func(context.Context, client.Request) (T, *client.TransportError),
	statusOf func(T) int,
) (T, *client.TransportError) {
	result, err := client.RunWithRetry(ctx, policy, makeReq,
		func(ctx context.Context, req client.Request, attempt uint64) (T, error) {
			start := time.Now()
			res, terr := send(ctx, req)
			if telemetry != nil {
				status := 0
				if terr == nil {
					status = statusOf(res)
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
// unknown errors as network errors.
func asTransportError(err error) *client.TransportError {
	if err == nil {
		return nil
	}
	if te, ok := err.(*client.TransportError); ok {
		return te
	}
	return client.NewNetworkError(err.Error())
}
