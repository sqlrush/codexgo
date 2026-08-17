package responsesws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/client"
)

// WebSocket-related constants, mirroring the Rust responses_websocket module.
const (
	wsXCodexTurnStateHeader   = "X-Codex-Turn-State"
	wsXModelsEtagHeader       = "X-Models-Etag"
	wsXReasoningIncluded      = "X-api.Reasoning-Included"
	wsOpenAIModelHeader       = "OpenAI-Model"
	wsConnLimitReachedCode    = "websocket_connection_limit_reached"
	wsConnLimitReachedMessage = "Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue."
)

// ResponsesWebsocketClose captures a close frame from a probe. It mirrors the
// Rust `ResponsesWebsocketClose`.
type ResponsesWebsocketClose struct {
	Code   string
	Reason string
}

// ResponsesWebsocketClient connects to the Responses WebSocket endpoint for one
// provider. It mirrors the Rust `ResponsesWebsocketClient`.
type ResponsesWebsocketClient struct {
	provider api.Provider
	auth     api.AuthProvider
	// httpClient is used for the WebSocket handshake. When nil the default
	// client is used.
	httpClient *http.Client
}

// NewResponsesWebsocketClient builds a WebSocket client for a provider and auth
// source. httpClient may be nil to use the default.
func NewResponsesWebsocketClient(provider api.Provider, auth api.AuthProvider, httpClient *http.Client) *ResponsesWebsocketClient {
	return &ResponsesWebsocketClient{provider: provider, auth: auth, httpClient: httpClient}
}

// ResponsesWebsocketConnection is an open WebSocket connection that can stream
// one response at a time. It mirrors the Rust `ResponsesWebsocketConnection`.
type ResponsesWebsocketConnection struct {
	mu                      sync.Mutex
	conn                    *websocket.Conn
	closed                  bool
	idleTimeout             time.Duration
	serverReasoningIncluded bool
	modelsEtag              string
	serverModel             string
	telemetry               api.WebsocketTelemetry
}

// Connect opens a WebSocket connection. It mirrors the Rust `connect`. The
// returned connection is ready to stream a request.
func (c *ResponsesWebsocketClient) Connect(
	ctx context.Context,
	extraHeaders, defaultHeaders http.Header,
	setTurnState func(string),
	telemetry api.WebsocketTelemetry,
) (*ResponsesWebsocketConnection, *api.APIError) {
	wsURL := c.provider.WebsocketURLForPath(api.ResponsesPath)

	headers := mergeRequestHeaders(c.provider.Headers, extraHeaders, defaultHeaders)
	c.auth.AddAuthHeaders(headers)

	conn, resp, dialErr := dialWebsocket(ctx, wsURL, headers, c.httpClient)
	if dialErr != nil {
		return nil, mapWSDialError(dialErr, wsURL, resp)
	}

	reasoningIncluded := resp.Header.Get(wsXReasoningIncluded) != ""
	modelsEtag := resp.Header.Get(wsXModelsEtagHeader)
	serverModel := resp.Header.Get(wsOpenAIModelHeader)
	if setTurnState != nil {
		if v := resp.Header.Get(wsXCodexTurnStateHeader); v != "" {
			setTurnState(v)
		}
	}

	return &ResponsesWebsocketConnection{
		conn:                    conn,
		idleTimeout:             c.provider.StreamIdleTimeout,
		serverReasoningIncluded: reasoningIncluded,
		modelsEtag:              modelsEtag,
		serverModel:             serverModel,
		telemetry:               telemetry,
	}, nil
}

// dialWebsocket performs the WebSocket handshake with permessage-deflate
// negotiated, mirroring the Rust deflate config.
func dialWebsocket(ctx context.Context, url string, headers http.Header, httpClient *http.Client) (*websocket.Conn, *http.Response, error) {
	opts := &websocket.DialOptions{
		HTTPHeader:      headers,
		CompressionMode: websocket.CompressionContextTakeover,
	}
	if httpClient != nil {
		opts.HTTPClient = httpClient
	}
	return websocket.Dial(ctx, url, opts)
}

// IsClosed reports whether the connection has been closed.
func (c *ResponsesWebsocketConnection) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Close closes the WebSocket connection with a normal-closure status.
func (c *ResponsesWebsocketConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn == nil {
		return nil
	}
	c.closed = true
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

// SendResponseProcessed sends a "response.processed" frame on a reused
// connection. It mirrors the Rust `send_response_processed`.
func (c *ResponsesWebsocketConnection) SendResponseProcessed(ctx context.Context, responseID string) *api.APIError {
	request := api.ResponsesWsRequest{
		Kind:      api.ResponsesWsRequestProcessed,
		Processed: &api.ResponseProcessedWsRequest{ResponseID: responseID},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return api.NewStreamError(fmt.Sprintf("failed to encode websocket request: %v", err))
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn == nil {
		return api.NewStreamError("websocket connection is closed")
	}
	return sendWebsocketRequest(ctx, c.conn, body, c.idleTimeout, c.telemetry, true)
}

// StreamRequest sends a request frame and returns a api.ResponseStream of the
// resulting events. It mirrors the Rust `stream_request`. The connection is held
// exclusively for the lifetime of the returned stream.
func (c *ResponsesWebsocketConnection) StreamRequest(ctx context.Context, request api.ResponsesWsRequest, connectionReused bool) (api.ResponseStream, *api.APIError) {
	body, err := json.Marshal(request)
	if err != nil {
		return api.ResponseStream{}, api.NewStreamError(fmt.Sprintf("failed to encode websocket request: %v", err))
	}

	out := make(chan api.ResponseResult, 1600)
	go func() {
		defer close(out)
		send := func(res api.ResponseResult) bool {
			select {
			case <-ctx.Done():
				return false
			case out <- res:
				return true
			}
		}

		if c.serverModel != "" {
			if !send(api.ResponseResult{Event: &api.ResponseEvent{Kind: api.ResponseEventServerModel, Model: c.serverModel}}) {
				return
			}
		}
		if c.modelsEtag != "" {
			if !send(api.ResponseResult{Event: &api.ResponseEvent{Kind: api.ResponseEventModelsEtag, ModelsEtag: c.modelsEtag}}) {
				return
			}
		}
		if c.serverReasoningIncluded {
			if !send(api.ResponseResult{Event: &api.ResponseEvent{Kind: api.ResponseEventServerReasoningIncluded, ReasoningIncluded: true}}) {
				return
			}
		}

		c.mu.Lock()
		if c.closed || c.conn == nil {
			c.mu.Unlock()
			send(api.ResponseResult{Err: api.NewStreamError("websocket connection is closed")})
			return
		}
		runErr := runWebsocketResponseStream(ctx, c.conn, send, body, c.idleTimeout, c.telemetry, connectionReused)
		if runErr != nil {
			// A terminal error tears down the connection immediately.
			c.closed = true
			conn := c.conn
			c.conn = nil
			c.mu.Unlock()
			if conn != nil {
				_ = conn.Close(websocket.StatusInternalError, "stream error")
			}
			send(api.ResponseResult{Err: runErr})
			return
		}
		c.mu.Unlock()
	}()

	return api.ResponseStream{Events: out, UpstreamRequestID: nil}, nil
}

// runWebsocketResponseStream sends the request and processes incoming text
// frames until completion. It mirrors the Rust `run_websocket_response_stream`.
func runWebsocketResponseStream(
	ctx context.Context,
	conn *websocket.Conn,
	send func(api.ResponseResult) bool,
	body json.RawMessage,
	idleTimeout time.Duration,
	telemetry api.WebsocketTelemetry,
	connectionReused bool,
) *api.APIError {
	var lastServerModel string
	if err := sendWebsocketRequest(ctx, conn, body, idleTimeout, telemetry, connectionReused); err != nil {
		return err
	}

	for {
		pollStart := time.Now()
		text, readErr := readWebsocketText(ctx, conn, idleTimeout)
		if telemetry != nil {
			telemetry.OnWSEvent(readErr, time.Since(pollStart))
		}
		if readErr != nil {
			return readErr
		}

		if wrapped := parseWrappedWebsocketErrorEvent(text); wrapped != nil {
			if mapped := mapWrappedWebsocketErrorEvent(wrapped, text); mapped != nil {
				return mapped
			}
		}

		var event api.ResponsesStreamEvent
		if json.Unmarshal([]byte(text), &event) != nil {
			continue
		}

		modelVerifications := event.ModelVerifications()
		if event.EventKind() == "codex.rate_limits" {
			if snapshot := api.ParseRateLimitEvent(text); snapshot != nil {
				send(api.ResponseResult{Event: &api.ResponseEvent{Kind: api.ResponseEventRateLimits, RateLimits: snapshot}})
			}
			continue
		}
		if model := event.ResponseModel(); model != "" && model != lastServerModel {
			send(api.ResponseResult{Event: &api.ResponseEvent{Kind: api.ResponseEventServerModel, Model: model}})
			lastServerModel = model
		}
		if len(modelVerifications) > 0 {
			if !send(api.ResponseResult{Event: &api.ResponseEvent{Kind: api.ResponseEventModelVerifications, Verifications: modelVerifications}}) {
				return api.NewStreamError("response event consumer dropped")
			}
		}

		ev, apiErr := api.ProcessResponsesEvent(event)
		switch {
		case apiErr != nil:
			return apiErr
		case ev != nil:
			isCompleted := ev.Kind == api.ResponseEventCompleted
			send(api.ResponseResult{Event: ev})
			if isCompleted {
				return nil
			}
		}
	}
}

// readWebsocketText reads the next text frame subject to the idle timeout. Binary
// and close frames map to terminal stream errors, mirroring the Rust handling.
func readWebsocketText(ctx context.Context, conn *websocket.Conn, idleTimeout time.Duration) (string, *api.APIError) {
	readCtx := ctx
	var cancel context.CancelFunc
	if idleTimeout > 0 {
		readCtx, cancel = context.WithTimeout(ctx, idleTimeout)
		defer cancel()
	}
	msgType, data, err := conn.Read(readCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return "", api.NewStreamError("idle timeout waiting for websocket")
		}
		var closeErr websocket.CloseError
		if errors.As(err, &closeErr) {
			return "", api.NewStreamError("websocket closed by server before response.completed")
		}
		return "", api.NewStreamError(err.Error())
	}
	switch msgType {
	case websocket.MessageText:
		return string(data), nil
	case websocket.MessageBinary:
		return "", api.NewStreamError("unexpected binary websocket event")
	default:
		return "", api.NewStreamError("unexpected websocket event")
	}
}

// sendWebsocketRequest writes a request frame subject to the idle timeout. It
// mirrors the Rust `send_websocket_request`.
func sendWebsocketRequest(
	ctx context.Context,
	conn *websocket.Conn,
	body json.RawMessage,
	idleTimeout time.Duration,
	telemetry api.WebsocketTelemetry,
	connectionReused bool,
) *api.APIError {
	writeCtx := ctx
	var cancel context.CancelFunc
	if idleTimeout > 0 {
		writeCtx, cancel = context.WithTimeout(ctx, idleTimeout)
		defer cancel()
	}
	start := time.Now()
	err := conn.Write(writeCtx, websocket.MessageText, body)
	var apiErr *api.APIError
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			apiErr = api.NewStreamError("idle timeout sending websocket request")
		} else {
			apiErr = api.NewStreamError(fmt.Sprintf("failed to send websocket request: %v", err))
		}
	}
	if telemetry != nil {
		telemetry.OnWSRequest(time.Since(start), apiErr, connectionReused)
	}
	return apiErr
}

// wrappedWebsocketErrorEvent mirrors the Rust deserialization struct for wrapped
// error events.
type wrappedWebsocketErrorEvent struct {
	Kind    string                     `json:"type"`
	Status  *int                       `json:"status"`
	Status2 *int                       `json:"status_code"`
	Error   *wrappedWebsocketError     `json:"error"`
	Headers map[string]json.RawMessage `json:"headers"`
}

type wrappedWebsocketError struct {
	Code    *string `json:"code"`
	Message *string `json:"message"`
}

func parseWrappedWebsocketErrorEvent(payload string) *wrappedWebsocketErrorEvent {
	var event wrappedWebsocketErrorEvent
	if json.Unmarshal([]byte(payload), &event) != nil {
		return nil
	}
	if event.Kind != "error" {
		return nil
	}
	return &event
}

// mapWrappedWebsocketErrorEvent maps a wrapped error event to an api.APIError. It
// mirrors the Rust `map_wrapped_websocket_error_event`.
func mapWrappedWebsocketErrorEvent(event *wrappedWebsocketErrorEvent, originalPayload string) *api.APIError {
	if event.Error != nil && event.Error.Code != nil && *event.Error.Code == wsConnLimitReachedCode {
		msg := wsConnLimitReachedMessage
		if event.Error.Message != nil {
			msg = *event.Error.Message
		}
		return api.NewRetryableError(msg, nil)
	}

	status := event.Status
	if status == nil {
		status = event.Status2
	}
	if status == nil {
		return nil
	}
	if *status >= 200 && *status <= 299 {
		return nil
	}
	headers := jsonHeadersToHTTPHeaders(event.Headers)
	return api.NewTransportError(client.NewHTTPError(*status, "", headers, originalPayload))
}

func jsonHeadersToHTTPHeaders(headers map[string]json.RawMessage) http.Header {
	if headers == nil {
		return nil
	}
	mapped := http.Header{}
	for name, raw := range headers {
		value, ok := jsonHeaderValue(raw)
		if !ok {
			continue
		}
		mapped.Set(name, value)
	}
	return mapped
}

func jsonHeaderValue(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return strconv.FormatFloat(f, 'g', -1, 64), true
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return strconv.FormatBool(b), true
	}
	return "", false
}

// mergeRequestHeaders merges provider, extra, and default headers. Extra headers
// override provider headers; default headers fill only absent keys. It mirrors
// the Rust `merge_request_headers`.
func mergeRequestHeaders(providerHeaders, extraHeaders, defaultHeaders http.Header) http.Header {
	headers := providerHeaders.Clone()
	if headers == nil {
		headers = http.Header{}
	}
	for name, values := range extraHeaders {
		headers.Del(name)
		for _, v := range values {
			headers.Add(name, v)
		}
	}
	for name, values := range defaultHeaders {
		if len(headers.Values(name)) == 0 {
			for _, v := range values {
				headers.Add(name, v)
			}
		}
	}
	return headers
}

// mapWSDialError maps a WebSocket handshake error (and its HTTP response, when
// present) to an api.APIError. It mirrors the Rust `map_ws_error`.
func mapWSDialError(err error, url string, resp *http.Response) *api.APIError {
	if resp != nil && (resp.StatusCode < 200 || resp.StatusCode > 299) {
		return api.NewTransportError(client.NewHTTPError(resp.StatusCode, url, resp.Header.Clone(), ""))
	}
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		return api.NewStreamError("websocket closed")
	}
	return api.NewTransportError(client.NewNetworkError(err.Error()))
}
