package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// MCP streamable HTTP transport constants. These mirror the header names and
// MIME types used by the reference rmcp StreamableHttpClientAdapter.
const (
	mimeEventStream  = "text/event-stream"
	mimeJSON         = "application/json"
	headerSessionID  = "Mcp-Session-Id"
	headerProtocol   = "MCP-Protocol-Version"
	maxNonJSONBody   = 8 * 1024
	sseDataFieldName = "data"
)

// ErrSessionExpired is returned when the server reports a 404 for a request that
// carried a session id, indicating the streamable HTTP session has expired. The
// connection manager treats this as a recoverable condition.
var ErrSessionExpired = errors.New("mcp: streamable HTTP session expired (404)")

// HTTPTransportConfig configures a streamable HTTP transport.
type HTTPTransportConfig struct {
	// URL is the MCP endpoint that accepts JSON-RPC POSTs.
	URL string
	// BearerToken, when non-empty, is sent as "Authorization: Bearer <token>"
	// on every request unless an Authorization header is already present in
	// DefaultHeaders.
	BearerToken string
	// DefaultHeaders are applied to every request. The caller builds these from
	// http_headers and env_http_headers (see [BuildHTTPHeaders]).
	DefaultHeaders http.Header
	// HTTPClient is the client used for requests. When nil, http.DefaultClient
	// is used.
	HTTPClient *http.Client
}

// httpTransport speaks the MCP streamable HTTP transport: each outbound message
// is POSTed to the endpoint, and responses (a single JSON object or an SSE
// stream of JSON-RPC messages) are queued for Receive. Server-initiated requests
// and notifications that arrive on SSE streams are surfaced through the same
// queue.
type httpTransport struct {
	url            string
	bearerToken    string
	defaultHeaders http.Header
	client         *http.Client

	mu        sync.Mutex
	sessionID string

	incoming chan []byte
	errCh    chan error

	closeOnce sync.Once
	closed    chan struct{}

	wg sync.WaitGroup
}

// NewHTTPTransport creates a streamable HTTP [Transport] for the configured
// endpoint. It performs no network I/O until the first Send.
func NewHTTPTransport(cfg HTTPTransportConfig) (Transport, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("mcp: http transport requires a url")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	headers := cfg.DefaultHeaders
	if headers == nil {
		headers = http.Header{}
	}
	return &httpTransport{
		url:            cfg.URL,
		bearerToken:    cfg.BearerToken,
		defaultHeaders: headers.Clone(),
		client:         client,
		incoming:       make(chan []byte, 16),
		errCh:          make(chan error, 1),
		closed:         make(chan struct{}),
	}, nil
}

// Send POSTs a single JSON-RPC message to the endpoint and dispatches the
// response body (JSON object or SSE stream) into the receive queue.
func (t *httpTransport) Send(ctx context.Context, message []byte) error {
	select {
	case <-t.closed:
		return ErrTransportClosed
	default:
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(message))
	if err != nil {
		return fmt.Errorf("mcp: build http request: %w", err)
	}
	t.applyHeaders(req)
	req.Header.Set("Accept", mimeEventStream+", "+mimeJSON)
	req.Header.Set("Content-Type", mimeJSON)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp: http request failed: %w", err)
	}

	if sid := resp.Header.Get(headerSessionID); sid != "" {
		t.setSessionID(sid)
	}

	if resp.StatusCode == http.StatusNotFound && t.hasSession() {
		resp.Body.Close()
		return ErrSessionExpired
	}

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		// Notification or empty acknowledgement: nothing to read back.
		resp.Body.Close()
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := readPreview(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("mcp: http status %d: %s", resp.StatusCode, preview)
	}

	contentType := resp.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, mimeEventStream):
		t.consumeSSE(resp.Body)
	case strings.HasPrefix(contentType, mimeJSON):
		t.consumeJSON(resp.Body)
	default:
		// Some servers reply 200 with an empty body for notifications.
		body := readPreview(resp.Body)
		resp.Body.Close()
		if strings.TrimSpace(body) == "" {
			return nil
		}
		return fmt.Errorf("mcp: unexpected content-type %q: %s", contentType, body)
	}
	return nil
}

// consumeJSON reads a single JSON object response body and enqueues it.
func (t *httpTransport) consumeJSON(body io.ReadCloser) {
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 64*1024*1024))
	if err != nil {
		t.reportError(fmt.Errorf("mcp: read http json body: %w", err))
		return
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return
	}
	t.enqueue(data)
}

// consumeSSE reads an event stream on a background goroutine, enqueuing each
// "data:" payload as a JSON-RPC message. The stream is read until it ends.
func (t *httpTransport) consumeSSE(body io.ReadCloser) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer body.Close()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		var data strings.Builder
		flush := func() {
			if data.Len() == 0 {
				return
			}
			payload := data.String()
			data.Reset()
			if strings.TrimSpace(payload) == "" {
				return
			}
			t.enqueue([]byte(payload))
		}
		for scanner.Scan() {
			select {
			case <-t.closed:
				return
			default:
			}
			line := scanner.Text()
			if line == "" {
				flush()
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue // comment / keep-alive
			}
			field, value, found := strings.Cut(line, ":")
			if !found {
				continue
			}
			value = strings.TrimPrefix(value, " ")
			if field == sseDataFieldName {
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(value)
			}
		}
		flush()
		if err := scanner.Err(); err != nil {
			t.reportError(fmt.Errorf("mcp: read sse stream: %w", err))
		}
	}()
}

// enqueue delivers a raw message to Receive, dropping it if the transport is
// closed.
func (t *httpTransport) enqueue(data []byte) {
	out := make([]byte, len(data))
	copy(out, data)
	select {
	case t.incoming <- out:
	case <-t.closed:
	}
}

// reportError records the first stream error so Receive can surface it.
func (t *httpTransport) reportError(err error) {
	select {
	case t.errCh <- err:
	default:
	}
}

// Receive blocks for the next queued JSON-RPC message.
func (t *httpTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-t.incoming:
		return msg, nil
	case err := <-t.errCh:
		return nil, err
	case <-t.closed:
		return nil, ErrTransportClosed
	}
}

// Close stops the transport, releasing any in-flight SSE readers.
func (t *httpTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closed)
	})
	t.wg.Wait()
	return nil
}

// applyHeaders sets the default headers, session id, and bearer token on a
// request. The bearer token is only added when no Authorization header is
// already present.
func (t *httpTransport) applyHeaders(req *http.Request) {
	for key, values := range t.defaultHeaders {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	if sid := t.getSessionID(); sid != "" {
		req.Header.Set(headerSessionID, sid)
	}
	if t.bearerToken != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+t.bearerToken)
	}
}

func (t *httpTransport) setSessionID(id string) {
	t.mu.Lock()
	t.sessionID = id
	t.mu.Unlock()
}

func (t *httpTransport) getSessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

func (t *httpTransport) hasSession() bool {
	return t.getSessionID() != ""
}

// readPreview reads up to maxNonJSONBody bytes from r for an error message.
func readPreview(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, maxNonJSONBody))
	return strings.TrimSpace(string(data))
}
