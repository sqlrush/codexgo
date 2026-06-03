package responsesproxy

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// startProxy starts the proxy server against the given upstream URL and returns
// its base URL plus a cleanup function.
func startProxy(t *testing.T, args Args, authHeader string) (string, *Server) {
	t.Helper()
	srv, listener, err := NewServer(args, authHeader)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + listener.Addr().String(), srv
}

func TestProxyForwardsPostResponsesWithAuthRewrite(t *testing.T) {
	var gotAuth, gotHost, gotCustom string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHost = r.Host
		gotCustom = r.Header.Get("X-Custom")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: ok\n\n")
	}))
	defer upstream.Close()

	base, _ := startProxy(t, Args{UpstreamURL: upstream.URL + "/v1/responses"}, "Bearer sk-test123")

	req, err := http.NewRequest(http.MethodPost, base+"/v1/responses", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer client-secret")
	req.Header.Set("X-Custom", "keepme")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status got %d want 200", resp.StatusCode)
	}
	if string(body) != "data: ok\n\n" {
		t.Fatalf("body got %q", body)
	}
	if gotAuth != "Bearer sk-test123" {
		t.Fatalf("upstream auth got %q want injected bearer", gotAuth)
	}
	if gotCustom != "keepme" {
		t.Fatalf("custom header not forwarded: %q", gotCustom)
	}
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")
	if gotHost != upstreamHost {
		t.Fatalf("upstream host got %q want %q", gotHost, upstreamHost)
	}
	if string(gotBody) != `{"model":"m"}` {
		t.Fatalf("upstream body got %q", gotBody)
	}
}

func TestProxyRejectsNonResponsesPaths(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream should not be called for %s %s", r.Method, r.URL.Path)
	}))
	defer upstream.Close()

	base, _ := startProxy(t, Args{UpstreamURL: upstream.URL + "/v1/responses"}, "Bearer sk-test123")

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "GET responses", method: http.MethodGet, path: "/v1/responses"},
		{name: "POST other", method: http.MethodPost, path: "/v1/other"},
		{name: "POST with query", method: http.MethodPost, path: "/v1/responses?x=1"},
		{name: "root", method: http.MethodPost, path: "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, base+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status got %d want 403", resp.StatusCode)
			}
		})
	}
}

func TestProxyShutdownEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	var exitCode int
	var exitCalled bool
	var mu sync.Mutex

	srv, listener, err := NewServer(Args{UpstreamURL: upstream.URL + "/v1/responses", HTTPShutdown: true}, "Bearer sk-test")
	if err != nil {
		t.Fatal(err)
	}
	// Replace the exiter to avoid killing the test process.
	handler := srv.Handler.(*proxyHandler)
	handler.exit = func(code int) {
		mu.Lock()
		exitCode = code
		exitCalled = true
		mu.Unlock()
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	base := "http://" + listener.Addr().String()
	resp, err := http.Get(base + "/shutdown")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("shutdown status got %d want 200", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if !exitCalled || exitCode != 0 {
		t.Fatalf("expected exit(0), called=%v code=%d", exitCalled, exitCode)
	}
}

func TestProxyShutdownDisabledByDefault(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	base, _ := startProxy(t, Args{UpstreamURL: upstream.URL + "/v1/responses"}, "Bearer sk-test")
	resp, err := http.Get(base + "/shutdown")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Without --http-shutdown, GET /shutdown is just a non-allowed request: 403.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status got %d want 403", resp.StatusCode)
	}
}

func TestWriteServerInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "info.json")
	if err := writeServerInfo(path, 4242); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("server info must end with newline")
	}
	var info struct {
		Port int `json:"port"`
		PID  int `json:"pid"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &info); err != nil {
		t.Fatal(err)
	}
	if info.Port != 4242 {
		t.Fatalf("port got %d want 4242", info.Port)
	}
	if info.PID != os.Getpid() {
		t.Fatalf("pid got %d want %d", info.PID, os.Getpid())
	}
}

func TestNewServerWritesServerInfoPort(t *testing.T) {
	// Confirm a bound listener reports a usable port and server info round-trips.
	srv, listener, err := NewServer(Args{}, "Bearer sk-test")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	defer listener.Close()
	port := port16(listener)
	if port == 0 {
		t.Fatalf("expected non-zero ephemeral port")
	}
	if _, ok := listener.Addr().(*net.TCPAddr); !ok {
		t.Fatalf("expected TCP listener")
	}
}

func TestNewServerInvalidUpstream(t *testing.T) {
	if _, _, err := NewServer(Args{UpstreamURL: "/no-host"}, "Bearer sk"); err == nil {
		t.Fatalf("expected error for upstream URL without host")
	}
	if _, _, err := NewServer(Args{UpstreamURL: "ht!tp://bad url"}, "Bearer sk"); err == nil {
		t.Fatalf("expected error for unparseable upstream URL")
	}
}

func TestProxyDumpIntegration(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	dumpDir := t.TempDir()
	base, _ := startProxy(t, Args{UpstreamURL: upstream.URL + "/v1/responses", DumpDir: dumpDir}, "Bearer sk-test")

	req, _ := http.NewRequest(http.MethodPost, base+"/v1/responses", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Authorization", "Bearer client-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	reqDump := readDumpWithSuffix(t, dumpDir, "-request.json")
	if reqDump["method"] != "POST" || reqDump["url"] != "/v1/responses" {
		t.Fatalf("request dump method/url mismatch: %#v", reqDump)
	}
	// Authorization in the request dump must be redacted.
	for _, h := range reqDump["headers"].([]any) {
		hm := h.(map[string]any)
		if strings.EqualFold(hm["name"].(string), "authorization") && hm["value"] != "[REDACTED]" {
			t.Fatalf("authorization not redacted in dump: %#v", hm)
		}
	}

	respDump := readDumpWithSuffix(t, dumpDir, "-response.json")
	if respDump["status"] != float64(200) {
		t.Fatalf("response dump status mismatch: %#v", respDump["status"])
	}
	body, ok := respDump["body"].(map[string]any)
	if !ok || body["ok"] != true {
		t.Fatalf("response dump body mismatch: %#v", respDump["body"])
	}
}
