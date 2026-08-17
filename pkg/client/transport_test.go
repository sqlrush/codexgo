package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecuteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing content-type, got %q", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"model":"m"}` {
			t.Errorf("unexpected body %q", body)
		}
		w.Header().Set("X-Test", "ok")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	tr := NewHTTPClientTransport(srv.Client())
	req := NewRequest(http.MethodPost, srv.URL).WithJSON(map[string]string{"model": "m"})
	resp, err := tr.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != 200 || string(resp.Body) != "hello" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Headers.Get("X-Test") != "ok" {
		t.Fatalf("missing response header")
	}
}

func TestExecuteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("overloaded"))
	}))
	defer srv.Close()

	tr := NewHTTPClientTransport(srv.Client())
	_, err := tr.Execute(context.Background(), NewRequest(http.MethodGet, srv.URL))
	te, ok := err.(*TransportError)
	if !ok {
		t.Fatalf("expected TransportError, got %T", err)
	}
	if te.Kind != TransportErrorHTTP || te.Status != 503 || te.Body != "overloaded" {
		t.Fatalf("unexpected error: %+v", te)
	}
}

func TestStreamSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("chunk1"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("chunk2"))
	}))
	defer srv.Close()

	tr := NewHTTPClientTransport(srv.Client())
	stream, err := tr.Stream(context.Background(), NewRequest(http.MethodGet, srv.URL))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if stream.Status != 200 {
		t.Fatalf("unexpected status %d", stream.Status)
	}
	var got []byte
	for chunk := range stream.Bytes {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		got = append(got, chunk.Data...)
	}
	if string(got) != "chunk1chunk2" {
		t.Fatalf("unexpected stream body %q", got)
	}
}

func TestStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	tr := NewHTTPClientTransport(srv.Client())
	_, err := tr.Stream(context.Background(), NewRequest(http.MethodGet, srv.URL))
	te, ok := err.(*TransportError)
	if !ok || te.Status != 429 || te.Body != "rate limited" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	tr := NewHTTPClientTransport(srv.Client())
	req := NewRequest(http.MethodGet, srv.URL)
	req.Timeout = 20 * time.Millisecond
	_, err := tr.Execute(context.Background(), req)
	te, ok := err.(*TransportError)
	if !ok || te.Kind != TransportErrorTimeout {
		t.Fatalf("expected timeout error, got %v", err)
	}
}
