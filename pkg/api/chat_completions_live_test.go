package api

// Live diagnostic for the chat-completions client. Env-gated so the normal
// test run never touches the network:
//
//	CODEXGO_CHAT_PROBE_BASE_URL=https://api.deepseek.com/v1 \
//	CODEXGO_CHAT_PROBE_MODEL=deepseek-v4-pro \
//	CODEXGO_CHAT_PROBE_KEY=sk-... \
//	go test ./internal/api/ -run TestChatCompletionsLiveProbe -v

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/client"
)

// probeBearer is a minimal AuthProvider attaching a fixed bearer token.
type probeBearer struct{ token string }

func (b probeBearer) AddAuthHeaders(h http.Header) { h.Set("Authorization", "Bearer "+b.token) }
func (b probeBearer) ApplyAuth(_ context.Context, req client.Request) (client.Request, *AuthError) {
	out := req.WithCompression(req.Compression)
	b.AddAuthHeaders(out.Headers)
	return out, nil
}

func TestChatCompletionsLiveProbe(t *testing.T) {
	key := os.Getenv("CODEXGO_CHAT_PROBE_KEY")
	baseURL := os.Getenv("CODEXGO_CHAT_PROBE_BASE_URL")
	model := os.Getenv("CODEXGO_CHAT_PROBE_MODEL")
	if key == "" || baseURL == "" || model == "" {
		t.Skip("CODEXGO_CHAT_PROBE_{KEY,BASE_URL,MODEL} not set; skipping live probe")
	}

	provider := Provider{
		Name:              "probe",
		BaseURL:           baseURL,
		Retry:             RetryConfig{MaxAttempts: 1},
		StreamIdleTimeout: 60 * time.Second,
	}
	c := NewChatCompletionsClient(client.NewHTTPClientTransport(http.DefaultClient), provider, probeBearer{token: key})

	hi := "用一个词回答:好"
	req := ChatCompletionsRequest{
		Model:    model,
		Messages: []ChatMessage{{Role: "user", Content: &hi}},
		Stream:   true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	start := time.Now()
	stream, apiErr := c.StreamRequest(ctx, req, nil)
	if apiErr != nil {
		t.Fatalf("request error: %v", apiErr)
	}
	sawCompleted := false
	for res := range stream.Events {
		if res.Err != nil {
			t.Fatalf("stream error after %.1fs: %v", time.Since(start).Seconds(), res.Err)
		}
		t.Logf("[%5.1fs] kind=%d delta=%q item=%v", time.Since(start).Seconds(), res.Event.Kind, res.Event.Delta, res.Event.Item != nil)
		if res.Event.Kind == ResponseEventCompleted {
			sawCompleted = true
		}
	}
	if !sawCompleted {
		t.Fatalf("stream closed without Completed")
	}
}
