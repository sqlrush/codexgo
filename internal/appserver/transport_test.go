package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/uds"
)

// newServeAssembly builds an assembly with a completing mock turn for transport
// tests.
func newServeAssembly(t *testing.T) *Assembly {
	t.Helper()
	asm, err := Assemble(AssemblyConfig{
		ModelClientFactory: mockClientFactory(completedTurn("hi")),
		CodexHome:          "/home/.codex",
		DefaultModel:       "gpt-test",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	return asm
}

// encodeLine marshals a request envelope to a single JSON line.
func encodeLine(t *testing.T, id int64, method string, params any) string {
	t.Helper()
	req := requestEnvelope(t, id, method, params)
	b, err := json.Marshal(appserverproto.NewRequestMessage(req))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return string(b) + "\n"
}

func TestServeStdioInitialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	asm := newServeAssembly(t)

	// Drive a single initialize request through the stdio serve loop.
	input := encodeLine(t, 1, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: "stdio-test", Version: "1.0"},
	})
	r := strings.NewReader(input)
	pr, pw := io.Pipe()

	done := make(chan error, 1)
	go func() {
		done <- ServeStdioWithProcessor(ctx, asm, Defaults{
			Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "stdio-agent",
		}, r, pw)
		_ = pw.Close()
	}()

	// Read the first line: the initialize response.
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	if !scanner.Scan() {
		t.Fatalf("no response line: %v", scanner.Err())
	}
	var msg appserverproto.JSONRPCMessage
	if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if msg.Kind != appserverproto.MessageKindResponse {
		t.Fatalf("first message kind = %d, want response", msg.Kind)
	}
	var resp appserverproto.InitializeResponse
	if err := json.Unmarshal(msg.Response.Result, &resp); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	if resp.UserAgent != "stdio-agent" {
		t.Fatalf("UserAgent = %q", resp.UserAgent)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serve loop did not exit after cancel")
	}
}

func TestWebSocketHandlerInitialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	asm := newServeAssembly(t)
	factory := NewProcessorFactory(asm, Defaults{
		Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "ws-agent",
	})

	// Build a connected pair of websocket Conns over an in-memory pipe by serving
	// the handler through httptest would require a real listener; instead drive
	// ServeWebSocketConn directly against a piped pair using websocket's net
	// adapter is non-trivial. Use a real loopback server.
	srv := newWSTestServer(t, factory)
	defer srv.stop()

	conn, _, err := websocket.Dial(ctx, srv.url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// Send initialize as one text frame.
	req := requestEnvelope(t, 1, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: "ws-test", Version: "1.0"},
	})
	frame, err := json.Marshal(appserverproto.NewRequestMessage(req))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg appserverproto.JSONRPCMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if msg.Kind != appserverproto.MessageKindResponse {
		t.Fatalf("response kind = %d", msg.Kind)
	}
	var resp appserverproto.InitializeResponse
	if err := json.Unmarshal(msg.Response.Result, &resp); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if resp.UserAgent != "ws-agent" {
		t.Fatalf("UserAgent = %q", resp.UserAgent)
	}
}

func TestServeUDSInitialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	asm := newServeAssembly(t)
	factory := NewProcessorFactory(asm, Defaults{
		Model: "gpt-test", ProviderID: "openai", Cwd: "/work", UserAgent: "uds-agent",
	})

	// macOS limits socket paths to ~104 bytes; keep the path short.
	socketPath := filepath.Join(t.TempDir(), "as.sock")

	serveErr := make(chan error, 1)
	go func() { serveErr <- ServeUDS(ctx, factory, socketPath) }()

	// Connect once the listener is up. Bind is synchronous in ServeUDS but runs
	// in the goroutine; retry briefly until Connect succeeds.
	var stream *uds.UnixStream
	deadline := time.Now().Add(2 * time.Second)
	for {
		s, err := uds.Connect(socketPath)
		if err == nil {
			stream = s
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connect uds: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer stream.Close()

	// Send initialize.
	if _, err := stream.Write([]byte(encodeLine(t, 1, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: "uds-test", Version: "1.0"},
	}))); err != nil {
		t.Fatalf("write: %v", err)
	}

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	if !scanner.Scan() {
		t.Fatalf("no response: %v", scanner.Err())
	}
	var msg appserverproto.JSONRPCMessage
	if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if msg.Kind != appserverproto.MessageKindResponse {
		t.Fatalf("response kind = %d", msg.Kind)
	}
	var resp appserverproto.InitializeResponse
	if err := json.Unmarshal(msg.Response.Result, &resp); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if resp.UserAgent != "uds-agent" {
		t.Fatalf("UserAgent = %q", resp.UserAgent)
	}

	cancel()
	select {
	case <-serveErr:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeUDS did not exit after cancel")
	}
}
