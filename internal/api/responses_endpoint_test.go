package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/client"
)

func TestResponsesClientStreamRequest(t *testing.T) {
	var gotAuth, gotAccept, gotSession string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotSession = r.Header.Get("session-id")
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Errorf("expected request body")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("OpenAI-Model", "server-model")
		w.Header().Set("X-Request-Id", "req-42")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hi\"}]}}\n\n"))
		if f != nil {
			f.Flush()
		}
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp1\"}}\n\n"))
	}))
	defer srv.Close()

	provider := Provider{
		Name:              "test",
		BaseURL:           srv.URL,
		Retry:             RetryConfig{MaxAttempts: 0, BaseDelay: time.Millisecond, Retry5xx: true, RetryTransport: true},
		StreamIdleTimeout: 2 * time.Second,
	}
	transport := client.NewHTTPClientTransport(srv.Client())
	c := NewResponsesClient(transport, provider, NewBearerAuth("tok"))

	sid := "sess-1"
	stream, apiErr := c.StreamRequest(context.Background(), ResponsesApiRequest{
		Model:      "m",
		ToolChoice: "auto",
		Stream:     true,
	}, ResponsesOptions{SessionID: &sid})
	if apiErr != nil {
		t.Fatalf("stream request: %v", apiErr)
	}

	var kinds []ResponseEventKind
	for res := range stream.Events {
		if res.Err != nil {
			t.Fatalf("stream error: %v", res.Err)
		}
		kinds = append(kinds, res.Event.Kind)
	}

	if gotAuth != "Bearer tok" {
		t.Fatalf("missing auth header, got %q", gotAuth)
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("missing accept header, got %q", gotAccept)
	}
	if gotSession != "sess-1" {
		t.Fatalf("missing session header, got %q", gotSession)
	}
	if stream.UpstreamRequestID == nil || *stream.UpstreamRequestID != "req-42" {
		t.Fatalf("unexpected upstream request id: %v", stream.UpstreamRequestID)
	}
	// First event should be the server model from the header.
	if len(kinds) < 3 {
		t.Fatalf("expected at least 3 events, got %v", kinds)
	}
	if kinds[0] != ResponseEventServerModel {
		t.Fatalf("expected server model first, got %v", kinds[0])
	}
	if kinds[len(kinds)-1] != ResponseEventCompleted {
		t.Fatalf("expected completed last, got %v", kinds[len(kinds)-1])
	}
}

func TestResponsesClientRetriesOn5xx(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\"}}\n\n"))
	}))
	defer srv.Close()

	provider := Provider{
		BaseURL:           srv.URL,
		Retry:             RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, Retry5xx: true, RetryTransport: true},
		StreamIdleTimeout: 2 * time.Second,
	}
	c := NewResponsesClient(client.NewHTTPClientTransport(srv.Client()), provider, NoOpAuth{})
	stream, apiErr := c.StreamRequest(context.Background(), ResponsesApiRequest{Model: "m", ToolChoice: "auto"}, ResponsesOptions{})
	if apiErr != nil {
		t.Fatalf("stream request: %v", apiErr)
	}
	for range stream.Events {
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts (1 retry), got %d", attempts)
	}
}

func TestResponsesClientReturnsTransportErrorOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte("bad"))
	}))
	defer srv.Close()
	provider := Provider{BaseURL: srv.URL, Retry: RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond, Retry5xx: true}, StreamIdleTimeout: time.Second}
	c := NewResponsesClient(client.NewHTTPClientTransport(srv.Client()), provider, NoOpAuth{})
	_, apiErr := c.StreamRequest(context.Background(), ResponsesApiRequest{Model: "m", ToolChoice: "auto"}, ResponsesOptions{})
	if apiErr == nil || apiErr.Kind != APIErrorTransport {
		t.Fatalf("expected transport error, got %v", apiErr)
	}
	if apiErr.Transport == nil || apiErr.Transport.Status != 400 {
		t.Fatalf("expected 400, got %+v", apiErr.Transport)
	}
}
