package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ServerRequestHandler reacts to a server-initiated JSON-RPC request (for
// example "elicitation/create"). It returns the result payload to send back to
// the server, or an error to convert into a JSON-RPC error response. The handler
// is invoked on the client's reader goroutine, so implementations must not block
// indefinitely; they should honor ctx.
type ServerRequestHandler func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)

// ServerNotificationHandler reacts to a server-initiated notification. Errors are
// logged by the caller; there is no response.
type ServerNotificationHandler func(method string, params json.RawMessage)

// pendingCall tracks an in-flight client request awaiting its response.
type pendingCall struct {
	ch chan Response
}

// Client is a JSON-RPC 2.0 MCP client bound to a single [Transport]. It owns the
// request/response correlation, dispatches server-initiated requests and
// notifications, and exposes typed MCP operations. A Client is safe for
// concurrent use by multiple goroutines.
type Client struct {
	transport Transport

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]pendingCall
	closed  bool
	initOK  bool

	onServerRequest      ServerRequestHandler
	onServerNotification ServerNotificationHandler

	readErrOnce sync.Once
	readErr     error
	done        chan struct{}

	closeOnce sync.Once
}

// ClientOption configures a [Client] at construction time.
type ClientOption func(*Client)

// WithServerRequestHandler installs the handler invoked for server-initiated
// requests such as elicitation/create.
func WithServerRequestHandler(handler ServerRequestHandler) ClientOption {
	return func(c *Client) { c.onServerRequest = handler }
}

// WithServerNotificationHandler installs the handler invoked for
// server-initiated notifications.
func WithServerNotificationHandler(handler ServerNotificationHandler) ClientOption {
	return func(c *Client) { c.onServerNotification = handler }
}

// NewClient wraps an established [Transport] in a JSON-RPC client and starts its
// reader loop. The caller retains ownership of the transport only insofar as
// [Client.Close] closes it. Apply options to register server-request and
// notification handlers.
func NewClient(transport Transport, opts ...ClientOption) *Client {
	c := &Client{
		transport: transport,
		pending:   make(map[int64]pendingCall),
		done:      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	go c.readLoop()
	return c
}

// readLoop consumes messages from the transport until it closes or errors,
// routing each message to a pending request, the server-request handler, or the
// notification handler.
func (c *Client) readLoop() {
	defer close(c.done)
	for {
		raw, err := c.transport.Receive(context.Background())
		if err != nil {
			c.fail(err)
			return
		}
		if len(raw) == 0 {
			continue
		}
		var msg Response
		if err := json.Unmarshal(raw, &msg); err != nil {
			// A malformed frame is non-fatal: skip it, matching tolerant
			// JSON-RPC stream readers.
			continue
		}
		c.dispatch(raw, msg)
	}
}

// dispatch routes a decoded message to the right consumer.
func (c *Client) dispatch(raw []byte, msg Response) {
	switch {
	case msg.IsServerRequest():
		c.handleServerRequest(msg)
	case msg.IsServerNotification():
		if c.onServerNotification != nil {
			c.onServerNotification(msg.Method, msg.Params)
		}
	default:
		c.completePending(msg)
	}
}

// completePending delivers a response to the goroutine awaiting the matching id.
func (c *Client) completePending(msg Response) {
	id, ok := decodeIntID(msg.ID)
	if !ok {
		return
	}
	c.mu.Lock()
	call, found := c.pending[id]
	if found {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if found {
		call.ch <- msg
	}
}

// handleServerRequest invokes the registered handler and writes the reply
// (result or error) back to the server.
func (c *Client) handleServerRequest(msg Response) {
	go func() {
		var (
			result json.RawMessage
			err    error
		)
		if c.onServerRequest != nil {
			result, err = c.onServerRequest(context.Background(), msg.Method, msg.Params)
		} else {
			err = fmt.Errorf("mcp: no handler for server request %q", msg.Method)
		}
		if writeErr := c.replyToServer(msg.ID, result, err); writeErr != nil {
			// The transport is gone; nothing more we can do.
			return
		}
	}()
}

// replyToServer sends a response envelope (result or error) for a
// server-initiated request id.
func (c *Client) replyToServer(id json.RawMessage, result json.RawMessage, opErr error) error {
	envelope := map[string]any{
		"jsonrpc": JSONRPCVersion,
		"id":      id,
	}
	if opErr != nil {
		envelope["error"] = map[string]any{
			"code":    -32603,
			"message": opErr.Error(),
		}
	} else if result != nil {
		envelope["result"] = result
	} else {
		envelope["result"] = json.RawMessage("{}")
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("mcp: encode server reply: %w", err)
	}
	return c.transport.Send(context.Background(), encoded)
}

// fail records the first terminal read error and wakes every waiter.
func (c *Client) fail(err error) {
	c.readErrOnce.Do(func() { c.readErr = err })
	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = make(map[int64]pendingCall)
	c.mu.Unlock()
	for _, call := range pending {
		call.ch <- Response{Error: &RPCError{Code: -32000, Message: err.Error()}}
	}
}

// call sends a request and blocks until the matching response arrives, ctx is
// cancelled, or the optional timeout elapses. A nil result is returned when the
// server replies with an empty result.
func (c *Client) call(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	id := c.nextID.Add(1)
	req, err := newRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: encode %s request: %w", method, err)
	}

	ch := make(chan Response, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientShutDown
	}
	c.pending[id] = pendingCall{ch: ch}
	c.mu.Unlock()

	if err := c.transport.Send(ctx, encoded); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp: send %s request: %w", method, err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp: %s: %w", method, ctx.Err())
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp: %s failed: %w", method, resp.Error)
		}
		return resp.Result, nil
	}
}

// notify sends a fire-and-forget notification.
func (c *Client) notify(ctx context.Context, method string, params any) error {
	note, err := newNotification(method, params)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(note)
	if err != nil {
		return fmt.Errorf("mcp: encode %s notification: %w", method, err)
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrClientShutDown
	}
	if err := c.transport.Send(ctx, encoded); err != nil {
		return fmt.Errorf("mcp: send %s notification: %w", method, err)
	}
	return nil
}

// Close shuts the client down, closing its transport and waking all waiters. It
// is idempotent and safe to call concurrently.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		err = c.transport.Close()
		<-c.done
	})
	return err
}

// decodeIntID extracts the integer id from a raw JSON id, reporting false when
// the id is absent, null, or not an integer.
func decodeIntID(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var id int64
	if err := json.Unmarshal(raw, &id); err != nil {
		return 0, false
	}
	return id, true
}

// errInitTwice guards Initialize against repeated calls.
var errInitTwice = ErrAlreadyInitialized

// errNotInit is returned by operations issued before Initialize.
var errNotInit = ErrNotInitialized

// requireInit reports an error when the session has not completed the handshake.
func (c *Client) requireInit() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClientShutDown
	}
	if !c.initOK {
		return errNotInit
	}
	return nil
}

// decodeResult unmarshals a response result into out, tolerating an empty result
// by leaving out at its zero value.
func decodeResult(result json.RawMessage, out any) error {
	if len(result) == 0 {
		return nil
	}
	if err := json.Unmarshal(result, out); err != nil {
		return fmt.Errorf("mcp: decode result: %w", err)
	}
	return nil
}

// assert that the named sentinel errors are wired up (keeps go vet/linters from
// flagging the indirection helpers as unused in builds that elide them).
var _ = errors.Is
var _ = errInitTwice
