package networkproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// hopByHopHeaders are stripped from forwarded requests/responses.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Connection",
	"Proxy-Authorization",
	"Proxy-Authenticate",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// httpProxyServer is the loopback HTTP forward proxy. It enforces policy on both
// plain HTTP forwarding and HTTPS CONNECT tunnels, plus the macOS x-unix-socket
// escape hatch.
type httpProxyServer struct {
	state   *NetworkProxyState
	decider NetworkPolicyDecider
	dialer  *net.Dialer
}

func newHTTPProxyServer(state *NetworkProxyState, decider NetworkPolicyDecider) *httpProxyServer {
	return &httpProxyServer{
		state:   state,
		decider: decider,
		dialer:  &net.Dialer{Timeout: 30 * time.Second},
	}
}

// runHTTPProxy serves the HTTP forward proxy on the listener until ctx is
// cancelled.
func runHTTPProxy(ctx context.Context, state *NetworkProxyState, listener net.Listener, decider NetworkPolicyDecider) error {
	srv := newHTTPProxyServer(state, decider)
	httpServer := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 30 * time.Second,
		// CONNECT hijacks the connection, so no idle/write timeouts on the server.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	if err := httpServer.Serve(listener); err != nil && !isClosedErr(err) {
		return fmt.Errorf("serve HTTP proxy: %w", err)
	}
	return nil
}

func (s *httpProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.handleConnect(w, r)
		return
	}
	s.handlePlain(w, r)
}

func clientAddr(r *http.Request) string {
	return r.RemoteAddr
}

// handleConnect handles HTTPS CONNECT tunnels.
func (s *httpProxyServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	host, port, ok := splitHostPort(r.Host, 443)
	host = NormalizeHost(host)
	if !ok || host == "" {
		writeTextResponse(w, http.StatusBadRequest, "missing authority")
		return
	}
	client := clientAddr(r)

	if !s.state.Enabled() {
		s.proxyDisabledResponse(w, ctx, host, port, client, "CONNECT", ProtocolHTTPSConnect, false)
		return
	}

	request := NetworkPolicyRequest{
		Protocol:   ProtocolHTTPSConnect,
		Host:       host,
		Port:       port,
		ClientAddr: client,
		Method:     "CONNECT",
	}
	decision := evaluateHostPolicy(ctx, s.state, s.decider, request)
	if decision.Kind == DecisionDeny {
		s.recordConnectBlock(ctx, host, port, client, decision.Reason, decision.Decision, decision.Source, nil)
		writeBlockedText(w, decision.Reason)
		return
	}

	mode := s.state.NetworkMode()
	hostHasHooks := s.state.HostHasMitmHooks(host)
	connectNeedsMitm := mode == NetworkModeLimited || hostHasHooks

	if connectNeedsMitm && !s.state.MitmEnabled() {
		// CONNECT requires MITM to enforce inner-request policy, but MITM is not
		// enabled. This is a faithful port: we block rather than tunnel.
		s.emitBlock(blockDecisionAuditArgs{
			source:        protocol.NetworkDecisionSourceModeGuard,
			reason:        reasonMitmRequired,
			protocol:      ProtocolHTTPSConnect,
			serverAddress: host,
			serverPort:    port,
			method:        "CONNECT",
			hasMethod:     true,
			clientAddr:    client,
			hasClient:     client != "",
		})
		modeCopy := mode
		s.recordConnectBlock(ctx, host, port, client, reasonMitmRequired, protocol.NetworkPolicyDecisionDeny, protocol.NetworkDecisionSourceModeGuard, &modeCopy)
		writeBlockedText(w, reasonMitmRequired)
		return
	}

	// Establish the upstream tunnel before hijacking so we can surface errors.
	allowUpstream := s.state.AllowUpstreamProxy()
	upstream, err := s.dialConnectTarget(ctx, host, port, allowUpstream)
	if err != nil {
		writeTextResponse(w, http.StatusBadGateway, "upstream failure")
		return
	}
	defer upstream.Close()

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

	tunnel(clientConn, upstream)
}

// dialConnectTarget dials the CONNECT target, optionally via an upstream HTTP
// proxy, enforcing the local/private target policy on direct dials.
func (s *httpProxyServer) dialConnectTarget(ctx context.Context, host string, port uint16, allowUpstream bool) (net.Conn, error) {
	if allowUpstream {
		if proxy := proxyForConnect(); proxy != nil {
			return s.dialViaUpstreamProxy(ctx, proxy, host, port)
		}
	}
	return s.dialChecked(ctx, host, port)
}

// dialChecked dials a host:port, rejecting non-public targets unless local
// binding is allowed (SSRF defense, mirroring TargetCheckedTcpConnector).
func (s *httpProxyServer) dialChecked(ctx context.Context, host string, port uint16) (net.Conn, error) {
	address := net.JoinHostPort(host, strconv.Itoa(int(port)))
	if !s.state.AllowLocalBinding() {
		// Resolve and reject non-public IPs before connecting.
		if addr, err := netip.ParseAddr(host); err == nil {
			if isNonPublicIP(addr) {
				return nil, fmt.Errorf("network target rejected by policy")
			}
		} else {
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", host, err)
			}
			for _, ip := range ips {
				if isNonPublicIP(ip) {
					return nil, fmt.Errorf("network target rejected by policy")
				}
			}
		}
	}
	conn, err := s.dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", address, err)
	}
	return conn, nil
}

// dialViaUpstreamProxy establishes a CONNECT tunnel through an upstream HTTP
// proxy.
func (s *httpProxyServer) dialViaUpstreamProxy(ctx context.Context, proxy *url.URL, host string, port uint16) (net.Conn, error) {
	proxyAddr := proxy.Host
	if proxy.Port() == "" {
		proxyAddr = net.JoinHostPort(proxy.Hostname(), "80")
	}
	conn, err := s.dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("dial upstream proxy %s: %w", proxyAddr, err)
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write upstream CONNECT: %w", err)
	}
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 1)
	for !strings.Contains(string(buf), "\r\n\r\n") {
		n, err := conn.Read(tmp)
		if err != nil || n == 0 {
			conn.Close()
			return nil, fmt.Errorf("read upstream CONNECT response: %w", err)
		}
		buf = append(buf, tmp[0])
		if len(buf) > 4096 {
			conn.Close()
			return nil, fmt.Errorf("upstream CONNECT response too large")
		}
	}
	if !strings.HasPrefix(string(buf), "HTTP/1.1 200") && !strings.HasPrefix(string(buf), "HTTP/1.0 200") {
		conn.Close()
		return nil, fmt.Errorf("upstream proxy refused CONNECT: %s", strings.SplitN(string(buf), "\r\n", 2)[0])
	}
	return conn, nil
}

// handlePlain handles plain (absolute-form) HTTP forward-proxy requests, plus
// the x-unix-socket escape hatch.
func (s *httpProxyServer) handlePlain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	client := clientAddr(r)
	methodAllowed := s.state.MethodAllowed(r.Method)

	if socketPath := r.Header.Get("x-unix-socket"); socketPath != "" {
		s.handleUnixSocket(w, r, socketPath, methodAllowed, client)
		return
	}

	if r.URL == nil || r.URL.Host == "" {
		writeTextResponse(w, http.StatusBadRequest, "missing host")
		return
	}
	host, port, _ := splitHostPort(r.URL.Host, defaultPortForScheme(r.URL.Scheme))
	host = NormalizeHost(host)
	if reason := validateAbsoluteFormHostHeader(r); reason != "" {
		writeTextResponse(w, http.StatusBadRequest, reason)
		return
	}

	if !s.state.Enabled() {
		s.proxyDisabledResponse(w, ctx, host, port, client, r.Method, ProtocolHTTP, false)
		return
	}

	request := NetworkPolicyRequest{
		Protocol:   ProtocolHTTP,
		Host:       host,
		Port:       port,
		ClientAddr: client,
		Method:     r.Method,
	}
	decision := evaluateHostPolicy(ctx, s.state, s.decider, request)
	if decision.Kind == DecisionDeny {
		details := &policyDecisionDetails{
			decision: decision.Decision, reason: decision.Reason, source: decision.Source,
			protocol: ProtocolHTTP, host: host, port: port,
		}
		s.recordPlainBlock(ctx, host, port, client, r.Method, decision.Reason, decision.Decision, decision.Source, nil)
		writeJSONBlocked(w, host, decision.Reason, details)
		return
	}

	if !methodAllowed {
		s.emitBlock(blockDecisionAuditArgs{
			source: protocol.NetworkDecisionSourceModeGuard, reason: reasonMethodNotAllowed,
			protocol: ProtocolHTTP, serverAddress: host, serverPort: port,
			method: r.Method, hasMethod: true, clientAddr: client, hasClient: client != "",
		})
		details := &policyDecisionDetails{
			decision: protocol.NetworkPolicyDecisionDeny, reason: reasonMethodNotAllowed,
			source: protocol.NetworkDecisionSourceModeGuard, protocol: ProtocolHTTP, host: host, port: port,
		}
		limited := NetworkModeLimited
		s.recordPlainBlock(ctx, host, port, client, r.Method, reasonMethodNotAllowed, protocol.NetworkPolicyDecisionDeny, protocol.NetworkDecisionSourceModeGuard, &limited)
		writeJSONBlocked(w, host, reasonMethodNotAllowed, details)
		return
	}

	s.forwardPlain(w, r, host, port)
}

// forwardPlain forwards an allowed plain HTTP request to the upstream.
func (s *httpProxyServer) forwardPlain(w http.ResponseWriter, r *http.Request, host string, port uint16) {
	allowUpstream := s.state.AllowUpstreamProxy()
	target := &url.URL{Scheme: r.URL.Scheme, Host: r.URL.Host}
	if target.Scheme == "" {
		target.Scheme = "http"
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			removeHopByHopHeaders(req.Header)
		},
		Transport: s.transportFor(allowUpstream),
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, _ error) {
			writeTextResponse(rw, http.StatusBadGateway, "upstream failure")
		},
		ModifyResponse: func(resp *http.Response) error {
			removeHopByHopHeaders(resp.Header)
			return nil
		},
	}
	proxy.ServeHTTP(w, r)
}

// transportFor builds an http.Transport that enforces the local-target policy on
// dial and optionally honors an upstream HTTP proxy.
func (s *httpProxyServer) transportFor(allowUpstream bool) *http.Transport {
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			host, portStr, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("split address %q: %w", address, err)
			}
			p, _ := strconv.ParseUint(portStr, 10, 16)
			return s.dialChecked(ctx, host, uint16(p))
		},
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if allowUpstream {
		if proxy := proxyForHTTP(); proxy != nil {
			tr.Proxy = http.ProxyURL(proxy)
		}
	}
	return tr
}

// handleUnixSocket handles the macOS-only x-unix-socket escape hatch.
func (s *httpProxyServer) handleUnixSocket(w http.ResponseWriter, r *http.Request, socketPath string, methodAllowed bool, client string) {
	ctx := r.Context()
	if !s.state.Enabled() {
		s.proxyDisabledResponse(w, ctx, socketPath, 0, client, r.Method, ProtocolHTTP, true)
		return
	}
	if !methodAllowed {
		s.emitBlock(blockDecisionAuditArgs{
			source: protocol.NetworkDecisionSourceModeGuard, reason: reasonMethodNotAllowed,
			protocol: ProtocolHTTP, serverAddress: "unix-socket", serverPort: 0,
			method: r.Method, hasMethod: true, clientAddr: client, hasClient: client != "",
		})
		writeJSONBlocked(w, "unix-socket", reasonMethodNotAllowed, nil)
		return
	}
	if !unixSocketPermissionsSupported() {
		s.emitBlock(blockDecisionAuditArgs{
			source: protocol.NetworkDecisionSourceProxyState, reason: reasonUnixSocketUnsupported,
			protocol: ProtocolHTTP, serverAddress: "unix-socket", serverPort: 0,
			method: r.Method, hasMethod: true, clientAddr: client, hasClient: client != "",
		})
		writeTextResponse(w, http.StatusNotImplemented, "unix sockets unsupported")
		return
	}
	if !s.state.IsUnixSocketAllowed(socketPath) {
		s.emitBlock(blockDecisionAuditArgs{
			source: protocol.NetworkDecisionSourceProxyState, reason: reasonNotAllowed,
			protocol: ProtocolHTTP, serverAddress: "unix-socket", serverPort: 0,
			method: r.Method, hasMethod: true, clientAddr: client, hasClient: client != "",
		})
		writeJSONBlocked(w, "unix-socket", reasonNotAllowed, nil)
		return
	}
	s.emitAllow(blockDecisionAuditArgs{
		source: protocol.NetworkDecisionSourceProxyState, reason: "allow",
		protocol: ProtocolHTTP, serverAddress: "unix-socket", serverPort: 0,
		method: r.Method, hasMethod: true, clientAddr: client, hasClient: client != "",
	})
	s.forwardUnixSocket(w, r, socketPath)
}

func (s *httpProxyServer) forwardUnixSocket(w http.ResponseWriter, r *http.Request, socketPath string) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return s.dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "unix"
			req.Header.Del("x-unix-socket")
			removeHopByHopHeaders(req.Header)
		},
		Transport: transport,
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, _ error) {
			writeTextResponse(rw, http.StatusBadGateway, "unix socket proxy failed")
		},
	}
	proxy.ServeHTTP(w, r)
}

// proxyDisabledResponse emits audit + telemetry and writes a 503 disabled
// response, mirroring Rust's `proxy_disabled_response`.
func (s *httpProxyServer) proxyDisabledResponse(w http.ResponseWriter, ctx context.Context, host string, port uint16, client, method string, proto NetworkProtocol, unixEndpointOverride bool) {
	auditAddress, auditPort := host, port
	if unixEndpointOverride {
		auditAddress, auditPort = "unix-socket", 0
	}
	s.emitBlock(blockDecisionAuditArgs{
		source: protocol.NetworkDecisionSourceProxyState, reason: reasonProxyDisabled,
		protocol: proto, serverAddress: auditAddress, serverPort: auditPort,
		method: method, hasMethod: method != "", clientAddr: client, hasClient: client != "",
	})
	deny := string(protocol.NetworkPolicyDecisionDeny)
	src := string(protocol.NetworkDecisionSourceProxyState)
	p := port
	s.state.RecordBlocked(ctx, newBlockedRequest(BlockedRequestArgs{
		Host: host, Reason: reasonProxyDisabled, Client: optStr(client), Method: optStr(method),
		Protocol: proto.PolicyProtocol(), Decision: &deny, Source: &src, Port: &p,
	}))
	writeTextResponse(w, http.StatusServiceUnavailable, blockedMessage(reasonProxyDisabled))
}

func (s *httpProxyServer) recordConnectBlock(ctx context.Context, host string, port uint16, client, reason string, decision protocol.NetworkPolicyDecision, source protocol.NetworkDecisionSource, mode *NetworkMode) {
	d := string(decision)
	sr := string(source)
	p := port
	s.state.RecordBlocked(ctx, newBlockedRequest(BlockedRequestArgs{
		Host: host, Reason: reason, Client: optStr(client), Method: strPtr("CONNECT"),
		Mode: mode, Protocol: "http-connect", Decision: &d, Source: &sr, Port: &p,
	}))
}

func (s *httpProxyServer) recordPlainBlock(ctx context.Context, host string, port uint16, client, method, reason string, decision protocol.NetworkPolicyDecision, source protocol.NetworkDecisionSource, mode *NetworkMode) {
	d := string(decision)
	sr := string(source)
	p := port
	s.state.RecordBlocked(ctx, newBlockedRequest(BlockedRequestArgs{
		Host: host, Reason: reason, Client: optStr(client), Method: optStr(method),
		Mode: mode, Protocol: "http", Decision: &d, Source: &sr, Port: &p,
	}))
}

func (s *httpProxyServer) emitBlock(args blockDecisionAuditArgs) {
	emitBlockDecisionAuditEvent(s.state.auditSink, s.state.metadata, args)
}

func (s *httpProxyServer) emitAllow(args blockDecisionAuditArgs) {
	emitAllowDecisionAuditEvent(s.state.auditSink, s.state.metadata, args)
}

// validateAbsoluteFormHostHeader ensures the Host header matches an absolute-form
// request target, mirroring Rust's `validate_absolute_form_host_header`. Returns
// the rejection reason, or "" when valid.
func validateAbsoluteFormHostHeader(r *http.Request) string {
	if r.URL == nil || r.URL.Scheme == "" {
		return ""
	}
	hostHeader := r.Header.Get("Host")
	if hostHeader == "" {
		hostHeader = r.Host
	}
	if hostHeader == "" {
		return ""
	}
	targetHost, targetPort, _ := splitHostPort(r.URL.Host, defaultPortForScheme(r.URL.Scheme))
	headerHost, headerPort, hasHeaderPort := splitHostPort(hostHeader, 0)
	if !strings.EqualFold(headerHost, targetHost) {
		return "Host header does not match request target"
	}
	if hasHeaderPort {
		if headerPort != targetPort {
			return "Host header does not match request target"
		}
		return ""
	}
	if targetPort != defaultPortForScheme(r.URL.Scheme) {
		return "Host header does not match request target"
	}
	return ""
}

func removeHopByHopHeaders(h http.Header) {
	if c := h.Get("Connection"); c != "" {
		for _, token := range strings.Split(c, ",") {
			token = strings.TrimSpace(token)
			if token != "" {
				h.Del(token)
			}
		}
	}
	for _, name := range hopByHopHeaders {
		h.Del(name)
	}
}

func splitHostPort(authority string, defaultPort uint16) (host string, port uint16, hadPort bool) {
	authority = strings.TrimSpace(authority)
	if authority == "" {
		return "", defaultPort, false
	}
	if strings.HasPrefix(authority, "[") {
		if end := strings.IndexByte(authority, ']'); end >= 0 {
			host = authority[1:end]
			rest := authority[end+1:]
			if strings.HasPrefix(rest, ":") {
				if p, err := strconv.ParseUint(rest[1:], 10, 16); err == nil {
					return host, uint16(p), true
				}
			}
			return host, defaultPort, false
		}
	}
	if strings.Count(authority, ":") == 1 {
		h, portStr, _ := strings.Cut(authority, ":")
		if p, err := strconv.ParseUint(portStr, 10, 16); err == nil {
			return h, uint16(p), true
		}
		return h, defaultPort, false
	}
	return authority, defaultPort, false
}

func defaultPortForScheme(scheme string) uint16 {
	switch strings.ToLower(scheme) {
	case "https", "wss":
		return 443
	default:
		return 80
	}
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// tunnel bidirectionally copies between two connections until both close.
func tunnel(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.SetReadDeadline(time.Now())
		}
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
}

func isClosedErr(err error) bool {
	return err == http.ErrServerClosed || errIsNetClosed(err)
}
