package responsesws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestResponsesWebsocketStreamRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header on ws handshake")
		}
		w.Header().Set("OpenAI-Model", "ws-server-model")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode: websocket.CompressionContextTakeover,
		})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()
		// Read the request frame.
		_, _, err = conn.Read(ctx)
		if err != nil {
			return
		}
		// Reply with one item and a completed event.
		item := `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`
		completed := `{"type":"response.completed","response":{"id":"resp-ws"}}`
		_ = conn.Write(ctx, websocket.MessageText, []byte(item))
		_ = conn.Write(ctx, websocket.MessageText, []byte(completed))
	}))
	defer srv.Close()

	provider := api.Provider{
		Name:              "test",
		BaseURL:           srv.URL,
		StreamIdleTimeout: 2 * time.Second,
	}
	wsClient := NewResponsesWebsocketClient(provider, api.NewBearerAuth("tok"), srv.Client())

	conn, apiErr := wsClient.Connect(context.Background(), http.Header{}, http.Header{}, nil, nil)
	if apiErr != nil {
		t.Fatalf("connect: %v", apiErr)
	}
	defer conn.Close()

	req := api.ResponsesWsRequest{
		Kind: api.ResponsesWsRequestCreate,
		Create: &api.ResponseCreateWsRequest{
			Model:      "m",
			Input:      []protocol.ResponseItem{},
			ToolChoice: "auto",
			Stream:     true,
		},
	}
	stream, apiErr := conn.StreamRequest(context.Background(), req, false)
	if apiErr != nil {
		t.Fatalf("stream request: %v", apiErr)
	}

	var kinds []api.ResponseEventKind
	for res := range stream.Events {
		if res.Err != nil {
			t.Fatalf("stream error: %v", res.Err)
		}
		kinds = append(kinds, res.Event.Kind)
	}
	if len(kinds) < 3 {
		t.Fatalf("expected at least 3 events, got %v", kinds)
	}
	if kinds[0] != api.ResponseEventServerModel {
		t.Fatalf("expected server model first, got %v", kinds[0])
	}
	if kinds[len(kinds)-1] != api.ResponseEventCompleted {
		t.Fatalf("expected completed last, got %v", kinds[len(kinds)-1])
	}
}

func TestResponsesWebsocketConnectionLimitReachedIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()
		_, _, _ = conn.Read(ctx)
		errEvent := `{"type":"error","error":{"code":"websocket_connection_limit_reached","message":"limit"}}`
		_ = conn.Write(ctx, websocket.MessageText, []byte(errEvent))
		// Give the client time to read before the handler returns.
		time.Sleep(50 * time.Millisecond)
	}))
	defer srv.Close()

	provider := api.Provider{BaseURL: srv.URL, StreamIdleTimeout: 2 * time.Second}
	wsClient := NewResponsesWebsocketClient(provider, api.NoOpAuth{}, srv.Client())
	conn, apiErr := wsClient.Connect(context.Background(), http.Header{}, http.Header{}, nil, nil)
	if apiErr != nil {
		t.Fatalf("connect: %v", apiErr)
	}
	defer conn.Close()

	req := api.ResponsesWsRequest{
		Kind:   api.ResponsesWsRequestCreate,
		Create: &api.ResponseCreateWsRequest{Model: "m", Input: []protocol.ResponseItem{}, ToolChoice: "auto"},
	}
	stream, apiErr := conn.StreamRequest(context.Background(), req, false)
	if apiErr != nil {
		t.Fatalf("stream request: %v", apiErr)
	}
	var lastErr *api.APIError
	for res := range stream.Events {
		if res.Err != nil {
			lastErr = res.Err
		}
	}
	if lastErr == nil || lastErr.Kind != api.APIErrorRetryable {
		t.Fatalf("expected retryable error, got %v", lastErr)
	}
}

func TestMergeRequestHeaders(t *testing.T) {
	provider := http.Header{}
	provider.Set("X-Common", "provider")
	provider.Set("X-api.Provider-Only", "p")
	extra := http.Header{}
	extra.Set("X-Common", "extra")
	def := http.Header{}
	def.Set("X-Default-Only", "d")
	def.Set("X-Common", "default")

	merged := mergeRequestHeaders(provider, extra, def)
	if merged.Get("X-Common") != "extra" {
		t.Fatalf("extra should override, got %q", merged.Get("X-Common"))
	}
	if merged.Get("X-api.Provider-Only") != "p" {
		t.Fatalf("provider-only missing")
	}
	if merged.Get("X-Default-Only") != "d" {
		t.Fatalf("default-only should fill, got %q", merged.Get("X-Default-Only"))
	}
}
