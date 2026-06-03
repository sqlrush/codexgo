package appserverclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// remoteMaxLineBytes bounds a single inbound JSON-RPC line on the remote
// transport, matching the server-side limit.
const remoteMaxLineBytes = 16 * 1024 * 1024

// RemoteAppServerClient talks to an out-of-process app-server over a
// newline-delimited JSON-RPC byte stream (stdio of a child process, a UDS
// connection, or any io.Reader/io.Writer pair). It issues requests, correlates
// responses by id, and surfaces server notifications on its event channel.
//
// It is the reduced Go analogue of the Rust RemoteAppServerClient.
type RemoteAppServerClient struct {
	w  *bufio.Writer
	wm sync.Mutex

	events chan ServerEvent

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan RequestResult
	closed  bool

	readDone chan struct{}
}

// StartRemote builds a remote client over the given reader/writer pair and
// starts the background read loop. closeOnShutdown, when non-nil, is invoked on
// [RemoteAppServerClient.Shutdown] to release the underlying transport (e.g.
// closing a socket or killing a child process).
func StartRemote(r io.Reader, w io.Writer) *RemoteAppServerClient {
	c := &RemoteAppServerClient{
		w:        bufio.NewWriter(w),
		events:   make(chan ServerEvent, defaultEventBuffer),
		pending:  make(map[int64]chan RequestResult),
		readDone: make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

// Request issues a typed client request and blocks until its response arrives or
// ctx is cancelled.
func (c *RemoteAppServerClient) Request(ctx context.Context, method string, params any) (RequestResult, error) {
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
	if err := c.send(appserverproto.NewRequestMessage(req)); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return RequestResult{}, err
	}

	select {
	case res := <-respCh:
		return res, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return RequestResult{}, ctx.Err()
	case <-c.readDone:
		return RequestResult{}, ErrClientClosed
	}
}

// RequestTyped issues a request and decodes a successful result into out.
func (c *RemoteAppServerClient) RequestTyped(ctx context.Context, method string, params any, out any) error {
	res, err := c.Request(ctx, method, params)
	if err != nil {
		return err
	}
	return res.Decode(out)
}

// NextEvent blocks until the next server event is available, honoring ctx.
func (c *RemoteAppServerClient) NextEvent(ctx context.Context) (ServerEvent, bool) {
	select {
	case ev, ok := <-c.events:
		return ev, ok
	case <-ctx.Done():
		return ServerEvent{}, false
	}
}

// Events returns the read-only event channel, closed when the read loop exits.
func (c *RemoteAppServerClient) Events() <-chan ServerEvent { return c.events }

// Shutdown marks the client closed and fails any pending requests. It does not
// close the underlying transport; the caller owns that lifecycle.
func (c *RemoteAppServerClient) Shutdown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	pending := c.pending
	c.pending = make(map[int64]chan RequestResult)
	c.mu.Unlock()

	for _, ch := range pending {
		select {
		case ch <- RequestResult{Error: &appserverproto.JSONRPCErrorBody{Code: -32603, Message: "client closed"}}:
		default:
		}
	}
}

// send writes one message as a newline-terminated JSON line.
func (c *RemoteAppServerClient) send(msg appserverproto.JSONRPCMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.wm.Lock()
	defer c.wm.Unlock()
	if _, err := c.w.Write(raw); err != nil {
		return err
	}
	if err := c.w.WriteByte('\n'); err != nil {
		return err
	}
	return c.w.Flush()
}

// readLoop reads inbound messages and routes responses/errors to pending
// requests and notifications to the event channel. It closes the event channel
// and the readDone signal when the reader ends.
func (c *RemoteAppServerClient) readLoop(r io.Reader) {
	defer close(c.readDone)
	defer close(c.events)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), remoteMaxLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		buf := make([]byte, len(line))
		copy(buf, line)

		var msg appserverproto.JSONRPCMessage
		if err := json.Unmarshal(buf, &msg); err != nil {
			continue
		}
		c.route(msg)
	}
	_ = scanner.Err()
}

// route delivers one inbound message to a pending request or the event channel.
func (c *RemoteAppServerClient) route(msg appserverproto.JSONRPCMessage) {
	switch msg.Kind {
	case appserverproto.MessageKindResponse:
		if id, ok := msg.Response.ID.Integer(); ok {
			c.deliver(id, RequestResult{Result: msg.Response.Result})
		}
	case appserverproto.MessageKindError:
		if id, ok := msg.Error.ID.Integer(); ok {
			body := msg.Error.Error
			c.deliver(id, RequestResult{Error: &body})
			return
		}
		c.pushEvent(ServerEvent{Error: msg.Error})
	case appserverproto.MessageKindNotification:
		c.pushEvent(ServerEvent{Notification: msg.Notification})
	default:
	}
}

// deliver routes a result to the matching pending request.
func (c *RemoteAppServerClient) deliver(id int64, res RequestResult) {
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

// pushEvent enqueues an event, dropping it when the channel is full.
func (c *RemoteAppServerClient) pushEvent(ev ServerEvent) {
	select {
	case c.events <- ev:
	default:
	}
}
