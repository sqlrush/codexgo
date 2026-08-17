package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestClientInitializeHandshake(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(scriptedServer())
	client := NewClient(tr)
	defer client.Close()

	res, err := client.Initialize(context.Background(), InitializeParams{}, time.Second)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if res.ServerInfo.Name != "test-server" || res.ServerInfo.Version != "1.2.3" {
		t.Fatalf("serverInfo=%+v", res.ServerInfo)
	}
	if res.Instructions == nil || *res.Instructions != "hello" {
		t.Fatalf("instructions=%v", res.Instructions)
	}

	// The default protocol version must be filled in, and an initialized
	// notification must have been sent.
	if !sentContainsMethod(t, tr, MethodInitialize) {
		t.Error("initialize request not sent")
	}
	if !sentContainsMethod(t, tr, MethodInitializedNotify) {
		t.Error("initialized notification not sent")
	}
}

func TestClientInitializeTwiceFails(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(scriptedServer())
	client := NewClient(tr)
	defer client.Close()

	if _, err := client.Initialize(context.Background(), InitializeParams{}, time.Second); err != nil {
		t.Fatalf("first Initialize: %v", err)
	}
	_, err := client.Initialize(context.Background(), InitializeParams{}, time.Second)
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second Initialize err=%v want ErrAlreadyInitialized", err)
	}
}

func TestClientOperationsRequireInit(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(scriptedServer())
	client := NewClient(tr)
	defer client.Close()

	_, err := client.ListTools(context.Background(), nil, time.Second)
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("ListTools before init err=%v want ErrNotInitialized", err)
	}
}

func TestClientListAndCallTool(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(scriptedServer())
	client := NewClient(tr)
	defer client.Close()
	mustInit(t, client)

	toolsList, err := client.ListAllTools(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("ListAllTools: %v", err)
	}
	if len(toolsList) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(toolsList))
	}

	res, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"msg":"hi"}`), nil, time.Second)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content=%+v", res.Content)
	}
}

func TestClientCallToolRejectsNonObjectArgs(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(scriptedServer())
	client := NewClient(tr)
	defer client.Close()
	mustInit(t, client)

	_, err := client.CallTool(context.Background(), "echo", json.RawMessage(`[1,2,3]`), nil, time.Second)
	if err == nil {
		t.Fatal("expected error for non-object arguments")
	}
}

func TestClientReadResource(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(scriptedServer())
	client := NewClient(tr)
	defer client.Close()
	mustInit(t, client)

	res, err := client.ReadResource(context.Background(), "file:///x", time.Second)
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents=%+v", res.Contents)
	}
}

func TestClientCallContextCancel(t *testing.T) {
	t.Parallel()
	// A server that never replies to anything but initialize/list.
	tr := newFakeTransport(func(req Response) []json.RawMessage {
		switch req.Method {
		case MethodInitialize:
			return []json.RawMessage{resultFrame(reqID(req), `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`)}
		case MethodInitializedNotify:
			return nil
		default:
			return nil // never respond -> force timeout/cancel
		}
	})
	client := NewClient(tr)
	defer client.Close()
	mustInit(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := client.Ping(ctx, 0)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v want DeadlineExceeded", err)
	}
}

func TestClientErrorResponse(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(func(req Response) []json.RawMessage {
		switch req.Method {
		case MethodInitialize:
			return []json.RawMessage{resultFrame(reqID(req), `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`)}
		case MethodInitializedNotify:
			return nil
		default:
			return []json.RawMessage{errorFrame(reqID(req), -32000, "kaboom")}
		}
	})
	client := NewClient(tr)
	defer client.Close()
	mustInit(t, client)

	_, err := client.ListTools(context.Background(), nil, time.Second)
	if err == nil {
		t.Fatal("expected rpc error")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError in chain, got %v", err)
	}
	if rpcErr.Code != -32000 {
		t.Fatalf("code=%d", rpcErr.Code)
	}
}

func TestClientCloseWakesWaiters(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(func(req Response) []json.RawMessage {
		switch req.Method {
		case MethodInitialize:
			return []json.RawMessage{resultFrame(reqID(req), `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`)}
		case MethodInitializedNotify:
			return nil
		default:
			return nil
		}
	})
	client := NewClient(tr)
	mustInit(t, client)

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Ping(context.Background(), 0)
	}()
	// Give the goroutine time to register its pending request.
	time.Sleep(20 * time.Millisecond)
	_ = client.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after close")
		}
	case <-time.After(time.Second):
		t.Fatal("waiter not woken by Close")
	}

	// Operations after Close fail with ErrClientShutDown.
	_, err := client.ListTools(context.Background(), nil, time.Second)
	if !errors.Is(err, ErrClientShutDown) {
		t.Fatalf("post-close ListTools err=%v want ErrClientShutDown", err)
	}
}

func TestClientServerRequestElicitation(t *testing.T) {
	t.Parallel()
	handlerCalled := make(chan string, 1)
	tr := newFakeTransport(scriptedServer())
	client := NewClient(tr, WithServerRequestHandler(func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
		handlerCalled <- method
		return json.Marshal(ElicitationResult{Action: ElicitationActionAccept})
	}))
	defer client.Close()
	mustInit(t, client)

	// Server initiates an elicitation/create request.
	tr.pushServerMessage(json.RawMessage(`{"jsonrpc":"2.0","id":999,"method":"elicitation/create","params":{"message":"yo"}}`))

	select {
	case m := <-handlerCalled:
		if m != MethodElicitationCreate {
			t.Fatalf("handler method=%q", m)
		}
	case <-time.After(time.Second):
		t.Fatal("server request handler not invoked")
	}

	// The reply is written from a goroutine after the handler returns; poll for
	// the client to write a reply addressed to id 999.
	deadline := time.After(time.Second)
	for {
		if sentContainsID(t, tr, "999") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no reply written for server request id 999")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestDeclineElicitation(t *testing.T) {
	t.Parallel()
	raw, err := declineElicitation(context.Background(), MethodElicitationCreate, nil)
	if err != nil {
		t.Fatalf("declineElicitation: %v", err)
	}
	var res ElicitationResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Action != ElicitationActionDecline {
		t.Fatalf("action=%q want decline", res.Action)
	}

	if _, err := declineElicitation(context.Background(), "some/other", nil); err == nil {
		t.Fatal("expected error for non-elicitation request")
	}
}

// mustInit runs the initialize handshake or fails the test.
func mustInit(t *testing.T, client *Client) {
	t.Helper()
	if _, err := client.Initialize(context.Background(), InitializeParams{}, time.Second); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
}

func sentContainsMethod(t *testing.T, tr *fakeTransport, method string) bool {
	t.Helper()
	tr.sentMu.Lock()
	defer tr.sentMu.Unlock()
	for _, frame := range tr.sent {
		var msg Response
		if err := json.Unmarshal(frame, &msg); err != nil {
			continue
		}
		if msg.Method == method {
			return true
		}
	}
	return false
}

func sentContainsID(t *testing.T, tr *fakeTransport, id string) bool {
	t.Helper()
	tr.sentMu.Lock()
	defer tr.sentMu.Unlock()
	for _, frame := range tr.sent {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(frame, &msg); err != nil {
			continue
		}
		if rawID, ok := msg["id"]; ok && string(rawID) == id {
			return true
		}
	}
	return false
}
