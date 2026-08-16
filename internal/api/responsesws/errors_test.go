package responsesws

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/client"
)

func TestMapWrappedWebsocketErrorEventWithStatus(t *testing.T) {
	status := 502
	event := &wrappedWebsocketErrorEvent{
		Kind:    "error",
		Status:  &status,
		Headers: map[string]json.RawMessage{"x-h": json.RawMessage(`"v"`), "x-n": json.RawMessage(`5`), "x-b": json.RawMessage(`true`)},
	}
	apiErr := mapWrappedWebsocketErrorEvent(event, "raw")
	if apiErr == nil || apiErr.Kind != api.APIErrorTransport {
		t.Fatalf("expected transport error, got %v", apiErr)
	}
	te := apiErr.Transport
	if te.Status != 502 || te.Body != "raw" {
		t.Fatalf("unexpected transport error: %+v", te)
	}
	if te.Headers.Get("x-h") != "v" || te.Headers.Get("x-n") != "5" || te.Headers.Get("x-b") != "true" {
		t.Fatalf("unexpected headers: %v", te.Headers)
	}
}

func TestMapWrappedWebsocketErrorEventSuccessStatusIgnored(t *testing.T) {
	status := 200
	event := &wrappedWebsocketErrorEvent{Kind: "error", Status: &status}
	if apiErr := mapWrappedWebsocketErrorEvent(event, "raw"); apiErr != nil {
		t.Fatalf("expected nil for success status, got %v", apiErr)
	}
}

func TestParseWrappedWebsocketErrorEventNonError(t *testing.T) {
	if parseWrappedWebsocketErrorEvent(`{"type":"response.completed"}`) != nil {
		t.Fatalf("expected nil for non-error event")
	}
}

func TestMapWSDialErrorHTTPStatus(t *testing.T) {
	// resp with non-2xx triggers an HTTP transport error.
	resp := &http.Response{StatusCode: 401, Header: http.Header{}}
	apiErr := mapWSDialError(errors.New("boom"), "wss://x", resp)
	if apiErr == nil || apiErr.Kind != api.APIErrorTransport || apiErr.Transport.Status != 401 {
		t.Fatalf("expected 401 transport error, got %v", apiErr)
	}
}

func TestMapWSDialErrorNetwork(t *testing.T) {
	apiErr := mapWSDialError(errors.New("connection refused"), "wss://x", nil)
	if apiErr == nil || apiErr.Kind != api.APIErrorTransport || apiErr.Transport.Kind != client.TransportErrorNetwork {
		t.Fatalf("expected network transport error, got %v", apiErr)
	}
}
