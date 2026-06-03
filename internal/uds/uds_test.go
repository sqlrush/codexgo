package uds

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// zeroDeadline clears any deadline (the zero time means "no deadline").
var zeroDeadline = time.Time{}

// bindOrSkip binds a listener at socketPath, skipping the test when the
// environment forbids binding Unix sockets (the Rust tests skip on
// PermissionDenied for the same reason).
func bindOrSkip(t *testing.T, socketPath string) *UnixListener {
	t.Helper()
	listener, err := Bind(socketPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("skipping test: failed to bind unix socket: %v", err)
		}
		t.Fatalf("failed to bind test socket: %v", err)
	}
	return listener
}

func TestPreparePrivateSocketDirectoryCreatesDirectory(t *testing.T) {
	t.Parallel()

	socketDir := filepath.Join(t.TempDir(), "app-server-control")
	if err := PreparePrivateSocketDirectory(socketDir); err != nil {
		t.Fatalf("socket dir should be created: %v", err)
	}

	info, err := os.Stat(socketDir)
	if err != nil {
		t.Fatalf("stat socket dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", socketDir)
	}
}

func TestPreparePrivateSocketDirectoryIsIdempotent(t *testing.T) {
	t.Parallel()

	socketDir := filepath.Join(t.TempDir(), "app-server-control")
	for i := 0; i < 2; i++ {
		if err := PreparePrivateSocketDirectory(socketDir); err != nil {
			t.Fatalf("call %d should succeed: %v", i, err)
		}
	}
}

func TestRegularFilePathIsNotStaleSocketPath(t *testing.T) {
	t.Parallel()

	regularFile := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(regularFile, []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("regular file should be created: %v", err)
	}

	stale, err := IsStaleSocketPath(regularFile)
	if err != nil {
		t.Fatalf("stale socket check should succeed: %v", err)
	}
	if stale {
		t.Fatalf("regular file should not be reported as a stale socket path")
	}
}

func TestBoundListenerPathIsStaleSocketPath(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "socket")
	listener := bindOrSkip(t, socketPath)
	defer listener.Close()

	stale, err := IsStaleSocketPath(socketPath)
	if err != nil {
		t.Fatalf("stale socket check should succeed: %v", err)
	}
	if !stale {
		t.Fatalf("bound listener path should be reported as a stale socket path")
	}
}

func TestStreamRoundTripsDataBetweenListenerAndClient(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "socket")
	listener := bindOrSkip(t, socketPath)
	defer listener.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	var serverErr error
	go func() {
		defer wg.Done()
		server, err := listener.Accept()
		if err != nil {
			serverErr = err
			return
		}
		defer server.Close()

		request := make([]byte, len("request"))
		if _, err := io.ReadFull(server, request); err != nil {
			serverErr = err
			return
		}
		if string(request) != "request" {
			serverErr = errors.New("unexpected request payload")
			return
		}
		if _, err := server.Write([]byte("response")); err != nil {
			serverErr = err
			return
		}
	}()

	client, err := Connect(socketPath)
	if err != nil {
		t.Fatalf("client should connect: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatalf("client should write request: %v", err)
	}
	response := make([]byte, len("response"))
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatalf("client should read response: %v", err)
	}
	if string(response) != "response" {
		t.Fatalf("unexpected response payload: %q", response)
	}

	wg.Wait()
	if serverErr != nil {
		t.Fatalf("server task failed: %v", serverErr)
	}
}

func TestUnixStreamSatisfiesNetConn(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "socket")
	listener := bindOrSkip(t, socketPath)
	defer listener.Close()

	client, err := Connect(socketPath)
	if err != nil {
		t.Fatalf("client should connect: %v", err)
	}
	defer client.Close()

	// Exercise the net.Conn surface so it is covered and verified at runtime.
	var conn net.Conn = client
	if conn.LocalAddr() == nil {
		t.Fatalf("expected non-nil local address")
	}
	if conn.RemoteAddr() == nil {
		t.Fatalf("expected non-nil remote address")
	}
	if err := conn.SetDeadline(zeroDeadline); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if err := conn.SetReadDeadline(zeroDeadline); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if err := conn.SetWriteDeadline(zeroDeadline); err != nil {
		t.Fatalf("set write deadline: %v", err)
	}

	server, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer server.Close()

	if err := client.CloseWrite(); err != nil {
		t.Fatalf("close write half: %v", err)
	}
	// After CloseWrite the server should observe EOF on read.
	buf := make([]byte, 1)
	if _, err := server.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after client CloseWrite, got %v", err)
	}
}

func TestUnixListenerAddr(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "socket")
	listener := bindOrSkip(t, socketPath)
	defer listener.Close()

	addr := listener.Addr()
	if addr == nil {
		t.Fatalf("expected non-nil listener address")
	}
	if addr.String() != socketPath {
		t.Fatalf("listener address = %q, want %q", addr.String(), socketPath)
	}
}

func TestConnectMissingPathFails(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := Connect(socketPath); err == nil {
		t.Fatalf("connect to missing socket should fail")
	}
}

func TestBindExistingPathFails(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "socket")
	listener := bindOrSkip(t, socketPath)
	defer listener.Close()

	// Binding the same path again must fail (the path is in use); this mirrors
	// tokio/std bind, which does not auto-unlink.
	if second, err := Bind(socketPath); err == nil {
		second.Close()
		t.Fatalf("binding an in-use socket path should fail")
	}
}
