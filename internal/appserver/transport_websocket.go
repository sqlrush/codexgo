package appserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// wsSink is an [OutgoingSink] that writes each JSON-RPC message as one WebSocket
// text frame. WebSocket frames are message-delimited, so no trailing newline is
// added. Concurrent writes are serialized with a mutex.
type wsSink struct {
	mu   sync.Mutex
	ctx  context.Context
	conn *websocket.Conn
}

// newWSSink wraps conn in a sink that uses ctx for write deadlines.
func newWSSink(ctx context.Context, conn *websocket.Conn) *wsSink {
	return &wsSink{ctx: ctx, conn: conn}
}

// Send marshals msg and writes it as a single text frame.
func (s *wsSink) Send(msg appserverproto.JSONRPCMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Write(s.ctx, websocket.MessageText, raw)
}

// WebSocketHandler returns an http.Handler that upgrades each request to a
// WebSocket connection and serves it with a processor built by factory. Each
// connection is single-client with its own [connectionState].
//
// It is the reduced port of the Rust websocket acceptor: each frame is one
// JSON-RPC message, requests are dispatched on their own goroutines, and
// outbound messages (responses, errors, codex/event notifications) are written
// back as text frames.
func WebSocketHandler(factory ProcessorFactory) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			// Accept already wrote an HTTP error response.
			return
		}
		ServeWebSocketConn(r.Context(), factory, conn)
	})
}

// ServeWebSocketConn serves a single already-accepted WebSocket connection. It
// reads JSON-RPC frames until the peer closes, ctx is cancelled, or a read error
// occurs, then shuts down the processor's forwarders and closes the connection.
func ServeWebSocketConn(ctx context.Context, factory ProcessorFactory, conn *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer conn.CloseNow()

	sink := newWSSink(ctx, conn)
	proc := factory(sink)
	connState := newConnectionState()
	defer proc.Shutdown(context.Background())

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText && typ != websocket.MessageBinary {
			continue
		}
		var msg appserverproto.JSONRPCMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Kind != appserverproto.MessageKindRequest {
			// Notifications/responses from the client are not handled in this subset.
			continue
		}
		req := *msg.Request
		wg.Add(1)
		go func() {
			defer wg.Done()
			proc.HandleRequest(ctx, connState, req)
		}()
	}
}
