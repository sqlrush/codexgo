package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPTransportJSONResponse(t *testing.T) {
	t.Parallel()
	var gotAccept, gotContentType, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", mimeJSON)
		w.Header().Set(headerSessionID, "sess-1")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, BearerToken: "tok", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	recvCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	frame, err := tr.Receive(recvCtx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(frame, &resp); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if !resp.IsResponse() {
		t.Fatalf("expected response frame, got %s", frame)
	}

	if !strings.Contains(gotAccept, mimeEventStream) || !strings.Contains(gotAccept, mimeJSON) {
		t.Errorf("Accept header=%q", gotAccept)
	}
	if gotContentType != mimeJSON {
		t.Errorf("Content-Type=%q", gotContentType)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization=%q", gotAuth)
	}
}

func TestHTTPTransportSSEResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", mimeEventStream)
		flusher, _ := w.(http.Flusher)
		// Two JSON-RPC frames delivered as SSE data events.
		_, _ = w.Write([]byte(": keep-alive\n"))
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"n\":1}}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"n\":2}}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	for i := 1; i <= 2; i++ {
		recvCtx, cancel := context.WithTimeout(ctx, time.Second)
		frame, err := tr.Receive(recvCtx)
		cancel()
		if err != nil {
			t.Fatalf("Receive %d: %v", i, err)
		}
		var resp Response
		if err := json.Unmarshal(frame, &resp); err != nil {
			t.Fatalf("decode frame %d: %v (%s)", i, err, frame)
		}
		if !resp.IsResponse() {
			t.Fatalf("frame %d not a response: %s", i, frame)
		}
	}
}

func TestHTTPTransportAcceptedStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer tr.Close()

	// A 202 (notification ack) returns no frame and no error.
	if err := tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestHTTPTransportSessionExpired(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First request seeds a session id; the next returns 404.
		if r.Header.Get(headerSessionID) == "" {
			w.Header().Set("Content-Type", mimeJSON)
			w.Header().Set(headerSessionID, "sess-1")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	// Drain the first frame.
	recvCtx, cancel := context.WithTimeout(ctx, time.Second)
	if _, err := tr.Receive(recvCtx); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	cancel()

	// The second request carries the session id and gets a 404 -> session expired.
	err = tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`))
	if err != ErrSessionExpired {
		t.Fatalf("Send err=%v want ErrSessionExpired", err)
	}
}

func TestHTTPTransportErrorStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal boom"))
	}))
	defer srv.Close()

	tr, err := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	defer tr.Close()

	err = tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected http 500 error, got %v", err)
	}
}

func TestHTTPTransportRequiresURL(t *testing.T) {
	t.Parallel()
	if _, err := NewHTTPTransport(HTTPTransportConfig{URL: "  "}); err == nil {
		t.Fatal("expected error for empty url")
	}
}

func TestHTTPTransportClosedSend(t *testing.T) {
	t.Parallel()
	tr, err := NewHTTPTransport(HTTPTransportConfig{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	_ = tr.Close()
	if err := tr.Send(context.Background(), []byte(`{}`)); err != ErrTransportClosed {
		t.Fatalf("Send after close err=%v want ErrTransportClosed", err)
	}
	if _, err := tr.Receive(context.Background()); err != ErrTransportClosed {
		t.Fatalf("Receive after close err=%v want ErrTransportClosed", err)
	}
}
