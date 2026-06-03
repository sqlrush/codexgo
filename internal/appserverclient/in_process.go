package appserverclient

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// defaultEventBuffer bounds the in-process event channel. When full, server
// notifications are dropped (with a recorded count) rather than blocking the
// engine, mirroring the Rust in-process client's saturation handling.
const defaultEventBuffer = 1024

// InProcessAppServerClient drives an [appserver.Processor] in the same process,
// without any serialization round-trip. It implements the request/response and
// event-stream surface shared with the remote client.
//
// It is the Go analogue of the Rust InProcessAppServerClient: requests are
// dispatched directly to the processor (each on its own goroutine so a turn does
// not block subsequent requests), responses are correlated by JSON-RPC id, and
// server notifications are delivered through the event channel.
type InProcessAppServerClient struct {
	proc *appserver.Processor
	conn *routingSink
	ctx  context.Context

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan RequestResult
	closed  bool
}

// InProcessStartArgs bundles the inputs needed to start an in-process client.
type InProcessStartArgs struct {
	// Assembly is the constructed engine; required.
	Assembly *appserver.Assembly
	// Defaults are the per-session default settings; required.
	Defaults appserver.Defaults
	// EventBuffer bounds the event channel; defaults to defaultEventBuffer.
	EventBuffer int
}

// StartInProcess constructs an in-process client over the supplied engine. The
// returned client is ready for requests and event consumption. The provided ctx
// scopes the lifetime of every spawned thread's event forwarders.
func StartInProcess(ctx context.Context, args InProcessStartArgs) *InProcessAppServerClient {
	buf := args.EventBuffer
	if buf <= 0 {
		buf = defaultEventBuffer
	}
	c := &InProcessAppServerClient{
		ctx:     ctx,
		pending: make(map[int64]chan RequestResult),
	}
	c.conn = newRoutingSink(c, buf)
	c.proc = appserver.NewProcessor(appserver.ProcessorConfig{
		Assembly: args.Assembly,
		Defaults: args.Defaults,
		Sink:     c.conn,
	})
	return c
}

// Request issues a typed client request and blocks until its response arrives or
// ctx is cancelled. The method is resolved from the registry; the params are
// marshaled into the request envelope. A deferred-response method (e.g.
// turn/interrupt) blocks until the engine answers.
func (c *InProcessAppServerClient) Request(ctx context.Context, method string, params any) (RequestResult, error) {
	id := c.nextID.Add(1)
	raw, err := marshalParams(params)
	if err != nil {
		return RequestResult{}, err
	}

	respCh := make(chan RequestResult, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return RequestResult{}, ErrClientClosed
	}
	c.pending[id] = respCh
	c.mu.Unlock()

	req := appserverproto.JSONRPCRequest{
		ID:     appserverproto.NewIntegerRequestId(id),
		Method: method,
		Params: raw,
	}

	// Dispatch on its own goroutine so the client can keep draining events while
	// a turn-driving request is in flight.
	go c.proc.HandleRequest(c.ctx, c.conn.connState, req)

	select {
	case res := <-respCh:
		return res, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return RequestResult{}, ctx.Err()
	}
}

// RequestTyped issues a request and decodes a successful result into out. It
// returns an error when the request fails or the result cannot be decoded.
func (c *InProcessAppServerClient) RequestTyped(ctx context.Context, method string, params any, out any) error {
	res, err := c.Request(ctx, method, params)
	if err != nil {
		return err
	}
	return res.Decode(out)
}

// NextEvent blocks until the next server event (notification or error) is
// available, honoring ctx. It returns false when the event channel is closed.
func (c *InProcessAppServerClient) NextEvent(ctx context.Context) (ServerEvent, bool) {
	select {
	case ev, ok := <-c.conn.events:
		return ev, ok
	case <-ctx.Done():
		return ServerEvent{}, false
	}
}

// Events returns the read-only event channel for callers that prefer to range
// over it. The channel is closed when the client is shut down.
func (c *InProcessAppServerClient) Events() <-chan ServerEvent { return c.conn.events }

// Shutdown stops the engine's thread forwarders and closes the event channel.
// Pending requests are failed with [ErrClientClosed].
func (c *InProcessAppServerClient) Shutdown(ctx context.Context) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	pending := c.pending
	c.pending = make(map[int64]chan RequestResult)
	c.mu.Unlock()

	c.proc.Shutdown(ctx)

	for _, ch := range pending {
		select {
		case ch <- RequestResult{Error: &appserverproto.JSONRPCErrorBody{Code: -32603, Message: "client closed"}}:
		default:
		}
	}
	c.conn.close()
}

// deliverResponse routes a response/error to the matching pending request.
func (c *InProcessAppServerClient) deliverResponse(id int64, res RequestResult) {
	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if ok {
		ch <- res
	}
}

// marshalParams marshals request params, treating a nil params as absent.
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}
