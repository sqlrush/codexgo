// Package responsesproxy is a faithful Go port of codex's `responses-api-proxy`
// crate. It runs a minimal forward proxy that accepts exactly
// `POST /v1/responses` on a localhost port and forwards each request to an
// upstream URL (OpenAI by default), rewriting the Authorization header with a
// Bearer token read from stdin.
//
// The CLI flags (--port, --server-info, --upstream-url, --http-shutdown,
// --dump-dir), the server-info JSON, and the optional request/response dump
// format match codex so the proxy is drop-in compatible.
package responsesproxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// Args holds the parsed CLI arguments for the proxy. It mirrors codex's `Args`.
type Args struct {
	// Port to listen on. When nil, an ephemeral port is used.
	Port *uint16
	// ServerInfo is the path of a JSON file to write startup info (single line),
	// including {"port": <u16>}. Empty means "do not write".
	ServerInfo string
	// HTTPShutdown enables the GET /shutdown endpoint.
	HTTPShutdown bool
	// UpstreamURL is the absolute URL requests are forwarded to. When empty, the
	// default OpenAI responses endpoint is used.
	UpstreamURL string
	// DumpDir is a directory where request/response dumps are written as JSON.
	// Empty disables dumping.
	DumpDir string
}

// defaultUpstreamURL is the default forwarding target. It mirrors codex's
// `--upstream-url` default value.
const defaultUpstreamURL = "https://api.openai.com/v1/responses"

// serverInfo is the JSON written to the --server-info file. It mirrors codex's
// `ServerInfo`.
type serverInfo struct {
	Port uint16 `json:"port"`
	PID  uint32 `json:"pid"`
}

// forwardConfig holds the parsed upstream URL and the Host header to send. It
// mirrors codex's `ForwardConfig`.
type forwardConfig struct {
	upstreamURL *url.URL
	hostHeader  string
}

// Exiter abstracts process exit so the shutdown endpoint can be tested. The
// production value calls os.Exit.
type Exiter func(code int)

// RunMain is the library entry point. It reads the auth header from stdin, binds
// the listener, optionally writes server info, and serves requests until the
// process exits. It mirrors codex's `run_main` and blocks until the server stops.
func RunMain(args Args) error {
	authHeader, err := ReadAuthHeaderFromStdin(os.Stdin)
	if err != nil {
		return err
	}

	server, listener, err := NewServer(args, authHeader)
	if err != nil {
		return err
	}

	if args.ServerInfo != "" {
		if err := writeServerInfo(args.ServerInfo, port16(listener)); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "responses-api-proxy listening on %s\n", listener.Addr())

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server stopped unexpectedly: %w", err)
	}
	return errors.New("server stopped unexpectedly")
}

// Server is a configured proxy HTTP server. It is constructed by [NewServer] and
// served with [http.Server.Serve] on the listener returned alongside it.
type Server struct {
	*http.Server
}

// NewServer builds the proxy [http.Server] and a bound listener from args and the
// pre-assembled Authorization header value. It is the primary seam for tests.
func NewServer(args Args, authHeader string) (*Server, net.Listener, error) {
	upstreamRaw := args.UpstreamURL
	if upstreamRaw == "" {
		upstreamRaw = defaultUpstreamURL
	}
	upstreamURL, err := url.Parse(upstreamRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing --upstream-url: %w", err)
	}
	hostHeader := upstreamURL.Host
	if hostHeader == "" {
		return nil, nil, errors.New("upstream URL must include a host")
	}

	config := &forwardConfig{upstreamURL: upstreamURL, hostHeader: hostHeader}

	var dumper *exchangeDumper
	if args.DumpDir != "" {
		dumper, err = newExchangeDumper(args.DumpDir)
		if err != nil {
			return nil, nil, fmt.Errorf("creating --dump-dir: %w", err)
		}
	}

	listener, err := bindListener(args.Port)
	if err != nil {
		return nil, nil, err
	}

	// Use a transport without a response-header or overall timeout so long-lived
	// streaming responses keep flowing, matching codex's Client::timeout(None).
	client := &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}

	handler := &proxyHandler{
		authHeader:   authHeader,
		config:       config,
		dumper:       dumper,
		client:       client,
		httpShutdown: args.HTTPShutdown,
		exit:         os.Exit,
	}

	return &Server{Server: &http.Server{Handler: handler}}, listener, nil
}

// bindListener binds a TCP listener on 127.0.0.1 at the requested port (or an
// ephemeral port when nil). It mirrors codex's `bind_listener`.
func bindListener(port *uint16) (net.Listener, error) {
	p := uint16(0)
	if port != nil {
		p = *port
	}
	addr := fmt.Sprintf("127.0.0.1:%d", p)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind %s: %w", addr, err)
	}
	return listener, nil
}

// writeServerInfo writes the single-line server info JSON to path, creating
// parent directories as needed. It mirrors codex's `write_server_info`.
func writeServerInfo(path string, port uint16) error {
	if parent := filepath.Dir(path); parent != "" && parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("creating server-info parent dir: %w", err)
		}
	}
	info := serverInfo{Port: port, PID: uint32(os.Getpid())}
	data, err := marshalNoHTMLEscape(info)
	if err != nil {
		return fmt.Errorf("serializing server info: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing server info: %w", err)
	}
	return nil
}

// port16 extracts the bound TCP port from a listener.
func port16(listener net.Listener) uint16 {
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		return uint16(tcpAddr.Port)
	}
	return 0
}

// hopByHopResponseHeaders are response headers managed by the transport layer
// and stripped before relaying, matching codex's tiny_http exclusion list.
var hopByHopResponseHeaders = map[string]struct{}{
	"content-length":    {},
	"transfer-encoding": {},
	"connection":        {},
	"trailer":           {},
	"upgrade":           {},
}
