package appserver

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// wsTestServer wraps an httptest server hosting the WebSocket handler.
type wsTestServer struct {
	server *httptest.Server
	url    string
}

// newWSTestServer starts a loopback HTTP server hosting the WebSocket handler.
func newWSTestServer(t *testing.T, factory ProcessorFactory) *wsTestServer {
	t.Helper()
	srv := httptest.NewServer(WebSocketHandler(factory))
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	return &wsTestServer{server: srv, url: url}
}

// stop shuts down the test server.
func (s *wsTestServer) stop() { s.server.Close() }
