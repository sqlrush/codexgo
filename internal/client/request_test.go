package client

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestPrepareBodyForSendSerializesJSONAndSetsContentType(t *testing.T) {
	req := NewRequest(http.MethodPost, "https://example.com/v1/responses").
		WithJSON(map[string]string{"model": "test-model"})

	prepared, err := req.PrepareBodyForSend()
	if err != nil {
		t.Fatalf("body should prepare: %v", err)
	}
	if got := string(prepared.Body); got != `{"model":"test-model"}` {
		t.Fatalf("unexpected body: %q", got)
	}
	if ct := prepared.Headers.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	// Original request must be unchanged.
	if req.Compression != CompressionNone {
		t.Fatalf("request compression mutated")
	}
}

func TestPrepareBodyForSendRejectsExistingContentEncodingWhenCompressing(t *testing.T) {
	req := NewRequest(http.MethodPost, "https://example.com/v1/responses").
		WithJSON(map[string]string{"model": "test-model"}).
		WithCompression(CompressionZstd)
	req.Headers.Set("Content-Encoding", "gzip")

	_, err := req.PrepareBodyForSend()
	if err == nil {
		t.Fatalf("conflicting content-encoding should fail")
	}
	want := "request compression was requested but content-encoding is already set"
	if err.Error() != want {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestPrepareBodyForSendRejectsCompressionForRawBody(t *testing.T) {
	req := NewRequest(http.MethodPost, "https://example.com").
		WithRawBody([]byte("hello")).
		WithCompression(CompressionZstd)
	_, err := req.PrepareBodyForSend()
	if err == nil || err.Error() != "request compression cannot be used with raw bodies" {
		t.Fatalf("expected raw-body compression error, got %v", err)
	}
}

func TestPrepareBodyForSendZstdRoundTrips(t *testing.T) {
	req := NewRequest(http.MethodPost, "https://example.com").
		WithJSON(map[string]any{"model": "m", "n": 1}).
		WithCompression(CompressionZstd)

	prepared, err := req.PrepareBodyForSend()
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Headers.Get("Content-Encoding") != "zstd" {
		t.Fatalf("missing zstd content-encoding")
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("decoder: %v", err)
	}
	defer dec.Close()
	decoded, err := dec.DecodeAll(prepared.Body, nil)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Contains(decoded, []byte(`"model":"m"`)) {
		t.Fatalf("unexpected decoded body: %s", decoded)
	}
}

func TestPrepareBodyForSendNoBody(t *testing.T) {
	req := NewRequest(http.MethodGet, "https://example.com")
	prepared, err := req.PrepareBodyForSend()
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Body != nil {
		t.Fatalf("expected nil body")
	}
	if len(prepared.BodyBytes()) != 0 {
		t.Fatalf("expected empty body bytes")
	}
}

func TestRawBodyNotMutated(t *testing.T) {
	raw := []byte("abc")
	body := NewRawBody(raw)
	raw[0] = 'z'
	if body.Raw[0] != 'a' {
		t.Fatalf("raw body shares backing array with caller input")
	}
}
