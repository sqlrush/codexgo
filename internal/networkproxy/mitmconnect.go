package networkproxy

// This file bridges the hijacked CONNECT socket into the MITM data path. It is
// the Go analogue of mitm.rs::mitm_tunnel: after the client's CONNECT is
// answered with 200, the upgraded stream is wrapped in a TLS acceptor that
// presents a per-host leaf certificate, and inner HTTP requests are served by
// the policy-enforcing mitmHandler (mitmdata.go).
//
// Where the Rust uses rama's HttpServer::auto over an Upgraded stream, we use a
// single-connection net/http server (http.Server.Serve over a one-shot
// listener) so the standard library parses HTTP/1.1 and HTTP/2 inner requests.

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"
)

// mitmConnect terminates a CONNECT tunnel with a minted leaf certificate and
// serves the decrypted inner HTTP requests through the policy-enforcing handler.
func (s *httpProxyServer) mitmConnect(w http.ResponseWriter, r *http.Request, ca *ManagedMitmCA, host string, port uint16, client string, mode NetworkMode, allowUpstream bool) {
	tlsConfig, err := ca.TLSConfigForHost(host)
	if err != nil {
		writeTextResponse(w, http.StatusInternalServerError, "error")
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		writeTextResponse(w, http.StatusInternalServerError, "error")
		return
	}
	clientConn, bufrw, err := hj.Hijack()
	if err != nil {
		writeTextResponse(w, http.StatusInternalServerError, "error")
		return
	}
	defer clientConn.Close()

	if _, err := bufrw.WriteString("HTTP/1.1 200 OK\r\n\r\n"); err != nil {
		return
	}
	if err := bufrw.Flush(); err != nil {
		return
	}

	tlsConn := tls.Server(clientConn, tlsConfig)
	handshakeCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		return
	}

	handler := &mitmHandler{
		state:      s.state,
		targetHost: host,
		targetPort: port,
		mode:       mode,
		clientAddr: client,
		transport:  newMitmTransport(s, allowUpstream),
	}

	serveSingleConn(r.Context(), tlsConn, handler)
}

// serveSingleConn serves HTTP requests from a single already-established
// connection until it closes. It adapts net/http (which serves listeners) to a
// pre-accepted conn via a one-shot listener.
func serveSingleConn(ctx context.Context, conn net.Conn, handler http.Handler) {
	listener := newSingleConnListener(conn)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	// Serve returns once the single connection is consumed and the synthetic
	// listener reports closed; the connection lifetime is owned by the caller.
	_ = server.Serve(listener)
}

// singleConnListener yields exactly one connection then blocks until closed,
// returning a net.ErrClosed-style error so http.Server.Serve stops cleanly.
type singleConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
	mu   sync.Mutex
	used bool
}

func newSingleConnListener(conn net.Conn) *singleConnListener {
	return &singleConnListener{conn: conn, done: make(chan struct{})}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.used {
		l.used = true
		l.mu.Unlock()
		return &closeNotifyConn{Conn: l.conn, listener: l}, nil
	}
	l.mu.Unlock()
	<-l.done
	return nil, net.ErrClosed
}

func (l *singleConnListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *singleConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

// closeNotifyConn signals the synthetic listener to stop accepting once the
// single connection is closed by the server.
type closeNotifyConn struct {
	net.Conn
	listener *singleConnListener
}

func (c *closeNotifyConn) Close() error {
	err := c.Conn.Close()
	c.listener.once.Do(func() { close(c.listener.done) })
	return err
}
