package networkproxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// startUpstream starts a local HTTP server returning a fixed body and returns
// its host:port.
func startUpstream(t *testing.T, body string) (host string, port uint16, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})}
	go func() { _ = srv.Serve(ln) }()
	ap := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", uint16(ap.Port), func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// startProxy builds and runs a proxy with the given settings and returns it plus
// a cleanup func.
func startProxy(t *testing.T, settings NetworkProxySettings) (*NetworkProxy, func()) {
	t.Helper()
	settings.Enabled = true
	cs, err := BuildConfigState(NetworkProxyConfig{Network: settings}, NetworkProxyConstraints{})
	if err != nil {
		t.Fatalf("BuildConfigState: %v", err)
	}
	state := NewNetworkProxyState(cs)
	state.SetLookupFunc(publicLookup)
	proxy, err := NewBuilder().State(state).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	handle, err := proxy.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return proxy, func() { _ = handle.Shutdown() }
}

func TestHTTPProxyForwardsAllowedRequest(t *testing.T) {
	upHost, upPort, closeUp := startUpstream(t, "hello-upstream")
	defer closeUp()

	settings := DefaultNetworkProxySettings()
	settings.AllowLocalBinding = true
	settings = settings.WithAllowedDomains([]string{upHost})
	proxy, closeProxy := startProxy(t, settings)
	defer closeProxy()

	proxyURL := &url.URL{Scheme: "http", Host: proxy.HTTPAddr().String()}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}

	resp, err := client.Get(fmt.Sprintf("http://%s:%d/", upHost, upPort))
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if string(data) != "hello-upstream" {
		t.Errorf("body = %q, want hello-upstream", data)
	}
}

func TestHTTPProxyBlocksDisallowedHost(t *testing.T) {
	settings := DefaultNetworkProxySettings()
	settings = settings.WithAllowedDomains([]string{"allowed.example"})
	proxy, closeProxy := startProxy(t, settings)
	defer closeProxy()

	proxyURL := &url.URL{Scheme: "http", Host: proxy.HTTPAddr().String()}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}

	resp, err := client.Get("http://blocked.example/")
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if got := resp.Header.Get("x-proxy-error"); got != "blocked-by-allowlist" {
		t.Errorf("x-proxy-error = %q, want blocked-by-allowlist", got)
	}
}

func TestHTTPProxyConnectTunnel(t *testing.T) {
	// Raw TCP echo upstream reachable via CONNECT.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err == nil {
			_, _ = conn.Write(buf)
		}
	}()
	upAddr := ln.Addr().(*net.TCPAddr)

	settings := DefaultNetworkProxySettings()
	settings.AllowLocalBinding = true
	settings = settings.WithAllowedDomains([]string{"127.0.0.1"})
	proxy, closeProxy := startProxy(t, settings)
	defer closeProxy()

	conn, err := net.Dial("tcp", proxy.HTTPAddr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	target := fmt.Sprintf("127.0.0.1:%d", upAddr.Port)
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	respBuf := make([]byte, 64)
	n, err := conn.Read(respBuf)
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if got := string(respBuf[:n]); got[:12] != "HTTP/1.1 200" {
		t.Fatalf("CONNECT response = %q, want 200", got)
	}
	// Tunnel echoes 4 bytes.
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write through tunnel: %v", err)
	}
	echo := make([]byte, 4)
	if _, err := io.ReadFull(conn, echo); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(echo) != "ping" {
		t.Errorf("echo = %q, want ping", echo)
	}
}

func TestSocks5ConnectAllowed(t *testing.T) {
	upHost, upPort, closeUp := startUpstream(t, "socks-upstream")
	defer closeUp()

	settings := DefaultNetworkProxySettings()
	settings.AllowLocalBinding = true
	settings = settings.WithAllowedDomains([]string{upHost})
	proxy, closeProxy := startProxy(t, settings)
	defer closeProxy()

	conn := socks5Connect(t, proxy.SocksAddr().String(), upHost, upPort)
	defer conn.Close()

	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s:%d\r\nConnection: close\r\n\r\n", upHost, upPort)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	data, _ := io.ReadAll(conn)
	if !contains(string(data), "socks-upstream") {
		t.Errorf("response missing upstream body: %q", data)
	}
}

func TestSocks5BlockedHostRejected(t *testing.T) {
	settings := DefaultNetworkProxySettings()
	settings = settings.WithAllowedDomains([]string{"allowed.example"})
	proxy, closeProxy := startProxy(t, settings)
	defer closeProxy()

	conn, err := net.Dial("tcp", proxy.SocksAddr().String())
	if err != nil {
		t.Fatalf("dial socks: %v", err)
	}
	defer conn.Close()
	socks5Handshake(t, conn)
	// CONNECT to a domain that is not allowlisted.
	reply := socks5SendConnect(t, conn, "blocked.example", 80)
	if reply != socksRepConnectionNotAllowed {
		t.Errorf("socks reply = %d, want connection-not-allowed (%d)", reply, socksRepConnectionNotAllowed)
	}
}

func TestSocks5LimitedModeBlocked(t *testing.T) {
	settings := DefaultNetworkProxySettings()
	settings.AllowLocalBinding = true
	settings.Mode = NetworkModeLimited
	settings = settings.WithAllowedDomains([]string{"127.0.0.1"})
	proxy, closeProxy := startProxy(t, settings)
	defer closeProxy()

	conn, err := net.Dial("tcp", proxy.SocksAddr().String())
	if err != nil {
		t.Fatalf("dial socks: %v", err)
	}
	defer conn.Close()
	socks5Handshake(t, conn)
	reply := socks5SendConnect(t, conn, "127.0.0.1", 80)
	if reply != socksRepConnectionNotAllowed {
		t.Errorf("socks reply = %d, want connection-not-allowed in limited mode", reply)
	}
}

func TestBlockedRequestObserverNotified(t *testing.T) {
	settings := DefaultNetworkProxySettings()
	settings = settings.WithAllowedDomains([]string{"allowed.example"})
	settings.Enabled = true
	cs, err := BuildConfigState(NetworkProxyConfig{Network: settings}, NetworkProxyConstraints{})
	if err != nil {
		t.Fatalf("BuildConfigState: %v", err)
	}
	state := NewNetworkProxyState(cs)
	state.SetLookupFunc(publicLookup)

	observed := make(chan BlockedRequest, 1)
	proxy, err := NewBuilder().
		State(state).
		BlockedRequestObserver(BlockedRequestObserverFunc(func(_ context.Context, r BlockedRequest) {
			select {
			case observed <- r:
			default:
			}
		})).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	handle, err := proxy.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer handle.Shutdown()

	proxyURL := &url.URL{Scheme: "http", Host: proxy.HTTPAddr().String()}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	resp, err := client.Get("http://blocked.example/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	select {
	case r := <-observed:
		if r.Host != "blocked.example" {
			t.Errorf("observed host = %q, want blocked.example", r.Host)
		}
		if r.Reason != reasonNotAllowed {
			t.Errorf("observed reason = %q, want %q", r.Reason, reasonNotAllowed)
		}
		if r.Protocol != "http" {
			t.Errorf("observed protocol = %q, want http", r.Protocol)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked-request observer was not notified")
	}

	snapshot := state.BlockedSnapshot()
	if len(snapshot) == 0 {
		t.Error("expected blocked snapshot to contain the entry")
	}
}

func TestProxyDisabledIsNoop(t *testing.T) {
	settings := DefaultNetworkProxySettings() // Enabled defaults false.
	cs, err := BuildConfigState(NetworkProxyConfig{Network: settings}, NetworkProxyConstraints{})
	if err != nil {
		t.Fatalf("BuildConfigState: %v", err)
	}
	state := NewNetworkProxyState(cs)
	proxy, err := NewBuilder().State(state).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	handle, err := proxy.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := handle.Shutdown(); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// --- SOCKS5 test client helpers ---

func socks5Handshake(t *testing.T, conn net.Conn) {
	t.Helper()
	if _, err := conn.Write([]byte{socksVersion5, 0x01, socksAuthNone}); err != nil {
		t.Fatalf("write greeting: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		t.Fatalf("read greeting reply: %v", err)
	}
	if resp[0] != socksVersion5 || resp[1] != socksAuthNone {
		t.Fatalf("unexpected greeting reply %v", resp)
	}
}

// socks5SendConnect sends a CONNECT and returns the reply code.
func socks5SendConnect(t *testing.T, conn net.Conn, host string, port uint16) byte {
	t.Helper()
	req := []byte{socksVersion5, socksCmdConnect, 0x00, socksAtypDomain, byte(len(host))}
	req = append(req, host...)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	req = append(req, portBytes...)
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write connect: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatalf("read connect reply head: %v", err)
	}
	// Drain the bound address + port.
	var addrLen int
	switch head[3] {
	case socksAtypIPv4:
		addrLen = 4
	case socksAtypIPv6:
		addrLen = 16
	case socksAtypDomain:
		l := make([]byte, 1)
		_, _ = io.ReadFull(conn, l)
		addrLen = int(l[0])
	}
	rest := make([]byte, addrLen+2)
	_, _ = io.ReadFull(conn, rest)
	return head[1]
}

// socks5Connect performs a full SOCKS5 CONNECT and returns the tunneled conn.
func socks5Connect(t *testing.T, proxyAddr, host string, port uint16) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial socks: %v", err)
	}
	socks5Handshake(t, conn)
	if reply := socks5SendConnect(t, conn, host, port); reply != socksRepSucceeded {
		conn.Close()
		t.Fatalf("socks connect reply = %d, want success", reply)
	}
	return conn
}

var _ = strconv.Itoa
